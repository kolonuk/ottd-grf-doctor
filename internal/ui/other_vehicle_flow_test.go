package ui

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/kolonuk/ottd-grf-doctor/internal/bananas"
	"github.com/kolonuk/ottd-grf-doctor/internal/engine"
	"github.com/kolonuk/ottd-grf-doctor/internal/grf"
)

// TestAircraftMatchFlowEndToEnd exercises the full non-train vehicle
// matching path the interactive TUI drives -- select a broken aircraft
// item, filter the catalog to aircraft-tagged candidates only, open the
// engine picker for a downloaded/parsed candidate (containing engines of
// several different features, to prove cross-feature filtering works),
// and pick one. This save fixture has no naturally-broken road vehicle/
// ship/aircraft GRF (only its two train sets are actually broken -- see
// project history, and internal/engine's own
// TestOtherVehiclesDetectAndFixEndToEnd for the equivalent proof at the
// detect/apply layer), so the broken item is synthesized here; the App
// wiring exercised (candidateMatches, matchSelectedTo, promptEnginePicker,
// applyAndSave's OtherVehicles branch) is the real, unmodified code a
// live session would run.
func TestAircraftMatchFlowEndToEnd(t *testing.T) {
	m, err := LoadModel("../../testdata/xpressways-2082/broken.sav")
	if err != nil {
		t.Fatal(err)
	}

	brokenAircraft := &Item{
		Kind: KindVehicleGRF, GRFID: "CAFEBABE", Broken: true,
		VehicleKind: engine.VehAircraft,
		OtherSlots:  []int{500},
		OtherVehicles: []engine.OtherVehicle{
			{VehicleID: 42, Kind: engine.VehAircraft, UnitNumber: 1, CargoType: 0, CargoCap: 100},
		},
	}
	m.Items = append([]*Item{brokenAircraft}, m.Items...)

	candidateID := "DEADBEEF"
	// Deliberately include engines of other features too, to prove the
	// picker only ever shows the item's own vehicle kind (aircraft) --
	// see countEnginesOfKind/promptEnginePicker's feature filtering.
	m.ParsedCandidates[candidateID] = &grf.ParsedGRF{
		Engines: []grf.ParsedEngine{
			{LocalID: 1, Feature: 0, Name: "Not an aircraft (train)"},
			{LocalID: 2, Feature: 1, Name: "Not an aircraft (road)"},
			{LocalID: 10, Feature: 3, Name: "Test Biplane", HasSpeed: true, Speed: 150},
			{LocalID: 11, Feature: 3, Name: "Test Jet", HasSpeed: true, Speed: 900},
		},
	}
	candidate := &bananas.ContentInfo{
		ContentID: 1, Name: "Test Aircraft Pack", UniqueID: 0xDEADBEEF,
		Tags: []string{"aircraft"},
	}
	if got := candidate.GRFIDHex(); got != candidateID {
		t.Fatalf("test setup bug: GRFIDHex() = %s, want %s", got, candidateID)
	}
	// A train-tagged decoy that must NOT show up once an aircraft item is
	// selected -- proves candidateMatches' per-kind tag filtering.
	decoy := bananas.ContentInfo{ContentID: 2, Name: "Decoy Train Pack", UniqueID: 0x11111111, Tags: []string{"train"}}

	a := NewApp(m)
	a.selectItem(0)
	if a.selectedItem != brokenAircraft {
		t.Fatalf("selectItem(0) selected %+v, want the synthetic broken aircraft item", a.selectedItem)
	}
	if got := a.vehicleList.GetTitle(); got != " Affected Aircraft " {
		t.Errorf("vehicleList title = %q, want %q", got, " Affected Aircraft ")
	}

	a.catalog = []bananas.ContentInfo{*candidate, decoy}
	a.filterCatalog("")
	if got := a.rightList.GetItemCount(); got != 1 {
		t.Fatalf("expected only the aircraft-tagged candidate to appear (decoy train excluded), got %d items", got)
	}

	a.matchSelectedTo(0)
	if !a.modalOpen {
		t.Fatal("expected the engine picker modal to be open after matching an aircraft item to a parsed candidate")
	}
	list, ok := a.tapp.GetFocus().(*tview.List)
	if !ok {
		t.Fatalf("expected focus to be on the engine picker's *tview.List, got %T", a.tapp.GetFocus())
	}
	if got := list.GetItemCount(); got != 2 {
		t.Fatalf("expected 2 aircraft engines in the picker (train/road engines filtered out), got %d", got)
	}

	// Simulate pressing Enter on the first listed engine -- the exact
	// dispatch path Application.Run uses (List.InputHandler), not a
	// direct call into private matching logic.
	handler := list.InputHandler()
	handler(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(p tview.Primitive) { a.tapp.SetFocus(p) })

	if brokenAircraft.Match == nil {
		t.Fatal("expected Match to be set after picking an engine")
	}
	if brokenAircraft.Match.GRFID != candidateID {
		t.Errorf("Match.GRFID = %s, want %s", brokenAircraft.Match.GRFID, candidateID)
	}
	if !brokenAircraft.Matched() {
		t.Error("Item.Matched() should be true once Match is set")
	}
	if a.modalOpen {
		t.Error("expected the picker modal to have closed after picking an engine")
	}
	if !a.dirty {
		t.Error("expected a.dirty to be set after making a match")
	}
}
