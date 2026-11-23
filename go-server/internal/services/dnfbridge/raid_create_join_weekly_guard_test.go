package dnfbridge

import (
	"strconv"
	"strings"
	"testing"

	repo "longheng.io/server/internal/modules/dnf/repository"
)

func weeklyGuardRecord(rule *currentRaidAccountWeeklyRule, account string, used int) repo.SettingsRecord {
	return repo.SettingsRecord{Scope: rule.Scope, Values: map[string]string{
		"weekly_epoch_v1":                       strconv.FormatInt(rule.Week, 10),
		"weekly_account_v1:" + account:         strconv.Itoa(used),
	}}
}

func TestCurrentRaidWeeklyExhaustedInRecord(t *testing.T) {
	rule := testAccountWeeklyRule(currentRaidAccountWeeklyLimit)
	const account = "dnf:test-account"

	cases := []struct {
		name string
		rec  repo.SettingsRecord
		want bool
	}{
		{"used_19_not_exhausted", weeklyGuardRecord(rule, account, 19), false},
		{"used_20_exhausted", weeklyGuardRecord(rule, account, 20), true},
		{"empty_week_not_exhausted", repo.SettingsRecord{Scope: rule.Scope, Values: map[string]string{}}, false},
		{"other_week_not_exhausted", repo.SettingsRecord{Scope: rule.Scope, Values: map[string]string{
			"weekly_epoch_v1":               "0",
			"weekly_account_v1:" + account: "20",
		}}, false},
	}
	for _, tc := range cases {
		if got := currentRaidWeeklyExhaustedInRecord(tc.rec, account, rule); got != tc.want {
			t.Fatalf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
	if currentRaidWeeklyExhaustedInRecord(weeklyGuardRecord(rule, account, 20), "", rule) {
		t.Fatal("empty account must not be exhausted")
	}
	if currentRaidWeeklyExhaustedInRecord(weeklyGuardRecord(rule, account, 20), account, nil) {
		t.Fatal("nil rule must not be exhausted")
	}
}

func TestCurrentRaidWeeklyFullMessage(t *testing.T) {
	for _, action := range []string{"创建", "加入"} {
		msg := currentRaidWeeklyFullMessage(action)
		if !strings.HasPrefix(msg, "无法"+action+"攻坚队") || !strings.Contains(msg, "20次") {
			t.Fatalf("unexpected message for %s: %q", action, msg)
		}
	}
}
