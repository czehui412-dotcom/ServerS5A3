package dnfbridge

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"longheng.io/server/internal/modules/dnf/raid"
	"longheng.io/server/internal/modules/dnf/raid/fiendwar"
	repo "longheng.io/server/internal/modules/dnf/repository"
)

// 账号周入场次数账本。与角色周通关次数（raid.WeeklyClears/op1442）不同，
// 这里记录的是"账号在本周进入某团本的次数"，用于客户端显示"账号周 X/Y"。
// 与日次数共用同一个 settings scope，确保门票扣减、日次数、账号周次数在同一事务提交。
type currentRaidAccountWeeklyRule struct {
	Mode  byte
	Scope string
	Limit int
	Week  int64
	Now   time.Time
}

const (
	currentRaidAccountWeeklyReceiptPrefix = "weekly_enter_receipt_v1:"
	currentRaidAccountWeeklyLimit         = 20
)

var currentRaidAccountWeeklyPolicy = raid.WeeklyPolicy{ResetWeekday: time.Thursday, ResetHour: 6}

func (s *Service) currentRaidAccountWeeklyRule(mode byte) (*currentRaidAccountWeeklyRule, error) {
	return s.currentRaidAccountWeeklyRuleAt(mode, s.gameplayNow())
}

func (s *Service) currentRaidAccountWeeklyRuleAt(mode byte, now time.Time) (*currentRaidAccountWeeklyRule, error) {
	var scope string
	switch mode {
	case 1:
		scope = "raid:anton:daily_entries"
	case 2:
		scope = "raid:luke:daily_entries"
	case 3:
		scope = fiendwar.FiendWarDailyScope
	default:
		return nil, fmt.Errorf("账号周入场次数模式无效")
	}
	week, err := raid.WeeklyEpoch(now, currentRaidAccountWeeklyPolicy)
	if err != nil {
		return nil, err
	}
	return &currentRaidAccountWeeklyRule{Mode: mode, Scope: scope, Limit: currentRaidAccountWeeklyLimit, Week: week, Now: now}, nil
}

func currentRaidAccountWeeklyUsed(record repo.SettingsRecord, account string, rule *currentRaidAccountWeeklyRule) (int, error) {
	if rule == nil || strings.TrimSpace(account) == "" {
		return 0, fmt.Errorf("账号周入场次数参数无效")
	}
	if stored, ok := record.Values["weekly_epoch_v1"]; !ok || stored != strconv.FormatInt(rule.Week, 10) {
		return 0, nil
	}
	value, ok := record.Values["weekly_account_v1:"+account]
	if !ok || value == "" {
		return 0, nil
	}
	used, err := strconv.Atoi(value)
	if err != nil || used < 0 || used > rule.Limit {
		return 0, fmt.Errorf("账号周入场次数记录无效")
	}
	return used, nil
}

func currentRaidAccountWeeklyReceiptKey(instanceID, account string) string {
	return currentRaidAccountWeeklyReceiptPrefix + instanceID + ":" + account
}

// reserveCurrentRaidAccountWeekly 在同一 settings 记录里预留账号周次数。
// 同实例+账号重复调用返回 replay=true，不再加次数。
func reserveCurrentRaidAccountWeekly(record repo.SettingsRecord, accounts []string, instanceID string, rule *currentRaidAccountWeeklyRule) (repo.SettingsRecord, bool, error) {
	if rule == nil || strings.TrimSpace(instanceID) == "" || len(accounts) == 0 {
		return record, false, fmt.Errorf("账号周入场次数预留参数无效")
	}
	next := repo.CloneSettings(record)
	if next.Values == nil {
		next.Values = make(map[string]string)
	}
	next.Scope = rule.Scope
	if stored, ok := next.Values["weekly_epoch_v1"]; ok && stored != strconv.FormatInt(rule.Week, 10) {
		for key := range next.Values {
			if strings.HasPrefix(key, "weekly_account_v1:") || strings.HasPrefix(key, currentRaidAccountWeeklyReceiptPrefix) {
				delete(next.Values, key)
			}
		}
	}
	next.Values["weekly_epoch_v1"] = strconv.FormatInt(rule.Week, 10)
	seen := make(map[string]bool, len(accounts))
	replay := true
	for _, account := range accounts {
		account = strings.TrimSpace(account)
		if account == "" || seen[account] {
			continue
		}
		seen[account] = true
		receipt := currentRaidAccountWeeklyReceiptKey(instanceID, account)
		if _, done := next.Values[receipt]; done {
			continue
		}
		used, err := currentRaidAccountWeeklyUsed(next, account, rule)
		if err != nil {
			return record, false, err
		}
		if used >= rule.Limit {
			return record, false, raid.ErrWeeklyClearLimit
		}
		next.Values[receipt] = strconv.FormatInt(rule.Now.Unix(), 10)
		next.Values["weekly_account_v1:"+account] = strconv.Itoa(used + 1)
		next.UpdatedAt = rule.Now.UTC()
		replay = false
	}
	return next, replay, nil
}

// refundCurrentRaidAccountWeekly 失败返还次数；只返还已有入场回执且尚未返还的实例。
func (s *Service) refundCurrentRaidAccountWeeklyForRecipient(ctx context.Context, repos repo.Group, mode byte, instanceID, account string, failedAt time.Time) {
	if s == nil || repos.Settings == nil || strings.TrimSpace(account) == "" || strings.TrimSpace(instanceID) == "" {
		return
	}
	rule, err := s.currentRaidAccountWeeklyRuleAt(mode, failedAt)
	if err != nil {
		s.logWarn("game-raid-account-weekly-refund-rule", "error", err, "mode", mode)
		return
	}
	record, _, err := repos.Settings.Load(ctx, rule.Scope)
	if err != nil {
		s.logWarn("game-raid-account-weekly-refund-load", "error", err, "account_id", account, "instance_id", instanceID)
		return
	}
	next, changed, err := refundCurrentRaidAccountWeekly(record, []string{account}, instanceID, rule)
	if err != nil {
		s.logWarn("game-raid-account-weekly-refund", "error", err, "account_id", account, "instance_id", instanceID)
		return
	}
	if !changed {
		return
	}
	if err := repos.Settings.Save(ctx, next); err != nil {
		s.logWarn("game-raid-account-weekly-refund-save", "error", err, "account_id", account, "instance_id", instanceID)
	}
}

func refundCurrentRaidAccountWeekly(record repo.SettingsRecord, accounts []string, instanceID string, rule *currentRaidAccountWeeklyRule) (repo.SettingsRecord, bool, error) {
	if rule == nil || strings.TrimSpace(instanceID) == "" || len(accounts) == 0 {
		return record, false, fmt.Errorf("账号周入场次数返还参数无效")
	}
	next := repo.CloneSettings(record)
	if stored, ok := next.Values["weekly_epoch_v1"]; !ok || stored != strconv.FormatInt(rule.Week, 10) {
		return next, false, nil
	}
	changed := false
	seen := make(map[string]bool, len(accounts))
	for _, account := range accounts {
		account = strings.TrimSpace(account)
		if account == "" || seen[account] {
			continue
		}
		seen[account] = true
		receipt := currentRaidAccountWeeklyReceiptKey(instanceID, account)
		if _, ok := next.Values[receipt]; !ok {
			continue
		}
		refunded := receipt + ":refunded"
		if _, ok := next.Values[refunded]; ok {
			continue
		}
		used, err := currentRaidAccountWeeklyUsed(next, account, rule)
		if err != nil {
			return record, false, err
		}
		if used > 0 {
			next.Values["weekly_account_v1:"+account] = strconv.Itoa(used - 1)
		}
	next.Values[refunded] = strconv.FormatInt(rule.Now.Unix(), 10)
	next.UpdatedAt = rule.Now.UTC()
	changed = true
	}
	return next, changed, nil
}

type accountRaidWeeklyRow struct {
	Mode         byte
	AccountUsed  int
	AccountLimit int
}

// accountRaidWeeklySnapshot 返回账号在三个团本的周入场次数（used/limit）。
// 读取失败时对应项返回 0/全局上限，不阻塞调用方的提示流程。
func (s *Service) accountRaidWeeklySnapshot(ctx context.Context, account string) []accountRaidWeeklyRow {
	rows := make([]accountRaidWeeklyRow, 0, 3)
	if strings.TrimSpace(account) == "" {
		return rows
	}
	repos, ok := s.repositoryGroup()
	if !ok || repos.Settings == nil {
		return rows
	}
	for mode := byte(1); mode <= 3; mode++ {
		rule, err := s.currentRaidAccountWeeklyRule(mode)
		if err != nil {
			continue
		}
		record, _, err := repos.Settings.Load(ctx, rule.Scope)
		if err != nil {
			continue
		}
		used, err := currentRaidAccountWeeklyUsed(record, account, rule)
		if err != nil || used < 0 {
			used = 0
		}
		if used > rule.Limit {
			used = rule.Limit
		}
		rows = append(rows, accountRaidWeeklyRow{Mode: mode, AccountUsed: used, AccountLimit: rule.Limit})
	}
	return rows
}
