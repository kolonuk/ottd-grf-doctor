package ui

import "testing"

func TestLoadModelXpressways(t *testing.T) {
	m, err := LoadModel("../../testdata/xpressways-2082/broken.sav")
	if err != nil {
		t.Fatal(err)
	}
	if m.Year != 2082 {
		t.Errorf("Year = %d, want 2082", m.Year)
	}
	var brokenCount, loadedCount int
	for _, it := range m.Items {
		if it.Broken {
			brokenCount++
			if len(it.Vehicles) == 0 && len(it.Slots) == 0 {
				t.Errorf("broken item %s has no slots or vehicles", it.GRFID)
			}
		} else {
			loadedCount++
			if it.Loaded == nil {
				t.Errorf("non-broken item %s has nil Loaded", it.GRFID)
			}
		}
	}
	if brokenCount != 2 {
		t.Errorf("broken item count = %d, want 2", brokenCount)
	}
	if loadedCount != len(m.NGRF) {
		t.Errorf("loaded item count = %d, want %d (len(NGRF))", loadedCount, len(m.NGRF))
	}
	// Broken items must be listed first (the left-list ordering the
	// design calls for).
	for i, it := range m.Items {
		if i < brokenCount && !it.Broken {
			t.Fatalf("item %d is not broken, but broken items should sort first", i)
		}
		if i >= brokenCount && it.Broken {
			t.Fatalf("item %d is broken but appears after the loaded items", i)
		}
	}

	// Exercise the railtype/warning path end-to-end for a known vehicle.
	for _, it := range m.Items {
		if !it.Broken || len(it.Vehicles) == 0 {
			continue
		}
		v := it.Vehicles[0]
		rt := m.RailtypeAtTile(v.Tile)
		if rt.String() == "" {
			t.Errorf("RailtypeAtTile returned empty string for tile %d", v.Tile)
		}
	}
}
