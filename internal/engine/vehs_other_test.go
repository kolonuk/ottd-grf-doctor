package engine

import (
	"testing"

	"github.com/kolonuk/ottd-grf-doctor/internal/sav"
)

// TestOtherVehiclesDetectAndFixEndToEnd exercises the full detect -> plan
// -> apply pipeline for a non-train vehicle type. This project's real
// test fixture happens to have never had a broken road/ship/aircraft GRF
// (only its two train sets were ever actually broken -- see project
// history), so this test manufactures the scenario the same way it
// really happens in play: take a GRF that genuinely supplies real
// road/ship/aircraft engines in this save (found by scanning EIDS/VEHS
// for a real, currently-working example), and simulate it going missing
// by excluding it from the NGRF list passed to Analyze -- everything
// downstream (Analyze's detection, Apply's slot allocation, and
// ApplyToPayload's byte-level EIDS/VEHS patching) is the real,
// unmodified code a live session would run against a truly broken save.
func TestOtherVehiclesDetectAndFixEndToEnd(t *testing.T) {
	path := "../../testdata/xpressways-2082/broken.sav"
	s, err := sav.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	chunks, err := sav.WalkChunks(s.Payload)
	if err != nil {
		t.Fatal(err)
	}
	cm := sav.ChunkMapOf(chunks)

	eids, err := ParseEIDS(s.Payload, cm["EIDS"])
	if err != nil {
		t.Fatal(err)
	}
	ngrf, err := ParseNGRF(s.Payload, cm["NGRF"])
	if err != nil {
		t.Fatal(err)
	}
	vehicles, err := ParseVEHS(s.Payload, cm["VEHS"])
	if err != nil {
		t.Fatal(err)
	}
	others, err := ParseOtherVehicles(s.Payload, cm["VEHS"])
	if err != nil {
		t.Fatal(err)
	}
	if len(others) == 0 {
		t.Fatal("expected this fixture to have real road/ship/aircraft vehicles to test against")
	}

	// Pick a real GRFID that actually supplies road/ship/aircraft engines
	// in this save (not a default/base-game slot) to simulate going missing.
	eidsBySlot := make(map[int]EIDSEntry, len(eids))
	for _, e := range eids {
		eidsBySlot[e.EngineID] = e
	}
	var targetGRFID string
	var targetKind VehicleType
	var affected []OtherVehicle
	for _, ov := range others {
		e, ok := eidsBySlot[int(ov.EngineType)]
		if !ok || e.GRFID == InvalidGRFID || e.GRFID == "00000000" {
			continue
		}
		if targetGRFID == "" {
			targetGRFID = e.GRFID
			targetKind = ov.Kind
		}
		if e.GRFID == targetGRFID {
			affected = append(affected, ov)
		}
	}
	if targetGRFID == "" {
		t.Fatal("couldn't find a real GRF-sourced road/ship/aircraft vehicle in this save to test against")
	}
	t.Logf("simulating GRFID %s (kind %d) going missing: %d vehicle(s) affected", targetGRFID, targetKind, len(affected))

	// Simulate the GRF going missing by excluding it from the loaded list.
	var reducedNGRF []NGRFEntry
	for _, n := range ngrf {
		if n.GRFID != targetGRFID {
			reducedNGRF = append(reducedNGRF, n)
		}
	}

	an := Analyze(eids, reducedNGRF, vehicles, others, nil, nil)
	var missing *MissingGRF
	for i := range an.Missing {
		if an.Missing[i].GRFID == targetGRFID {
			missing = &an.Missing[i]
		}
	}
	if missing == nil {
		t.Fatal("Analyze did not detect the simulated missing GRF")
	}
	if len(missing.OtherVehicles) != len(affected) {
		t.Fatalf("Analyze found %d affected other-vehicles, want %d", len(missing.OtherVehicles), len(affected))
	}
	if missing.OtherVehicles[0].Kind != targetKind {
		t.Fatalf("Analyze's OtherVehicles has kind %d, want %d", missing.OtherVehicles[0].Kind, targetKind)
	}

	// Build and apply a plan reassigning every affected vehicle to a
	// synthetic replacement engine, then verify the resulting payload
	// actually reflects that reassignment.
	var vehicleIDs []int
	for _, ov := range missing.OtherVehicles {
		vehicleIDs = append(vehicleIDs, ov.VehicleID)
	}
	target := TargetEngine{GRFID: "CAFEBABE", InternalID: 7, Name: "Test Replacement"}
	plan := &Plan{Assignments: []Assignment{NewAssignment(vehicleIDs, target)}}

	res, err := Apply(an, plan)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(res.VehiclesMoved) != len(vehicleIDs) {
		t.Fatalf("expected %d vehicles moved, got %d", len(vehicleIDs), len(res.VehiclesMoved))
	}

	out, err := ApplyToPayload(s.Payload, eids, ngrf, vehicles, others, res, nil, nil)
	if err != nil {
		t.Fatalf("ApplyToPayload: %v", err)
	}

	// Re-parse the result and confirm every affected vehicle now points
	// at a slot whose EIDS entry is the replacement engine, tagged with
	// the correct VehicleType (not silently defaulted to VehTrain).
	outChunks, err := sav.WalkChunks(out)
	if err != nil {
		t.Fatal(err)
	}
	outCM := sav.ChunkMapOf(outChunks)
	newEIDS, err := ParseEIDS(out, outCM["EIDS"])
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateUniqueKeys(newEIDS); err != nil {
		t.Fatalf("post-apply EIDS failed uniqueness validation: %v", err)
	}
	newOthers, err := ParseOtherVehicles(out, outCM["VEHS"])
	if err != nil {
		t.Fatal(err)
	}
	newOtherByID := make(map[int]OtherVehicle, len(newOthers))
	for _, ov := range newOthers {
		newOtherByID[ov.VehicleID] = ov
	}

	newEIDSBySlot := make(map[int]EIDSEntry, len(newEIDS))
	for _, e := range newEIDS {
		newEIDSBySlot[e.EngineID] = e
	}

	checked := 0
	for vid, newSlot := range res.VehiclesMoved {
		ov, ok := newOtherByID[vid]
		if !ok {
			t.Fatalf("vehicle %d missing after apply", vid)
		}
		if int(ov.EngineType) != newSlot {
			t.Errorf("vehicle %d: engine_type = %d, want %d", vid, ov.EngineType, newSlot)
		}
		e, ok := newEIDSBySlot[newSlot]
		if !ok {
			t.Fatalf("slot %d has no EIDS entry after apply", newSlot)
		}
		if e.GRFID != target.GRFID || e.InternalID != target.InternalID {
			t.Errorf("slot %d EIDS entry = (grfid=%s id=%d), want (grfid=%s id=%d)", newSlot, e.GRFID, e.InternalID, target.GRFID, target.InternalID)
		}
		if e.Type != targetKind {
			t.Errorf("slot %d EIDS entry type = %d, want %d (the original slot's vehicle type -- a wrong type here is the exact bug class ValidateUniqueKeys exists to catch)", newSlot, e.Type, targetKind)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no vehicles were actually checked")
	}
	t.Logf("verified %d reassigned %v vehicle(s) end to end", checked, targetKind)
}
