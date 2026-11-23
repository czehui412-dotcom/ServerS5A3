package dnfbridge

import (
	"testing"
	"time"

	"longheng.io/server/internal/modules/dnf/raid"
	repo "longheng.io/server/internal/modules/dnf/repository"
)

func testAccountWeeklyRule(limit int) *currentRaidAccountWeeklyRule {
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.FixedZone("CST", 8*3600))
	week, _ := raid.WeeklyEpoch(now, currentRaidAccountWeeklyPolicy)
	return &currentRaidAccountWeeklyRule{Mode: 1, Scope: "raid:anton:daily_entries", Limit: limit, Week: week, Now: now}
}

func TestCurrentRaidAccountWeeklyRuleUsesFixedLimit(t *testing.T) {
	s := &Service{}
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.FixedZone("CST", 8*3600))
	for mode := byte(1); mode <= 3; mode++ {
		rule, err := s.currentRaidAccountWeeklyRuleAt(mode, now)
		if err != nil {
			t.Fatalf("mode %d: %v", mode, err)
		}
		if rule.Limit != currentRaidAccountWeeklyLimit {
			t.Fatalf("mode %d: limit=%d want %d", mode, rule.Limit, currentRaidAccountWeeklyLimit)
		}
		if rule.Scope == "" {
			t.Fatalf("mode %d: empty scope", mode)
		}
	}
}

func TestCurrentRaidAccountWeeklyReserveRefundAndReplay(t *testing.T) {
	rule := testAccountWeeklyRule(2)
	record := repo.SettingsRecord{Scope: rule.Scope, Values: map[string]string{}}
	next, replay, err := reserveCurrentRaidAccountWeekly(record, []string{"account-1", "account-1"}, "instance-1", rule)
	if err != nil || replay {
		t.Fatal("first reserve failed", replay, err)
	}
	used, err := currentRaidAccountWeeklyUsed(next, "account-1", rule)
	if err != nil || used != 1 {
		t.Fatal("first reserve count", used, err)
	}
	again, replay, err := reserveCurrentRaidAccountWeekly(next, []string{"account-1"}, "instance-1", rule)
	if err != nil || !replay {
		t.Fatal("replay should not charge twice", replay, err)
	}
	used, _ = currentRaidAccountWeeklyUsed(again, "account-1", rule)
	if used != 1 {
		t.Fatal("replay changed count", used)
	}
	second, _, err := reserveCurrentRaidAccountWeekly(again, []string{"account-1"}, "instance-2", rule)
	if err != nil {
		t.Fatal("second reserve failed", err)
	}
	used, _ = currentRaidAccountWeeklyUsed(second, "account-1", rule)
	if used != 2 {
		t.Fatal("second reserve count", used)
	}
	_, _, err = reserveCurrentRaidAccountWeekly(second, []string{"account-1"}, "instance-3", rule)
	if err != raid.ErrWeeklyClearLimit {
		t.Fatal("weekly limit not enforced", err)
	}
	refunded, changed, err := refundCurrentRaidAccountWeekly(second, []string{"account-1"}, "instance-2", rule)
	if err != nil || !changed {
		t.Fatal("refund failed", changed, err)
	}
	used, _ = currentRaidAccountWeeklyUsed(refunded, "account-1", rule)
	if used != 1 {
		t.Fatal("refund did not restore count", used)
	}
	_, changed, err = refundCurrentRaidAccountWeekly(refunded, []string{"account-1"}, "instance-2", rule)
	if err != nil || changed {
		t.Fatal("duplicate refund should be ignored", changed, err)
	}
}

func TestCurrentRaidAccountWeeklyResetsOnNewWeek(t *testing.T) {
	rule := testAccountWeeklyRule(2)
	record := repo.SettingsRecord{Scope: rule.Scope, Values: map[string]string{
		"weekly_epoch_v1":       "1",
		"weekly_account_v1:a-1": "2",
	}}
	next, _, err := reserveCurrentRaidAccountWeekly(record, []string{"a-1"}, "instance-1", rule)
	if err != nil {
		t.Fatal(err)
	}
	used, err := currentRaidAccountWeeklyUsed(next, "a-1", rule)
	if err != nil || used != 1 {
		t.Fatal("new week was not reset", used, err)
	}
}
