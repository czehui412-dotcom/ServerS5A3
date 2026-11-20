package dnfbridge

import (
	"strings"
	"testing"
)

func fixedCountTestCatalog(t *testing.T) *pvfDungeonDropCatalog {
	t.Helper()
	source := bridgeHellPartyRewardPVF()
	source[hellPartyRewardDropInfoPath] = privateNativeHellText(1000, 4)
	source["equipment/weapon/test_drop.equ"] = strings.Replace(
		source["equipment/weapon/test_drop.equ"], "[rarity]\n0", "[rarity]\n4", 1)
	catalog, err := newPVFDungeonDropCatalog(source)
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func fixedCountTestRequest(catalog *pvfDungeonDropCatalog, seed uint32) hellPartyRewardRequest {
	return hellPartyRewardRequest{
		DungeonMinimumLevel:  10,
		DungeonBasisLevel:    hellPartyRewardTestBasisLevel,
		AbyssPartyDifficulty: 1,
		DungeonDifficulty:    1,
		RewardRollCount:      8,
		dropCatalog:          catalog,
		rng:                  newCurrentDungeonDropLCG(seed),
	}
}

func TestFixedCountHellRewardHonorsTierAndPreservesRoomRNG(t *testing.T) {
	catalog := fixedCountTestCatalog(t)
	replay := fixedCountTestRequest(catalog, 7)
	if _, err := currentNativePVFHellReward(replay); err != nil {
		t.Fatal(err)
	}

	request := fixedCountTestRequest(catalog, 7)
	fixed, err := currentFixedCountHellReward(request, 4, 99)
	if err != nil {
		t.Fatal(err)
	}
	if len(fixed.Selected) != 4 {
		t.Fatalf("selected=%d want=4", len(fixed.Selected))
	}
	if fixed.RateSource != "runtime_count_tier" || !fixed.RateOverridden {
		t.Fatalf("fixed tier metadata missing: %+v", fixed)
	}
	if request.rng.Seed() != replay.rng.Seed() {
		t.Fatalf("main RNG changed: fixed=%d baseline=%d", request.rng.Seed(), replay.rng.Seed())
	}
}

func TestFixedCountHellRewardOffDropsNothingAndPreservesRoomRNG(t *testing.T) {
	catalog := fixedCountTestCatalog(t)
	replay := fixedCountTestRequest(catalog, 11)
	if _, err := currentNativePVFHellReward(replay); err != nil {
		t.Fatal(err)
	}
	request := fixedCountTestRequest(catalog, 11)
	off, err := currentFixedCountHellReward(request, 0, 99)
	if err != nil {
		t.Fatal(err)
	}
	if len(off.Selected) != 0 {
		t.Fatalf("off tier selected=%d", len(off.Selected))
	}
	if request.rng.Seed() != replay.rng.Seed() {
		t.Fatalf("main RNG changed: off=%d baseline=%d", request.rng.Seed(), replay.rng.Seed())
	}
}
