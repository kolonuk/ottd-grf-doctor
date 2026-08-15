package ui

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/kolonuk/ottd-grf-doctor/internal/bananas"
	"github.com/kolonuk/ottd-grf-doctor/internal/grf"
)

// TestObjectMatchFlowEndToEnd exercises the full object/scenery matching
// path the interactive TUI drives: select a broken [OBJ] item, open the
// object picker for a downloaded/parsed candidate, and pick an object --
// the same sequence a user drives via Tab/Enter. This save fixture has no
// naturally-broken object GRF (its only real breakage is two train sets
// -- see the project history), so the broken item and its candidate are
// synthesized here; everything downstream (App wiring, promptObjectPicker,
// ObjectMatch/applyAndSave's object path) is the real, unmodified code a
// live session would run.
func TestObjectMatchFlowEndToEnd(t *testing.T) {
	m, err := LoadModel("../../testdata/xpressways-2082/broken.sav")
	if err != nil {
		t.Fatal(err)
	}

	brokenObj := &Item{
		Kind: KindObjectGRF, GRFID: "CAFEBABE", Broken: true,
		ObjectSlots: []int{5, 6},
	}
	m.Items = append([]*Item{brokenObj}, m.Items...)

	candidateID := "DEADBEEF"
	m.ParsedCandidates[candidateID] = &grf.ParsedGRF{
		Objects: []grf.ParsedObject{
			{LocalID: 7, Name: "Test Statue"},
			{LocalID: 3, Name: "Test Lighthouse"},
		},
	}
	candidate := &bananas.ContentInfo{
		ContentID: 1, Name: "Test Object Pack", UniqueID: 0xDEADBEEF,
		Tags: []string{"object"},
	}
	if got := candidate.GRFIDHex(); got != candidateID {
		t.Fatalf("test setup bug: GRFIDHex() = %s, want %s", got, candidateID)
	}

	a := NewApp(m)
	a.selectItem(0)
	if a.selectedItem != brokenObj {
		t.Fatalf("selectItem(0) selected %+v, want the synthetic broken object item", a.selectedItem)
	}
	if a.vehicleList.GetTitle() != " Affected Objects " {
		t.Errorf("vehicleList title = %q, want %q", a.vehicleList.GetTitle(), " Affected Objects ")
	}

	// Drive matchSelectedTo the way a real Enter-on-candidate keypress
	// would (via currentCandidate's index-based lookup, not a direct
	// call), so this also exercises the outer list's candidate-matching
	// plumbing for object items.
	a.catalog = []bananas.ContentInfo{*candidate}
	a.filterCatalog("")
	if a.rightList.GetItemCount() != 1 {
		t.Fatalf("expected the object-tagged candidate to appear in the filtered list, got %d items", a.rightList.GetItemCount())
	}
	a.matchSelectedTo(0)

	if !a.modalOpen {
		t.Fatal("expected the object picker modal to be open after matching an object item to a parsed candidate")
	}
	list, ok := a.tapp.GetFocus().(*tview.List)
	if !ok {
		t.Fatalf("expected focus to be on the object picker's *tview.List, got %T", a.tapp.GetFocus())
	}
	if list.GetItemCount() != 2 {
		t.Fatalf("expected 2 objects in the picker, got %d", list.GetItemCount())
	}

	// Simulate pressing Enter on the first listed object -- the exact
	// dispatch path Application.Run uses (List.InputHandler), not a
	// direct call into private matching logic.
	handler := list.InputHandler()
	handler(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(p tview.Primitive) { a.tapp.SetFocus(p) })

	if brokenObj.ObjectMatch == nil {
		t.Fatal("expected ObjectMatch to be set after picking an object")
	}
	if brokenObj.ObjectMatch.TargetGRFID != candidateID {
		t.Errorf("ObjectMatch.TargetGRFID = %s, want %s", brokenObj.ObjectMatch.TargetGRFID, candidateID)
	}
	if !brokenObj.Matched() {
		t.Error("Item.Matched() should be true once ObjectMatch is set")
	}
	if a.modalOpen {
		t.Error("expected the picker modal to have closed after picking an object")
	}
	if !a.dirty {
		t.Error("expected a.dirty to be set after making a match")
	}
}
