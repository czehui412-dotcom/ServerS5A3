package dnfbridge

// currentFixedCountHellReward implements the GM "guaranteed 1..N pieces" tier.
// It first consumes the exact native PVF RNG stream so the separate epic-piece
// guarantee that runs later in the same room keeps its original sequence, then
// discards those probabilistic items and samples the fixed tier from a private
// RNG seeded by the room seed. Only real .equ candidates can be selected.
func currentFixedCountHellReward(request hellPartyRewardRequest, max int, roomSeed uint32) (hellPartyRewardResult, error) {
	native, err := currentNativePVFHellReward(request)
	if err != nil {
		return native, err
	}
	if max <= 0 {
		native.Selected = nil
		native.RateSource = "runtime_count_tier_off"
		native.RateOverridden = true
		return native, nil
	}

	pick := request
	pick.rng = newCurrentDungeonDropLCG(roomSeed)
	pick.RewardRollCount = int64(max * 3)
	forced := hellPartyRewardDropChanceDenominator - 1
	picked, err := currentNativePVFHellRewardWithChance(pick, &forced)
	if err != nil {
		return native, err
	}

	native.Selected = append([]hellPartyRewardItem(nil), picked.Selected...)
	if len(native.Selected) > max {
		native.Selected = native.Selected[:max]
	}
	for len(native.Selected) < max && len(picked.Selected) > 0 {
		native.Selected = append(native.Selected, picked.Selected[len(native.Selected)%len(picked.Selected)])
	}
	native.RateSource = "runtime_count_tier"
	native.RateOverridden = true
	return native, nil
}
