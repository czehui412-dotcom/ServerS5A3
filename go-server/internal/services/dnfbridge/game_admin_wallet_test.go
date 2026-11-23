package dnfbridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"

	"longheng.io/server/internal/modules/dnf/adminapi"
	dnfcerashop "longheng.io/server/internal/modules/dnf/cerashop"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

const walletProjectionTestAccount = "dnf:paodian-projection-account"

func newWalletProjectionFixture(t *testing.T, accountID string, cera, token, happy int64) (*Service, *gameSession, *bufferConn, dnfrepo.Group) {
	t.Helper()
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	account := dnfrepo.AccountRecord{AccountID: accountID}
	dnfcerashop.SetBalance(&account, cera)
	dnfcerashop.SetTokenCeraBalance(&account, token)
	dnfcerashop.SetHappyTokenCeraBalance(&account, happy)
	if err := repositories.Account.Save(ctx, account); err != nil {
		t.Fatal(err)
	}
	conn := &bufferConn{}
	service := &Service{
		options: options{
			accountID:          accountID,
			gameUpperHeader:    gameUpperHeaderChannel13,
			gameUpperBodyCodec: gameUpperBodyCodecPlain,
		},
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	session := &gameSession{
		conn:                conn,
		connID:              t.Name(),
		accountID:           accountID,
		selectedCharacterID: 19,
		characterGeneration: 1,
	}
	service.mu.Lock()
	service.gameSessions = map[uint16]*gameSession{19: session}
	service.mu.Unlock()
	return service, session, conn, repositories
}

func TestCreditAccountWalletProjectionRefreshesWalletAndShowsSystemNotice(t *testing.T) {
	service, _, conn, repositories := newWalletProjectionFixture(t, walletProjectionTestAccount, 123, 45, 6)
	notice := "每60秒获得30代币券，当前获得30代币券，本次登录共计获得90代币券。"
	pushed, accounts, balanceSet, err := service.creditAccountWalletProjection(adminapi.WalletGrant{
		AccountID:     walletProjectionTestAccount,
		Notice:        notice,
		CurrencyLabel: "代币券",
		Amount:        30,
	})
	if err != nil || pushed != 1 || !balanceSet || len(accounts) != 1 || accounts[0] != "19" {
		t.Fatalf("projection: pushed=%d accounts=%v balanceSet=%t err=%v", pushed, accounts, balanceSet, err)
	}

	data := conn.write.Bytes()
	wallet, rest := splitGameServerUpperPacket(t, data)
	if wallet.Header.Classification != 0 || wallet.Header.MsgID != currentCeraShopUpdateMsgID {
		t.Fatalf("wallet header class=%d msg=%d, want class0/op%d", wallet.Header.Classification, wallet.Header.MsgID, currentCeraShopUpdateMsgID)
	}
	if want := buildCurrentCeraShopWalletBody(123, 45, 6); !bytes.Equal(wallet.Body, want) {
		t.Fatalf("wallet body=%x want %x", wallet.Body, want)
	}

	chat, rest := splitGameServerUpperPacket(t, rest)
	if chat.Header.Classification != 0 || chat.Header.MsgID != currentAreaChatNotifyOpcode {
		t.Fatalf("notice header class=%d msg=%d, want class0/op%d", chat.Header.Classification, chat.Header.MsgID, currentAreaChatNotifyOpcode)
	}
	if len(chat.Body) < 9 || chat.Body[0] != currentChatNoticeTypeSystem || binary.LittleEndian.Uint16(chat.Body[1:3]) != 0xffff || chat.Body[3] != 0 {
		t.Fatalf("NOTI12 prefix=%x, want type=0(system) sender=ffff channel=0", chat.Body[:min(len(chat.Body), 4)])
	}
	encoded, encErr := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(notice))
	if encErr != nil {
		t.Fatal(encErr)
	}
	if got := binary.LittleEndian.Uint32(chat.Body[4:8]); got != uint32(len(encoded)) {
		t.Fatalf("NOTI12 message dstr length=%d want %d", got, len(encoded))
	}
	if !bytes.Equal(chat.Body[8:8+len(encoded)], encoded) {
		t.Fatalf("NOTI12 message=%x want %x", chat.Body[8:8+len(encoded)], encoded)
	}
	if restLen := len(rest); restLen > 0 {
		extra, _ := splitGameServerUpperPacket(t, rest)
		t.Fatalf("unexpected extra packet msg=%d", extra.Header.MsgID)
	}

	// Projection is display-only: the durable wallet must be untouched.
	account, found, err := repositories.Account.Load(context.Background(), walletProjectionTestAccount)
	if err != nil || !found {
		t.Fatalf("load account: found=%t err=%v", found, err)
	}
	balance := dnfcerashop.WalletBalance(account)
	if balance.Cera != 123 || balance.TokenCera != 45 || balance.HappyTokenCera != 6 {
		t.Fatalf("projection changed wallet: %+v", balance)
	}
}

func TestCreditAccountWalletProjectionSkipsOtherAccountsAndEmptyNotice(t *testing.T) {
	service, _, conn, _ := newWalletProjectionFixture(t, walletProjectionTestAccount, 10, 20, 0)

	pushed, accounts, _, err := service.creditAccountWalletProjection(adminapi.WalletGrant{
		AccountID: "dnf:someone-else",
		Notice:    "不该收到",
	})
	if err != nil || pushed != 0 || len(accounts) != 0 || conn.write.Len() != 0 {
		t.Fatalf("foreign account projection: pushed=%d accounts=%v bytes=%d err=%v", pushed, accounts, conn.write.Len(), err)
	}

	pushed, accounts, _, err = service.creditAccountWalletProjection(adminapi.WalletGrant{
		AccountID: walletProjectionTestAccount,
		Notice:    "   ",
	})
	if err != nil || pushed != 1 || len(accounts) != 1 {
		t.Fatalf("empty-notice projection: pushed=%d accounts=%v err=%v", pushed, accounts, err)
	}
	wallet, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if wallet.Header.MsgID != currentCeraShopUpdateMsgID {
		t.Fatalf("wallet msg=%d want %d", wallet.Header.MsgID, currentCeraShopUpdateMsgID)
	}
	if len(rest) != 0 {
		t.Fatalf("empty notice must not emit a chat packet, rest=%d", len(rest))
	}
}

func TestCreditAccountWalletProjectionRejectsMissingAccount(t *testing.T) {
	service := &Service{}
	if _, _, _, err := service.creditAccountWalletProjection(adminapi.WalletGrant{}); err == nil {
		t.Fatal("missing account must be rejected")
	}
}
