package dnfbridge

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/text/encoding/simplifiedchinese"
)

const raidAccountWeeklyNoticeTimeout = 1500 * time.Millisecond

// Current class0/1228 (0373F7D0) consumes one byte and records GetTickCount
// through 01A54A70. The byte is discarded; it is not a reward or counter.
// 01A54A90 rejects the raid directory while that timestamp is -1, then for
// 3000 ms after this notice. Without it, "please try again later" never ends.
// The selected town bootstrap is the lifecycle owner, including reconnect;
// ordinary movement and directory requests must not restart the cooldown.
const currentRaidChannelReadyMsgID uint16 = 1228

func (s *Service) sendCurrentRaidChannelReady(session *gameSession, source string) error {
	if s == nil || session == nil || session.selectedCharacterID == 0 || s.options.currentClientVersion != "official-24.9" {
		return nil
	}
	if _, supported := currentRaidRouteForChannel(int(session.channel.Type)); !supported {
		return nil
	}
	if err := s.sendGameUpperRawClass(session, currentRaidChannelReadyMsgID, []byte{0}, 0); err != nil {
		return err
	}
	s.logGameEvent(session, "game-current-raid-channel-ready", "source", source,
		"char_id", session.selectedCharacterID, "msg_id", currentRaidChannelReadyMsgID,
		"classification", 0, "body_len", 1, "cooldown_ms", 3000)
	s.sendRaidAccountWeeklyNotice(session)
	return nil
}

// sendRaidAccountWeeklyNotice 在进团本频道时向"系统"聊天页发一条账号周入场提示。
// 失败只记日志，不影响 op1228 主流程。
func (s *Service) sendRaidAccountWeeklyNotice(session *gameSession) {
	account := s.accountIDForSession(session)
	if account == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), raidAccountWeeklyNoticeTimeout)
	defer cancel()
	rows := s.accountRaidWeeklySnapshot(ctx, account)
	if len(rows) != 3 {
		return
	}
	used := map[byte]int{}
	limit := currentRaidAccountWeeklyLimit
	for _, row := range rows {
		used[row.Mode], limit = row.AccountUsed, row.AccountLimit
	}
	// 显示"剩余/上限"：满 20 次时显示 0/20，没用过显示 20/20。
	// snapshot 已把 used 钳制在 0..limit，这里 limit-used 不会为负。
	message := fmt.Sprintf("【账号周入场】安徒恩 %d/%d　卢克 %d/%d　超时空 %d/%d",
		limit-used[1], limit, limit-used[2], limit, limit-used[3], limit)
	encoded, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(message))
	if err != nil || len(encoded) > 240 {
		s.logGameEvent(session, "game-raid-account-weekly-notice", "result", "encode_failed", "bytes", len(encoded))
		return
	}
	if err := s.sendMegaphoneChatNotice(session, encoded); err != nil {
		s.logGameEvent(session, "game-raid-account-weekly-notice", "result", "send_failed", "error", err.Error())
		return
	}
	s.logGameEvent(session, "game-raid-account-weekly-notice", "result", "sent",
		"account", account, "char_id", session.selectedCharacterID,
		"anton", used[1], "luke", used[2], "fiendwar", used[3], "limit", limit)
}
