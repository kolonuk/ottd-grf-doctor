package engine

import "testing"

// TestDefaultTrainEnginesSpeedPower spot-checks the Speed/Power mining
// against known values from OpenTTD's own source
// (src/table/engines.h's _orig_rail_vehicle_info array, read directly
// during this package's development -- not guessed): SH '40' (id 24) is
// RVI(20, G, 30, 176, 5000, 82, 205, RC_E, 0, C, E), so max_speed=176,
// power=5000; X2001 (id 54, the first Monorail loco) is
// RVI(25, G, 52, 304, 9000, ...), so max_speed=304, power=9000.
func TestDefaultTrainEnginesSpeedPower(t *testing.T) {
	sh40, ok := DefaultTrainEngines[24]
	if !ok {
		t.Fatal("expected engine 24 (SH '40') to exist")
	}
	if sh40.Speed != 176 || sh40.Power != 5000 {
		t.Errorf("SH '40': got speed=%d power=%d, want speed=176 power=5000", sh40.Speed, sh40.Power)
	}
	if sh40.IsWagon {
		t.Error("SH '40' is a locomotive, not a wagon")
	}

	x2001, ok := DefaultTrainEngines[54]
	if !ok {
		t.Fatal("expected engine 54 (X2001) to exist")
	}
	if x2001.Speed != 304 || x2001.Power != 9000 {
		t.Errorf("X2001: got speed=%d power=%d, want speed=304 power=9000", x2001.Speed, x2001.Power)
	}

	wagon, ok := DefaultTrainEngines[27] // Passenger Carriage
	if !ok {
		t.Fatal("expected engine 27 (Passenger Carriage) to exist")
	}
	if !wagon.IsWagon || wagon.Speed != 0 || wagon.Power != 0 {
		t.Errorf("Passenger Carriage: expected a wagon with speed=0 power=0, got IsWagon=%v speed=%d power=%d", wagon.IsWagon, wagon.Speed, wagon.Power)
	}
}

// TestSubstituteEngineForRealSave verifies SubstituteEngineFor against
// the real fixture: every EIDS slot the broken Brianum train GRF used
// (see the project's original investigation) has substitute_id ==
// internal_id (see EncodeEIDSEntry's doc comment for why), so looking up
// slot 328 (a real broken slot from that save) must resolve to whatever
// default engine DefaultTrainEngines has at that same ID.
func TestSubstituteEngineForRealSave(t *testing.T) {
	eids := []EIDSEntry{
		{EngineID: 328, GRFID: "42533032", InternalID: 24, Type: VehTrain, SubstituteID: 24},
	}
	got, ok := SubstituteEngineFor(eids, 328)
	if !ok {
		t.Fatal("expected a substitute engine for slot 328")
	}
	want := DefaultTrainEngines[24]
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}

	if _, ok := SubstituteEngineFor(eids, 999); ok {
		t.Error("expected no substitute for a slot not present in eids")
	}
}
