package grf

import (
	"os"
	"testing"
)

// TestParseRealGRF verifies the dynamic parser against a real,
// currently-published NewGRF (JP+ Multiple Units), not a synthetic/
// hand-built file. The assertions cross-check against real-world,
// independently verifiable facts (the KiHa 40 series DMU's well-
// documented 1977 introduction), not just "did it not crash".
//
// The 18MB .grf itself is deliberately NOT committed to the repo (see
// .gitignore) -- fetch it locally to re-run this test:
//
//	mkdir -p testdata/grf-fixtures
//	# download "JP+ Multiple Units" from https://bananas.openttd.org
//	# and place the extracted JPplus_v055.grf at the path below.
//
// Skips (not fails) when the fixture isn't present, so a fresh clone's
// test suite stays green without it.
func TestParseRealGRF(t *testing.T) {
	const fixture = "../../testdata/grf-fixtures/JPplus_v055.grf"
	if _, err := os.Stat(fixture); err != nil {
		t.Skipf("real GRF fixture not present (%v) -- see this test's doc comment to fetch it locally", err)
	}
	parsed, err := ParseGRF(fixture)
	if err != nil {
		t.Fatalf("ParseGRF: %v", err)
	}
	if len(parsed.Warnings) > 0 {
		t.Logf("parser warnings (non-fatal, expected for some properties this package doesn't decode): %v", parsed.Warnings[:min(5, len(parsed.Warnings))])
	}
	if len(parsed.Engines) < 50 {
		t.Fatalf("expected at least 50 engines from a real multi-unit set, got %d", len(parsed.Engines))
	}

	byID := make(map[uint16]ParsedEngine, len(parsed.Engines))
	for _, e := range parsed.Engines {
		byID[e.LocalID] = e
	}

	// KiHa 40 series: real-world diesel multiple unit, entered service
	// 1977. daysTill1920 + (1977-1920 years, accounting for leap years)
	// should land in 1977 via this package's own year conversion --
	// reuse the same day-count math the engine package uses so this
	// test doesn't need to hardcode a specific day number.
	kiha40, ok := byID[1500]
	if !ok {
		t.Fatal("expected engine #1500 (KiHa 40 series) to be present")
	}
	if !kiha40.HasIntroDate {
		t.Fatal("KiHa 40 series: expected an introduction date to be set")
	}
	introYear := DayCountToYear(kiha40.IntroDate)
	if introYear != 1977 {
		t.Errorf("KiHa 40 series introduction year = %d, want 1977 (real-world fact, independently verifiable)", introYear)
	}
	if !kiha40.HasTrackType || kiha40.TrackType != 0 {
		t.Errorf("KiHa 40 series (a DMU running on regular rail): track type = %v/%d, want has=true type=0 (RAIL)", kiha40.HasTrackType, kiha40.TrackType)
	}
	if kiha40.IsWagon {
		t.Error("KiHa 40 series is a powered railcar, not a wagon")
	}
	if !kiha40.HasSpeed || kiha40.Speed == 0 {
		t.Error("expected a nonzero max speed for the KiHa 40 series")
	}
	if !kiha40.HasPower || kiha40.Power == 0 {
		t.Error("expected nonzero power for the KiHa 40 series (it's an engine, not a wagon)")
	}

	withData := 0
	for _, e := range parsed.Engines {
		if e.HasTrackType || e.HasIntroDate || e.HasSpeed || e.HasPower {
			withData++
		}
	}
	if withData < 50 {
		t.Errorf("only %d/%d engines had any dynamically-parsed property; expected the large majority to for a real set", withData, len(parsed.Engines))
	}
	t.Logf("%d/%d engines had at least one property dynamically parsed", withData, len(parsed.Engines))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
