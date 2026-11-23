package dnfbridge

import (
	"context"
	"strconv"
	"strings"

	"golang.org/x/text/encoding/simplifiedchinese"

	repo "longheng.io/server/internal/modules/dnf/repository"
)

// currentRaidWeeklyExhaustedInRecord 是账号周满的纯判定：已用达到上限即为满。
// 账本读取/周次有效性由调用方保证，读取失败时调用方放行给 raid start 权威闸门。
func currentRaidWeeklyExhaustedInRecord(record repo.SettingsRecord, account string, rule *currentRaidAccountWeeklyRule) bool {
	if rule == nil || strings.TrimSpace(account) == "" {
		return false
	}
	used, err := currentRaidAccountWeeklyUsed(record, account, rule)
	return err == nil && used >= rule.Limit
}

// currentRaidCreateJoinWeeklyExhausted 是创建/加入攻坚队阶段的账号周次数前置闸门。
// 它只读账本、不写次数；真正的预留仍在 raid start 的 consumeRaidEntryCostAndWeekly
// 同一事务里，start 仍是最终权威。读取失败时放行，让 start 继续判定。
func (s *Service) currentRaidCreateJoinWeeklyExhausted(session *gameSession, route byte, account string) bool {
	if session == nil || strings.TrimSpace(account) == "" || route > 2 {
		return false
	}
	rule, err := s.currentRaidAccountWeeklyRule(route + 1)
	if err != nil {
		return false
	}
	repos, ok := s.repositoryGroup()
	if !ok || repos.Settings == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	record, _, err := repos.Settings.Load(ctx, rule.Scope)
	if err != nil {
		return false
	}
	return currentRaidWeeklyExhaustedInRecord(record, account, rule)
}

func currentRaidWeeklyFullMessage(action string) string {
	return "无法" + action + "攻坚队：账号本周入场次数已达" + strconv.Itoa(currentRaidAccountWeeklyLimit) + "次。"
}

// NOTI12 的 type 字节决定落到客户端哪个聊天页签：0=系统（浅蓝）、14=喇叭（绿）。
// 两种 surface 都由客户端按 type 选，服务端不消耗喇叭道具。
const (
	currentChatNoticeTypeSystem    = byte(0)
	currentChatNoticeTypeMegaphone = byte(14)
)

// sendSystemChatNotice 把一段已编码的 GBK 文本发到系统页(type0，浅蓝)。
func (s *Service) sendSystemChatNotice(session *gameSession, encoded []byte) error {
	if session == nil {
		return nil
	}
	var w packetWriter
	w.writeByte(currentChatNoticeTypeSystem)
	w.writeUint16(0xffff)
	w.writeByte(0)
	w.writeRawDstr(encoded)
	w.writeRawDstr(nil)
	return s.sendGameUpperRawClass(session, currentAreaChatNotifyOpcode, w.bytes(), 0)
}

// sendMegaphoneChatNotice 把一段已编码的 GBK 文本只发到喇叭页(type14)。
// 前缀 u8 + u16 + u8 + DSTR 与旧版 type14 完全一致，尾部多一个空 DSTR 不影响 reader。
// 通用工具：团本次数提示、泡点奖励提示等都复用它。
func (s *Service) sendMegaphoneChatNotice(session *gameSession, encoded []byte) error {
	if session == nil {
		return nil
	}
	var w packetWriter
	w.writeByte(currentChatNoticeTypeMegaphone)
	w.writeUint16(0xffff)
	w.writeByte(0)
	w.writeRawDstr(encoded)
	w.writeRawDstr(nil)
	return s.sendGameUpperRawClass(session, currentAreaChatNotifyOpcode, w.bytes(), 0)
}

// sendRaidCreateJoinWeeklyNotice 发一条 NOTI12 喇叭页文本说明账号周次数已满。
// DSTR 群发文本不用 roster 短名编码（后者会在角色名长度处截断整段原因）。
func (s *Service) sendRaidCreateJoinWeeklyNotice(session *gameSession, message string) error {
	if session == nil {
		return nil
	}
	encoded, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte(message))
	if err != nil {
		return err
	}
	return s.sendMegaphoneChatNotice(session, encoded)
}

// sendRaidCreateJoinWeeklyRejection 先用原 opcode 的失败 ACK 关闭客户端等待态，
// 再补一条 NOTI12 系统文本说明账号周次数已满。
func (s *Service) sendRaidCreateJoinWeeklyRejection(session *gameSession, op uint16, message string) error {
	if err := s.sendRaidCommandFailure(session, op); err != nil {
		return err
	}
	return s.sendRaidCreateJoinWeeklyNotice(session, message)
}
