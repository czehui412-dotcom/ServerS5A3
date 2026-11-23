package dnfbridge

import (
	"errors"
	"strconv"
	"strings"

	"golang.org/x/text/encoding/simplifiedchinese"

	"longheng.io/server/internal/modules/dnf/adminapi"
)

// paodianNoticeMaxBytes keeps the GBK-encoded system notice inside the current
// client's 256-wide-character text buffer (240 bytes leaves room for a trailing
// multi-byte glyph).
const paodianNoticeMaxBytes = 240

// creditAccountWalletProjection projects a wallet grant already committed by
// the gateway to every live session owned by the account. It never credits:
// balances are re-read from the authoritative repository, so this is
// display-only and safe to retry. A session that has logged out or switched
// character between snapshot and delivery is dropped by the identity check.
func (s *Service) creditAccountWalletProjection(grant adminapi.WalletGrant) (int, []string, bool, error) {
	accountID := strings.TrimSpace(grant.AccountID)
	if accountID == "" {
		return 0, nil, false, errors.New("wallet grant account required")
	}

	text := strings.TrimSpace(grant.Notice)
	encoded, encErr := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(text))
	if encErr != nil || len(encoded) > paodianNoticeMaxBytes {
		encoded, encErr = simplifiedchinese.GBK.NewEncoder().Bytes([]byte("泡点奖励已到账。"))
		if encErr != nil {
			return 0, nil, false, encErr
		}
	}

	identities := s.paodianOnlineIdentities(accountID)
	pushed := 0
	accounts := make([]string, 0, len(identities))
	for _, identity := range identities {
		character := strconv.Itoa(int(identity.character))
		err := s.tryPostGameSessionCharacterEvent(identity.session, "credit-account-wallet",
			identity.character, identity.generation, func() error {
				if !s.boundGameSessionCharacterCurrent(identity) {
					return nil
				}
				// 0x35 class0 wallet refresh from the authoritative account wallet.
				if err := s.sendCurrentAccountWallet(identity.session); err != nil {
					return err
				}
				if text == "" {
					return nil
				}
				// NOTI12 type=0 -> "系统" chat tab（浅蓝）；团本次数提示仍走喇叭页 type14。
				return s.sendSystemChatNotice(identity.session, encoded)
			})
		if err != nil {
			s.logWarn("dnfbridge admin wallet projection failed", "char_id", character, "error", err)
			continue
		}
		pushed++
		accounts = append(accounts, character)
	}
	return pushed, accounts, true, nil
}

// paodianOnlineIdentities collects every live session owned by one account.
// It mirrors administrativeOnlineSnapshot's identity guards so a half-closed
// or re-logged session can never receive a stale projection.
func (s *Service) paodianOnlineIdentities(accountID string) []boundGameSessionCharacter {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]boundGameSessionCharacter, 0, len(s.gameSessions))
	for id, session := range s.gameSessions {
		if session == nil || id == 0 || session.selectedCharacterID != id || session.characterGeneration == 0 {
			continue
		}
		if s.accountIDForSession(session) != accountID {
			continue
		}
		out = append(out, boundGameSessionCharacter{
			session: session, character: id, generation: session.characterGeneration,
		})
	}
	return out
}
