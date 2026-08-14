package engine

import (
	"testing"

	"github.com/kolonuk/ottd-grf-doctor/internal/sav"
)

// TestTileRailtypeMatchesKnownGood confirms MapTiles reproduces the by-hand
// finding from this project's original investigation: every vehicle on
// the old Brianum engine slots sits on MONO track, which is the entire
// reason the naive "swap in a standard-rail replacement" attempt failed.
func TestTileRailtypeMatchesKnownGood(t *testing.T) {
	s, err := sav.Load("../../testdata/xpressways-2082/broken.sav")
	if err != nil {
		t.Fatal(err)
	}
	chunks, err := sav.WalkChunks(s.Payload)
	if err != nil {
		t.Fatal(err)
	}
	cm := sav.ChunkMapOf(chunks)

	labels, err := ParseRailtypeLabels(s.Payload, cm["RAIL"])
	if err != nil {
		t.Fatal(err)
	}
	tiles, err := LoadMapTiles(s.Payload, cm)
	if err != nil {
		t.Fatal(err)
	}

	vehicles, err := ParseVEHS(s.Payload, cm["VEHS"])
	if err != nil {
		t.Fatal(err)
	}

	checked := 0
	for _, v := range vehicles {
		if v.EngineType != 24 && v.EngineType != 328 && v.EngineType != 329 {
			continue
		}
		idx := tiles.RailtypeIndex(v.Tile)
		if int(idx) >= len(labels) {
			t.Fatalf("vehicle %d: railtype index %d out of range", v.VehicleID, idx)
		}
		label := labels[idx]
		if RailtypeFromLabel(label) != RailtypeMono {
			t.Errorf("vehicle %d (engine %d) on tile %d: got railtype %q, want MONO", v.VehicleID, v.EngineType, v.Tile, label)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no affected vehicles found to check -- fixture or filter is wrong")
	}
	t.Logf("checked %d vehicles, all confirmed on MONO track", checked)
}
