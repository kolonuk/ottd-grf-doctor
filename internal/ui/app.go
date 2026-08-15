package ui

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/kolonuk/ottd-grf-doctor/internal/bananas"
	"github.com/kolonuk/ottd-grf-doctor/internal/engine"
	"github.com/kolonuk/ottd-grf-doctor/internal/grf"
	"github.com/kolonuk/ottd-grf-doctor/internal/lint"
	"github.com/rivo/tview"
)

// App wires a Model to the tview screens described in the project's
// design: left = every referenced GRF (broken first, ticked once
// matched); top-centre = detail of the selected GRF; bottom-centre =
// detail of the selected vehicle within it; right = a searchable,
// downloadable browser of replacement candidates.
type App struct {
	model *Model
	tapp  *tview.Application

	leftList    *tview.List
	grfDetail   *tview.TextView
	vehicleList *tview.List
	vehicleInfo *tview.TextView
	searchInput *tview.InputField
	rightList   *tview.List
	statusBar   *tview.TextView

	catalog       []bananas.ContentInfo
	catalogStatus string

	selectedItem *Item

	// Modal state. tview routes every key through globalKeys before the
	// focused primitive ever sees it (Application-level SetInputCapture
	// runs first); globalKeys must know a modal is open so it stops
	// hijacking Tab/hotkeys meant for the modal's own List/Form -- see
	// globalKeys' doc comment for the bug this fixes.
	modalOpen          bool
	modalDismissAnyKey bool
	modalClose         func()

	// dirty tracks whether there's matching/removal work that hasn't
	// been applied+saved yet -- set whenever a match or removal is made,
	// cleared on a successful applyAndSave. Standard "unsaved changes"
	// TUI convention: quitting (q/Esc/Ctrl-X) asks for confirmation
	// while dirty instead of silently discarding the session's work.
	// Ctrl-C is deliberately left as tview's unconditional hard-quit, as
	// is conventional for terminal apps.
	dirty bool
}

// NewApp builds the App around an already-loaded Model.
func NewApp(m *Model) *App {
	a := &App{model: m, tapp: tview.NewApplication()}
	a.build()
	return a
}

// Run starts the terminal UI event loop; it blocks until the user quits.
func (a *App) Run() error {
	go a.loadCatalog()
	a.tapp.EnableMouse(true)
	return a.tapp.SetRoot(a.rootLayout(), true).SetFocus(a.leftList).Run()
}

func (a *App) build() {
	a.leftList = tview.NewList().ShowSecondaryText(true)
	a.leftList.SetBorder(true).SetTitle(" NewGRFs (broken first) ")
	a.leftList.SetWrapAround(false)

	a.grfDetail = tview.NewTextView().SetDynamicColors(true).SetWrap(true)
	a.grfDetail.SetBorder(true).SetTitle(" GRF Detail ")

	a.vehicleList = tview.NewList().ShowSecondaryText(false)
	a.vehicleList.SetBorder(true).SetTitle(" Affected Vehicles ")
	a.vehicleList.SetWrapAround(false)

	a.vehicleInfo = tview.NewTextView().SetDynamicColors(true).SetWrap(true)
	a.vehicleInfo.SetBorder(true).SetTitle(" Vehicle Detail ")

	a.searchInput = tview.NewInputField().SetLabel("Search: ")
	a.rightList = tview.NewList().ShowSecondaryText(true)
	a.rightList.SetBorder(true).SetTitle(" Replacement candidates ")
	a.rightList.SetWrapAround(false)

	a.statusBar = tview.NewTextView().SetDynamicColors(true)

	a.populateLeftList()

	// SetFocusFunc fires every time a primitive gains focus, however that
	// happened -- initial Run(), Tab-cycling, or a modal closing -- so
	// wiring the status-bar/detail-pane refresh here means every call
	// site that moves focus gets it for free instead of needing its own
	// explicit update call (see updateStatusHelp/showCandidateDetail).
	a.leftList.SetFocusFunc(func() {
		a.renderGRFDetail()
		a.updateStatusHelp()
	})
	a.vehicleList.SetFocusFunc(func() { a.updateStatusHelp() })
	a.searchInput.SetFocusFunc(func() { a.updateStatusHelp() })
	a.rightList.SetFocusFunc(func() {
		a.showCandidateDetail(a.rightList.GetCurrentItem())
		a.updateStatusHelp()
	})

	a.leftList.SetChangedFunc(func(i int, main, sec string, sc rune) {
		a.selectItem(i)
	})
	a.vehicleList.SetChangedFunc(func(i int, main, sec string, sc rune) {
		a.showVehicleDetail(i)
	})
	a.vehicleList.SetSelectedFunc(func(i int, main, sec string, sc rune) {
		a.toggleRemoveVehicle(i)
	})
	a.searchInput.SetChangedFunc(func(text string) { a.filterCatalog(text) })
	a.rightList.SetChangedFunc(func(i int, main, sec string, sc rune) {
		a.showCandidateDetail(i)
	})
	a.rightList.SetSelectedFunc(func(i int, main, sec string, sc rune) {
		a.matchSelectedTo(i)
	})

	a.tapp.SetInputCapture(a.globalKeys)

	if len(a.model.Items) > 0 {
		a.selectItem(0)
	}
	a.updateStatusHelp()
}

func (a *App) rootLayout() tview.Primitive {
	centre := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.grfDetail, 0, 1, false).
		AddItem(tview.NewFlex().
			AddItem(a.vehicleList, 0, 1, false).
			AddItem(a.vehicleInfo, 0, 1, false), 0, 1, false)

	right := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.searchInput, 1, 0, false).
		AddItem(a.rightList, 0, 1, false)

	main := tview.NewFlex().
		AddItem(a.leftList, 0, 1, true).
		AddItem(centre, 0, 2, false).
		AddItem(right, 0, 1, false)

	return tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(main, 0, 1, true).
		AddItem(a.statusBar, 1, 0, false)
}

func (a *App) setStatus(s string) { a.statusBar.SetText(s) }

// --- left panel -------------------------------------------------------

// itemLabelAndSec computes one left-list row's display text. Shared by
// populateLeftList (initial build) and refreshLeftItemLabel (targeted
// update) so the two never drift out of sync.
func itemLabelAndSec(it *Item) (label, sec string) {
	mark := "  "
	if it.Broken {
		mark = "[red]![-]"
		if it.Matched() {
			mark = "[green]✓[-]" // tick
		}
	} else {
		mark = "[gray]ok[-]"
	}
	kindTag := ""
	if it.Kind == KindObjectGRF {
		kindTag = "[OBJ] "
	}
	label = fmt.Sprintf("%s %s%s", mark, kindTag, it.GRFID)
	if it.Kind == KindObjectGRF {
		sec = fmt.Sprintf("%d object type(s), %d instance(s)", len(it.ObjectSlots), len(it.ObjectInstances))
	} else if it.Broken {
		sec = fmt.Sprintf("%d slot(s), %d vehicle(s)", len(it.Slots), len(it.Vehicles))
	} else if it.Loaded != nil {
		sec = shorten(it.Loaded.Filename, 40)
	}
	return label, sec
}

func (a *App) populateLeftList() {
	a.leftList.Clear()
	for _, it := range a.model.Items {
		label, sec := itemLabelAndSec(it)
		a.leftList.AddItem(label, sec, 0, nil)
	}
}

// refreshLeftItemLabel updates a single row's text in place via
// SetItemText, which touches neither the list's current-item index nor
// its "changed" callback. populateLeftList's old approach (Clear then
// rebuild) reset the widget's internal cursor to 0 on every refresh; if
// the refreshed row wasn't item 0, the subsequent SetCurrentItem(cur)
// restoring it would itself fire the "changed" callback (0 != cur),
// re-entering selectItem -> populateVehicleList and silently snapping the
// *vehicle* list's cursor back to its own top -- the exact cursor-reset
// bug reported against toggleRemoveVehicle. A targeted SetItemText has no
// such side effects.
func (a *App) refreshLeftItemLabel(index int) {
	if index < 0 || index >= len(a.model.Items) {
		return
	}
	label, sec := itemLabelAndSec(a.model.Items[index])
	a.leftList.SetItemText(index, label, sec)
}

func (a *App) selectItem(i int) {
	if i < 0 || i >= len(a.model.Items) {
		return
	}
	a.selectedItem = a.model.Items[i]
	a.renderGRFDetail()
	a.populateVehicleList()
	// The set of suitable replacement candidates depends on this item's
	// kind (trains for a broken vehicle GRF, objects for a broken object
	// GRF) -- see candidateMatches -- so re-filter whenever the left-list
	// selection changes, not just when the search text changes.
	a.filterCatalog(a.searchInput.GetText())
}

// --- centre: GRF detail -------------------------------------------------

func (a *App) renderGRFDetail() {
	it := a.selectedItem
	if it == nil {
		a.grfDetail.SetText("")
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[yellow]GRFID:[-] %s\n", it.GRFID)
	if it.Kind == KindObjectGRF {
		fmt.Fprintf(&b, "[red]Status: BROKEN (object/scenery GRF)[-] -- referenced by OBID but not loaded.\n")
		fmt.Fprintf(&b, "Object type slots: %v\n", it.ObjectSlots)
		fmt.Fprintf(&b, "Placed instances: %d\n", len(it.ObjectInstances))
		if it.ObjectMatch != nil {
			fmt.Fprintf(&b, "\n[green]Matched to:[-] grfid=%s entity_id=%d\n", it.ObjectMatch.TargetGRFID, it.ObjectMatch.TargetEntity)
		} else {
			fmt.Fprint(&b, "\n[gray]Not matched yet.[-] To fix:\n"+
				"[gray] 1.[-] Tab to [yellow]Replacement candidates[-] (right) and search/browse --\n"+
				"    only GRFs tagged \"object\" are shown\n"+
				"[gray] 2.[-] Press [yellow]d[-] to download and parse it\n"+
				"[gray] 3.[-] Press [yellow]Enter[-] on it to pick a specific object\n")
			fmt.Fprint(&b, "[gray]Object matching is best-effort: unresolved slots are left as-is rather than blocking the rest of the fix.[-]\n")
		}
	} else if it.Broken {
		fmt.Fprintf(&b, "[red]Status: BROKEN[-] -- this GRF is referenced by the save but not loaded.\n")
		fmt.Fprintf(&b, "Vehicle kind: %s\n", vehicleKindLabel(it.VehicleKind))
		if len(it.OtherVehicles) > 0 {
			fmt.Fprintf(&b, "Pool slots: %v\n", it.OtherSlots)
			fmt.Fprintf(&b, "Affected %s: %d\n", strings.ToLower(vehicleKindLabel(it.VehicleKind)), len(it.OtherVehicles))
		} else {
			fmt.Fprintf(&b, "Pool slots: %v\n", it.Slots)
			fmt.Fprintf(&b, "Affected vehicles: %d\n", len(it.Vehicles))
		}
		if it.Match != nil {
			fmt.Fprintf(&b, "\n[green]Matched to:[-] %s (grfid=%s internal_id=%d)\n", it.Match.Name, it.Match.GRFID, it.Match.InternalID)
		} else {
			compareLine := "[gray] 3.[-] Press [yellow]Enter[-] on it to open the engine picker\n"
			if it.VehicleKind == engine.VehTrain {
				compareLine = "[gray] 3.[-] Press [yellow]Enter[-] on it to open the engine picker, compared\n" +
					"    side-by-side against what this vehicle currently looks like\n"
			}
			fmt.Fprint(&b, "\n[gray]Not matched yet.[-] To fix:\n"+
				"[gray] 1.[-] Tab to [yellow]Replacement candidates[-] (right) and search/browse --\n"+
				"    only GRFs tagged \""+vehicleKindTag(it.VehicleKind)+"\" are shown\n"+
				"[gray] 2.[-] Press [yellow]d[-] to download and parse it (or skip this and\n"+
				"    press Enter directly if you already know the internal ID)\n"+
				compareLine)
		}
		if n := len(it.RemovedVehIDs); n > 0 {
			fmt.Fprintf(&b, "%d vehicle(s) marked for removal instead of replacement.\n", n)
		}
	} else if it.Loaded != nil {
		fmt.Fprintf(&b, "[green]Status: loaded[-]\n")
		fmt.Fprintf(&b, "Filename: %s\n", it.Loaded.Filename)
		fmt.Fprintf(&b, "Version: %d\n", it.Loaded.Version)
	}
	a.grfDetail.SetText(b.String())
}

// --- centre: vehicle list/detail ----------------------------------------

func (a *App) populateVehicleList() {
	a.vehicleList.Clear()
	a.vehicleInfo.SetText("")
	it := a.selectedItem
	if it == nil {
		a.vehicleList.SetTitle(" Affected Vehicles ")
		return
	}
	if it.Kind == KindObjectGRF {
		a.vehicleList.SetTitle(" Affected Objects ")
		for _, o := range it.ObjectInstances {
			a.vehicleList.AddItem(fmt.Sprintf("tile %d", o.Tile), "", 0, nil)
		}
		if len(it.ObjectInstances) > 0 {
			a.showVehicleDetail(0)
		}
		return
	}
	a.vehicleList.SetTitle(" Affected " + vehicleKindLabel(it.VehicleKind) + " ")
	if !it.Broken {
		return
	}
	if len(it.OtherVehicles) > 0 {
		// Road vehicles/ships/aircraft support matching but not removal
		// (see Item.OtherVehicles' doc comment), so no per-row mark here.
		for _, v := range it.OtherVehicles {
			a.vehicleList.AddItem(fmt.Sprintf("  #%d", v.UnitNumber), "", 0, nil)
		}
		a.showVehicleDetail(0)
		return
	}
	for _, v := range it.Vehicles {
		mark := " "
		if it.RemovedVehIDs[v.VehicleID] {
			mark = "[red]x[-]"
		}
		name := fmt.Sprintf("#%d", v.UnitNumber)
		a.vehicleList.AddItem(fmt.Sprintf("%s %s", mark, name), "", 0, nil)
	}
	if len(it.Vehicles) > 0 {
		a.showVehicleDetail(0)
	}
}

// vehicleKindLabel is the display name for a VehicleKind, used in panel
// titles ("Affected Trains", "Affected Aircraft", ...).
func vehicleKindLabel(k engine.VehicleType) string {
	switch k {
	case engine.VehTrain:
		return "Trains"
	case engine.VehRoad:
		return "Road Vehicles"
	case engine.VehShip:
		return "Ships"
	case engine.VehAircraft:
		return "Aircraft"
	default:
		return "Vehicles"
	}
}

func (a *App) showVehicleDetail(i int) {
	it := a.selectedItem
	if it == nil {
		a.vehicleInfo.SetText("")
		return
	}
	if it.Kind == KindObjectGRF {
		a.showObjectInstanceDetail(i)
		return
	}
	if len(it.OtherVehicles) > 0 {
		a.showOtherVehicleDetail(i)
		return
	}
	if i < 0 || i >= len(it.Vehicles) {
		a.vehicleInfo.SetText("")
		return
	}
	v := it.Vehicles[i]
	var b strings.Builder
	fmt.Fprintf(&b, "[yellow]Unit #:[-] %d\n", v.UnitNumber)
	fmt.Fprintf(&b, "[yellow]Cargo type:[-] %d   [yellow]Capacity:[-] %d\n", v.CargoType, v.CargoCap)
	fmt.Fprintf(&b, "[yellow]Tile:[-] %d\n", v.Tile)
	rt := a.model.RailtypeAtTile(v.Tile)
	fmt.Fprintf(&b, "[yellow]Track here:[-] %s\n", rt)
	// The vehicle's TRUE original engine (whatever the missing GRF
	// defined) can't be recovered -- but OpenTTD substitutes the closest
	// base-game default engine so it still renders as something, and
	// that substitute's real stats are a reasonable proxy for "what this
	// looks like right now" to compare replacement candidates against.
	if sub, ok := engine.SubstituteEngineFor(a.model.EIDS, v.EngineType); ok {
		fmt.Fprintf(&b, "[yellow]Currently displayed as:[-] %s (speed=%d power=%d", sub.Name, sub.Speed, sub.Power)
		if sub.RetireYear != 0 {
			fmt.Fprintf(&b, " intro=%d retire=%d", sub.IntroYear, sub.RetireYear)
		} else {
			fmt.Fprintf(&b, " intro=%d never retires", sub.IntroYear)
		}
		fmt.Fprint(&b, ")\n")
	}
	if it.RemovedVehIDs[v.VehicleID] {
		fmt.Fprint(&b, "\n[red]Marked for removal[-] (Enter to undo)\n")
	} else {
		fmt.Fprint(&b, "\n(Enter to mark this vehicle for removal instead of replacement)\n")
	}
	if it.Match != nil {
		candidateRT, hasDate, introYear, retireYear := a.replacementInfo(it.Match, it.VehicleKind)
		for _, w := range engine.CheckRailtypeCompatibility(rt, candidateRT) {
			fmt.Fprintf(&b, "\n[orange]Warning:[-] %s\n", w.Message)
		}
		if hasDate {
			for _, w := range engine.CheckEngineDateAvailability(a.model.Year, introYear, retireYear) {
				fmt.Fprintf(&b, "\n[orange]Warning:[-] %s\n", w.Message)
			}
		}
	}
	a.vehicleInfo.SetText(b.String())
}

// showOtherVehicleDetail is showVehicleDetail's equivalent for road
// vehicles/ships/aircraft (see Item.OtherVehicles' doc comment for why
// they don't share TrainVehicle's removal/multiheaded-pairing/railtype-
// at-tile concerns): unit, cargo, and -- if a match has been made --
// date-availability warnings against it. No track/infrastructure
// compatibility check is done here (this tool doesn't parse road/
// waterway/airport tile data the way it does rail).
func (a *App) showOtherVehicleDetail(i int) {
	it := a.selectedItem
	if it == nil || i < 0 || i >= len(it.OtherVehicles) {
		a.vehicleInfo.SetText("")
		return
	}
	v := it.OtherVehicles[i]
	var b strings.Builder
	fmt.Fprintf(&b, "[yellow]Unit #:[-] %d\n", v.UnitNumber)
	fmt.Fprintf(&b, "[yellow]Cargo type:[-] %d   [yellow]Capacity:[-] %d\n", v.CargoType, v.CargoCap)
	fmt.Fprintf(&b, "[yellow]Tile:[-] %d\n", v.Tile)
	// No "currently displayed as" comparison here: engine.SubstituteEngineFor
	// only has default-engine data for trains (see internal/engine/defaults.go's
	// scope), so it can't be shown accurately for road vehicles/ships/aircraft.
	if it.Match != nil {
		_, hasDate, introYear, retireYear := a.replacementInfo(it.Match, it.VehicleKind)
		if hasDate {
			for _, w := range engine.CheckEngineDateAvailability(a.model.Year, introYear, retireYear) {
				fmt.Fprintf(&b, "\n[orange]Warning:[-] %s\n", w.Message)
			}
		}
	}
	a.vehicleInfo.SetText(b.String())
}

// showObjectInstanceDetail renders one placed object instance (tile,
// footprint, build date, town) in the same bottom-centre pane vehicle
// detail uses. Object instances have no per-instance removal/replacement
// state -- unlike vehicles, fixing a broken object GRF is a single
// GRF-level OBID repoint (see ApplyObjectSwaps), not a per-instance choice.
func (a *App) showObjectInstanceDetail(i int) {
	it := a.selectedItem
	if it == nil || i < 0 || i >= len(it.ObjectInstances) {
		a.vehicleInfo.SetText("")
		return
	}
	o := it.ObjectInstances[i]
	var b strings.Builder
	fmt.Fprintf(&b, "[yellow]Tile:[-] %d\n", o.Tile)
	fmt.Fprintf(&b, "[yellow]Footprint:[-] %dx%d\n", o.Width, o.Height)
	fmt.Fprintf(&b, "[yellow]Build date (day count):[-] %d\n", o.BuildDate)
	fmt.Fprintf(&b, "[yellow]Colour:[-] %d   [yellow]View:[-] %d\n", o.Colour, o.View)
	fmt.Fprintf(&b, "[yellow]Object type slot:[-] %d\n", o.ObjectType)
	a.vehicleInfo.SetText(b.String())
}

func (a *App) toggleRemoveVehicle(i int) {
	it := a.selectedItem
	if it == nil || it.Kind == KindObjectGRF || i < 0 || i >= len(it.Vehicles) {
		return
	}
	id := it.Vehicles[i].VehicleID
	if it.RemovedVehIDs[id] {
		delete(it.RemovedVehIDs, id)
	} else {
		it.RemovedVehIDs[id] = true
	}
	a.dirty = true
	a.populateVehicleList()
	a.vehicleList.SetCurrentItem(i)
	a.renderGRFDetail()
	a.refreshLeftItemLabel(a.leftList.GetCurrentItem())
}

// replacementInfo looks up everything known about a chosen target
// engine: the default-engine table if it's a base-game engine (trains
// only -- this tool's default-engine data was only ever mined for
// trains, see internal/engine/defaults.go), the dynamically-parsed
// candidate roster if it's a downloaded third-party GRF this session has
// parsed (see internal/grf), or nothing if neither -- e.g. an internal
// ID typed in manually without downloading first. vehicleKind narrows
// the parsed-candidate lookup to the right feature, since local IDs are
// only unique within one vehicle type (grf.ParsedEngine.Feature uses the
// same 0..3 encoding as engine.VehicleType, verified against both
// packages' source).
func (a *App) replacementInfo(t *engine.TargetEngine, vehicleKind engine.VehicleType) (railtype engine.Railtype, hasDate bool, introYear, retireYear int) {
	if t.GRFID == engine.InvalidGRFID {
		if vehicleKind == engine.VehTrain {
			if d, ok := engine.DefaultTrainEngines[t.InternalID]; ok {
				return d.Railtype, true, d.IntroYear, d.RetireYear
			}
		}
		return engine.RailtypeUnknown, false, 0, 0
	}
	if parsed, ok := a.model.ParsedCandidates[t.GRFID]; ok {
		for _, e := range parsed.Engines {
			if e.LocalID != t.InternalID || e.Feature != uint8(vehicleKind) {
				continue
			}
			rt := RailtypeOfParsedEngine(&e)
			if !e.HasIntroDate {
				return rt, false, 0, 0
			}
			introYear := grf.DayCountToYear(e.IntroDate)
			retireYear := 0
			if e.HasModelLife && e.ModelLife != 255 {
				retireYear = introYear + int(e.ModelLife)
			}
			return rt, true, introYear, retireYear
		}
	}
	return engine.RailtypeUnknown, false, 0, 0
}

// --- right panel: replacement browser ------------------------------------

func (a *App) loadCatalog() {
	a.catalogStatus = "Loading NewGRF catalog from content.openttd.org..."
	a.tapp.QueueUpdateDraw(func() { a.setStatus(a.catalogStatus) })

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	items, err := a.model.Bananas.ListNewGRFs(ctx, 5*time.Second)
	if err != nil {
		a.catalogStatus = fmt.Sprintf("[red]Catalog load failed: %v[-] (browse/download unavailable, matching still works manually)", err)
		a.tapp.QueueUpdateDraw(func() { a.setStatus(a.catalogStatus) })
		return
	}
	a.catalog = items
	a.catalogStatus = fmt.Sprintf("Catalog loaded: %d NewGRFs. Type to search.", len(items))
	a.tapp.QueueUpdateDraw(func() {
		a.setStatus(a.catalogStatus)
		a.filterCatalog(a.searchInput.GetText())
	})
}

// countEnginesOfKind counts a parsed GRF's engines matching the given
// vehicle kind (grf.ParsedEngine.Feature uses the same 0..3 encoding as
// engine.VehicleType) -- used to decide whether the real engine-picker
// UI has anything to show for this item's specific vehicle type, versus
// falling back to promptInternalID.
func countEnginesOfKind(parsed *grf.ParsedGRF, kind engine.VehicleType) int {
	n := 0
	for _, e := range parsed.Engines {
		if e.Feature == uint8(kind) {
			n++
		}
	}
	return n
}

// vehicleKindTag maps a VehicleType to the BaNaNaS catalog tag that
// identifies GRFs defining that kind of vehicle (verified against the
// live catalog: see project history).
func vehicleKindTag(k engine.VehicleType) string {
	switch k {
	case engine.VehRoad:
		return "road-vehicle"
	case engine.VehShip:
		return "ship"
	case engine.VehAircraft:
		return "aircraft"
	default:
		return "train"
	}
}

// candidateMatches decides whether one catalog entry is worth offering as
// a replacement for the currently-selected broken item, and whether it
// matches the search box's text. Without the tag check, every one of the
// ~1200 catalog NewGRFs was offered as a candidate for every broken item
// -- signal sets, town name generators, ships as a train replacement --
// which is the "appear to be ALL grfs, even ones not suitable" complaint
// this fixes.
//
// This deliberately does NOT hide candidates based on track compatibility
// (an earlier version of this filter did, and it had a bad interaction:
// pressing 'd' to download and check a candidate would make it vanish
// from the list the instant it turned out incompatible, which is exactly
// the wrong moment to lose it -- you just asked to look at it). Track
// compatibility is surfaced instead, as a warning tag on the row -- see
// filterCatalog -- once the candidate is actually known (parsed).
func (a *App) candidateMatches(c *bananas.ContentInfo, query string) bool {
	wantTag := "train"
	if a.selectedItem != nil {
		if a.selectedItem.Kind == KindObjectGRF {
			wantTag = "object"
		} else {
			wantTag = vehicleKindTag(a.selectedItem.VehicleKind)
		}
	}
	tagged := false
	for _, t := range c.Tags {
		if t == wantTag {
			tagged = true
			break
		}
	}
	if !tagged {
		return false
	}
	q := strings.ToLower(strings.TrimSpace(query))
	if q != "" && !strings.Contains(strings.ToLower(c.Name), q) && !strings.Contains(strings.ToLower(c.Desc), q) {
		return false
	}
	return true
}

// candidateHasCompatibleTrack reports whether c is known (downloaded and
// parsed this session) to have at least one engine whose track type
// matches the broken vehicle's actual current track. The bool return
// distinguishes "known incompatible" from "not parsed yet, can't tell" --
// only the former should ever be flagged to the user.
func (a *App) candidateHasCompatibleTrack(c *bananas.ContentInfo) (known, compatible bool) {
	if a.selectedItem == nil || a.selectedItem.Kind != KindVehicleGRF || len(a.selectedItem.Vehicles) == 0 {
		return false, false
	}
	parsed, ok := a.model.ParsedCandidates[c.GRFIDHex()]
	if !ok {
		return false, false
	}
	trackAt := a.model.RailtypeAtTile(a.selectedItem.Vehicles[0].Tile)
	for i := range parsed.Engines {
		if engineTrackCompatible(trackAt, RailtypeOfParsedEngine(&parsed.Engines[i])) {
			return true, true
		}
	}
	return true, false
}

func (a *App) filterCatalog(query string) {
	a.rightList.Clear()
	shown := 0
	for i := range a.catalog {
		c := &a.catalog[i]
		if !a.candidateMatches(c, query) {
			continue
		}
		sec := fmt.Sprintf("grfid=%s v%s", c.GRFIDHex(), c.Version)
		if known, compatible := a.candidateHasCompatibleTrack(c); known && !compatible {
			sec += "  [orange]! no engine matches this track[-]"
		}
		a.rightList.AddItem(c.Name, sec, 0, nil)
		shown++
		if shown >= 200 {
			break // keep the list responsive; narrow the search for more
		}
	}
	if a.tapp.GetFocus() == a.rightList {
		a.showCandidateDetail(a.rightList.GetCurrentItem())
	}
}

// currentCandidate maps the right list's selection back into a.catalog,
// respecting the same filter filterCatalog applied.
func (a *App) currentCandidate(listIndex int) *bananas.ContentInfo {
	if len(a.catalog) == 0 {
		return nil
	}
	query := a.searchInput.GetText()
	idx := 0
	for i := range a.catalog {
		c := &a.catalog[i]
		if !a.candidateMatches(c, query) {
			continue
		}
		if idx == listIndex {
			return c
		}
		idx++
	}
	return nil
}

// showCandidateDetail renders the right list's currently highlighted
// replacement candidate into the shared top-centre detail pane. Combined
// with build()'s SetFocusFunc/SetChangedFunc wiring on both leftList and
// rightList, this is what makes that pane alternate between the broken
// item's detail and the highlighted candidate's detail as focus and
// selection move between the two lists.
func (a *App) showCandidateDetail(i int) {
	c := a.currentCandidate(i)
	if c == nil {
		if len(a.catalog) == 0 {
			a.grfDetail.SetText("[gray]Catalog still loading...[-]")
		} else {
			a.grfDetail.SetText("[gray]No matching candidates for this item.[-]")
		}
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[yellow]%s[-]\n", c.Name)
	fmt.Fprintf(&b, "[yellow]GRFID:[-] %s   [yellow]Version:[-] %s\n", c.GRFIDHex(), c.Version)
	if len(c.Tags) > 0 {
		fmt.Fprintf(&b, "[yellow]Tags:[-] %s\n", strings.Join(c.Tags, ", "))
	}
	if c.Desc != "" {
		fmt.Fprintf(&b, "\n%s\n", c.Desc)
	}
	if _, ok := a.model.ParsedCandidates[c.GRFIDHex()]; ok {
		fmt.Fprint(&b, "\n[green]Downloaded and parsed[-] -- Enter shows its real engine/object list.\n")
	} else {
		fmt.Fprint(&b, "\n[gray]Not downloaded yet.[-] Press 'd' to download and parse it -- Enter still\nworks without downloading, but falls back to asking for a raw internal ID.\n")
	}
	a.grfDetail.SetText(b.String())
}

func (a *App) downloadSelected() {
	idx := a.rightList.GetCurrentItem()
	c := a.currentCandidate(idx)
	if c == nil {
		return
	}
	a.setStatus(fmt.Sprintf("Downloading %s...", c.Name))
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		destDir := "grf-downloads/" + c.GRFIDHex()
		files, err := a.model.Bananas.Download(ctx, c.ContentID, destDir)
		a.tapp.QueueUpdateDraw(func() {
			if err != nil {
				a.showError("Download failed", fmt.Sprintf("Couldn't download %s:\n\n%v", c.Name, err))
				return
			}
			grfPath := findGRFFile(files)
			if grfPath == "" {
				a.showError("Download failed", fmt.Sprintf("Downloaded %s, but the package contained no .grf file.", c.Name))
				return
			}
			// ApplyToPayload/insertNGRF hashes the file itself when the
			// plan is applied (see internal/engine.MD5File) -- what
			// matters here is queuing the right path and identity.
			a.model.PendingGRFs = append(a.model.PendingGRFs, engine.NewGRFToInsert{
				LocalPath: grfPath,
				Filename:  c.GRFIDHex() + ".grf",
				GRFID:     c.GRFIDHex(),
				Version:   1,
				Palette:   9,
			})

			// Dynamically parse the actual GRF binary so matching can
			// show real engines (name, track type, dates, speed, power)
			// instead of asking the user to type an internal ID blind --
			// see internal/grf.
			parsed, perr := grf.ParseGRF(grfPath)
			if perr != nil {
				a.setStatus(fmt.Sprintf("[green]Downloaded %s[-] but [yellow]couldn't parse its engine list[-] -- see popup for details", c.Name))
				a.showError("Couldn't parse "+c.Name,
					fmt.Sprintf("%v\n\nThe download itself succeeded and is queued to insert into the save -- "+
						"you can still match it manually (Enter on it will ask for the internal engine ID directly).", perr))
				return
			}
			a.model.ParsedCandidates[c.GRFIDHex()] = parsed
			a.setStatus(fmt.Sprintf("[green]Downloaded and parsed %s: %d engine(s) found[-] -- ready to match", c.Name, len(parsed.Engines)))
			a.filterCatalog(a.searchInput.GetText()) // a newly-parsed candidate can now show its track-compatibility warning
		})
	}()
}

func findGRFFile(paths []string) string {
	for _, p := range paths {
		if strings.HasSuffix(strings.ToLower(p), ".grf") {
			return p
		}
	}
	return ""
}

func (a *App) matchSelectedTo(rightIdx int) {
	it := a.selectedItem
	if it == nil || !it.Broken {
		a.setStatus("[red]Select a broken item on the left first.[-]")
		return
	}
	c := a.currentCandidate(rightIdx)
	if c == nil {
		return
	}
	if it.Kind == KindObjectGRF {
		parsed, ok := a.model.ParsedCandidates[c.GRFIDHex()]
		if !ok || len(parsed.Objects) == 0 {
			a.setStatus("[red]That GRF hasn't been downloaded/parsed yet, or has no object specs -- download it first (see 'd').[-]")
			return
		}
		a.promptObjectPicker(c, parsed, it)
		return
	}
	if parsed, ok := a.model.ParsedCandidates[c.GRFIDHex()]; ok && countEnginesOfKind(parsed, it.VehicleKind) > 0 {
		a.promptEnginePicker(c, parsed, it)
		return
	}
	a.promptInternalID(c, it)
}

// openModal shows content as a popup and gives it focus. From then on,
// globalKeys stops treating Tab/'q'/'d'/'A'/'?'/'h' as global hotkeys
// (a.modalOpen) so they reach the modal's own List/Form navigation
// instead -- previously Tab was hijacked by cycleFocus() even with a
// modal open, silently pulling focus back to the background left list,
// so every keypress after that looked like it was hitting an
// unresponsive popup when it was actually reaching the panel behind it.
// Escape/Ctrl-X call modalClose (closing just the modal, not the app);
// dismissAnyKey closes it on any key at all, for a plain help screen.
func (a *App) openModal(content, focus tview.Primitive, cancelFocus tview.Primitive, dismissAnyKey bool) {
	a.modalOpen = true
	a.modalDismissAnyKey = dismissAnyKey
	a.modalClose = func() { a.closeModal(cancelFocus) }

	pages := tview.NewPages()
	pages.AddPage("background", a.rootLayout(), true, true)
	pages.AddPage("modal", content, true, true)
	a.tapp.SetRoot(pages, true).SetFocus(focus)
}

// closeModal restores the main screen and focuses the given primitive.
// Call this directly from a modal's own success/cancel handlers (buttons,
// SetSelectedFunc); modalClose (set by openModal) wraps it for the
// Escape/Ctrl-X path handled centrally in globalKeys.
func (a *App) closeModal(focus tview.Primitive) {
	a.modalOpen = false
	a.modalDismissAnyKey = false
	a.modalClose = nil
	a.tapp.SetRoot(a.rootLayout(), true).SetFocus(focus)
}

// promptObjectPicker is the object/scenery equivalent of
// promptEnginePicker: shows the replacement GRF's actual, dynamically-
// parsed object roster and lets the user pick one directly. Selecting an
// object sets it.ObjectMatch, which applyAndSave turns into an
// engine.ObjectAssignment repointing every one of this item's OBID slots
// at the chosen (grfid, entity_id) -- see ApplyObjectSwaps.
func (a *App) promptObjectPicker(c *bananas.ContentInfo, parsed *grf.ParsedGRF, it *Item) {
	list := tview.NewList().ShowSecondaryText(true)
	list.SetWrapAround(false)
	objects := append([]grf.ParsedObject(nil), parsed.Objects...)
	sort.Slice(objects, func(i, j int) bool { return objects[i].LocalID < objects[j].LocalID })
	for i := range objects {
		o := &objects[i]
		sec := fmt.Sprintf("id=%d", o.LocalID)
		if o.HasIntroDate {
			sec += fmt.Sprintf("  intro=%d", grf.DayCountToYear(o.IntroDate))
			retireYear := 0
			if o.HasEndOfLifeDate && o.EndOfLifeDate != 0 {
				retireYear = grf.DayCountToYear(o.EndOfLifeDate)
			}
			for _, w := range engine.CheckEngineDateAvailability(a.model.Year, grf.DayCountToYear(o.IntroDate), retireYear) {
				sec += "  [orange]! " + w.Message + "[-]"
			}
		}
		name := o.Name
		if name == "" {
			name = fmt.Sprintf("object #%d", o.LocalID)
		}
		list.AddItem(name, sec, 0, nil)
	}

	list.SetSelectedFunc(func(i int, main, sec string, sc rune) {
		o := objects[i]
		entity := uint8(o.LocalID)
		if o.LocalID > 0xFF {
			// Only reachable for a GRF using the SLV_EXTEND_ENTITY_MAPPING
			// wide-entity encoding this save predates -- not expected in
			// practice for this tool's target saves, but fail loudly
			// rather than silently truncate the ID.
			a.setStatus(fmt.Sprintf("[red]Object local ID %d doesn't fit this save's 1-byte entity_id field -- can't match automatically.[-]", o.LocalID))
			a.closeModal(a.leftList)
			return
		}
		it.ObjectMatch = &engine.ObjectAssignment{Slots: it.ObjectSlots, TargetGRFID: c.GRFIDHex(), TargetEntity: entity}
		a.dirty = true
		a.renderGRFDetail()
		a.refreshLeftItemLabel(a.leftList.GetCurrentItem())
		a.closeModal(a.leftList)
	})
	list.SetBorder(true).SetTitle(fmt.Sprintf(" Match %s -> pick object in %s (Esc to cancel) ", it.GRFID, c.Name))
	a.openModal(center(list, 100, 24), list, a.rightList, false)
}

// engineTrackCompatible reports whether a candidate engine's track type
// is known to be usable on the vehicle's actual track. Deliberately more
// permissive than CheckRailtypeCompatibility's warning trigger (which
// also flags an *unknown* candidate track as worth a warning): here,
// "unknown" on either side means "can't judge" and stays visible by
// default, only a confirmed mismatch between two known railtypes is
// filtered out -- see promptEnginePicker's 't' toggle for seeing those
// too.
func engineTrackCompatible(actual, candidate engine.Railtype) bool {
	if actual == engine.RailtypeUnknown || candidate == engine.RailtypeUnknown {
		return true
	}
	return actual == candidate
}

// promptEnginePicker shows the replacement GRF's actual, dynamically-
// parsed engine roster (name, track type, dates, speed/power) for the
// item's specific vehicle kind (grf.ParsedEngine.Feature) and lets the
// user pick one directly -- no blind internal-ID entry, no hardcoded
// per-GRF table (see internal/grf). A header above the list shows what
// the broken vehicle currently displays as for trains (its EIDS
// substitute default engine, see engine.SubstituteEngineFor -- this
// tool only has default-engine data for trains, so road vehicles/ships/
// aircraft skip that line) for a side-by-side comparison against each
// candidate row's own speed/power/dates. For trains, only engines whose
// track type matches the broken vehicle's actual track are listed by
// default (or every engine, if either side's track type isn't known);
// road vehicles/ships/aircraft always show every engine (this tool has
// no tile-level road/waterway/airport data to compare against) -- 't'
// toggles showing the full roster for trains too, including confirmed
// mismatches, with the same inline warning tags shown for those.
func (a *App) promptEnginePicker(c *bananas.ContentInfo, parsed *grf.ParsedGRF, it *Item) {
	var trackAt engine.Railtype
	var affectedCount int
	var repCargoType uint8
	var repCargoCap uint16

	header := tview.NewTextView().SetDynamicColors(true).SetWrap(true)
	header.SetBorder(true).SetTitle(" Comparison ")
	var h strings.Builder
	switch {
	case len(it.Vehicles) > 0:
		rep := &it.Vehicles[0]
		trackAt = a.model.RailtypeAtTile(rep.Tile)
		affectedCount, repCargoType, repCargoCap = len(it.Vehicles), rep.CargoType, rep.CargoCap
		fmt.Fprintf(&h, "[yellow]This save's track here:[-] %s   [yellow]Cargo:[-] type %d, capacity %d   [yellow]Affected vehicles:[-] %d\n",
			trackAt, repCargoType, repCargoCap, affectedCount)
		if sub, ok := engine.SubstituteEngineFor(a.model.EIDS, rep.EngineType); ok {
			retire := fmt.Sprintf("retire=%d", sub.RetireYear)
			if sub.RetireYear == 0 {
				retire = "never retires"
			}
			fmt.Fprintf(&h, "[yellow]Currently displayed as:[-] %s -- track=%s speed=%d power=%d intro=%d %s",
				sub.Name, sub.Railtype, sub.Speed, sub.Power, sub.IntroYear, retire)
		} else {
			fmt.Fprint(&h, "[gray]No current-engine data available for comparison.[-]")
		}
	case len(it.OtherVehicles) > 0:
		rep := &it.OtherVehicles[0]
		affectedCount, repCargoType, repCargoCap = len(it.OtherVehicles), rep.CargoType, rep.CargoCap
		fmt.Fprintf(&h, "[yellow]Cargo:[-] type %d, capacity %d   [yellow]Affected %s:[-] %d\n",
			repCargoType, repCargoCap, strings.ToLower(vehicleKindLabel(it.VehicleKind)), affectedCount)
		fmt.Fprint(&h, "[gray]No current-engine data available for comparison (default-engine data is only mined for trains).[-]")
	}
	header.SetText(h.String())

	var engines []grf.ParsedEngine
	for _, e := range parsed.Engines {
		if e.Feature == uint8(it.VehicleKind) {
			engines = append(engines, e)
		}
	}
	sort.Slice(engines, func(i, j int) bool { return engines[i].LocalID < engines[j].LocalID })

	list := tview.NewList().ShowSecondaryText(true)
	list.SetWrapAround(false)

	showAll := false
	var shown []grf.ParsedEngine
	render := func() {
		list.Clear()
		shown = shown[:0]
		hidden := 0
		isTrain := it.VehicleKind == engine.VehTrain
		for i := range engines {
			e := &engines[i]
			var candidateRT engine.Railtype
			if isTrain {
				candidateRT = RailtypeOfParsedEngine(e)
				if !showAll && !engineTrackCompatible(trackAt, candidateRT) {
					hidden++
					continue
				}
			}
			shown = append(shown, *e)

			var warn []string
			if isTrain {
				for _, w := range engine.CheckRailtypeCompatibility(trackAt, candidateRT) {
					warn = append(warn, w.Message)
				}
			}
			if e.HasIntroDate {
				introYear := grf.DayCountToYear(e.IntroDate)
				retireYear := 0
				if e.HasModelLife && e.ModelLife != 255 {
					retireYear = introYear + int(e.ModelLife)
				}
				for _, w := range engine.CheckEngineDateAvailability(a.model.Year, introYear, retireYear) {
					warn = append(warn, w.Message)
				}
			}
			sec := fmt.Sprintf("id=%d", e.LocalID)
			if isTrain && e.HasTrackType {
				sec += "  track=" + candidateRT.String()
			}
			if e.HasIntroDate {
				sec += fmt.Sprintf("  intro=%d", grf.DayCountToYear(e.IntroDate))
			}
			if e.HasSpeed {
				sec += fmt.Sprintf("  speed=%d", e.Speed)
			}
			if e.HasPower {
				sec += fmt.Sprintf("  power=%d", e.Power)
			}
			if len(warn) > 0 {
				sec += "  [orange]! " + strings.Join(warn, "; ") + "[-]"
			}
			list.AddItem(e.Name, sec, 0, nil)
		}
		title := fmt.Sprintf(" Match %s -> pick engine in %s ", it.GRFID, c.Name)
		if showAll {
			title += fmt.Sprintf("[showing all %d, 't' for track-matches only] ", len(engines))
		} else if hidden > 0 {
			title += fmt.Sprintf("[%d hidden by track mismatch, 't' to show all] ", hidden)
		}
		title += "(Esc to cancel) "
		list.SetTitle(title)
	}
	render()

	list.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Rune() == 't' {
			showAll = !showAll
			cur := list.GetCurrentItem()
			render()
			list.SetCurrentItem(cur)
			return nil
		}
		return event
	})
	list.SetSelectedFunc(func(i int, main, sec string, sc rune) {
		if i < 0 || i >= len(shown) {
			return
		}
		e := shown[i]
		it.Match = &engine.TargetEngine{GRFID: c.GRFIDHex(), InternalID: e.LocalID, Name: e.Name}
		a.dirty = true
		a.renderGRFDetail()
		a.populateVehicleList()
		a.refreshLeftItemLabel(a.leftList.GetCurrentItem())
		a.closeModal(a.leftList)
	})
	list.SetBorder(true)

	flex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(header, 5, 0, false).
		AddItem(list, 0, 1, true)
	a.openModal(center(flex, 110, 30), list, a.rightList, false)
}

// promptInternalID is the fallback when a candidate GRF hasn't been
// downloaded (and therefore dynamically parsed) yet: it asks for the
// specific engine's "internal ID" directly -- a NewGRF concept meaning
// the engine's position number within that GRF file (0, 1, 2... in the
// order its author defined them), NOT anything to do with this save or
// this tool. It's normally found in the GRF's own readme/changelog, or by
// trial in-game. Download the GRF first (see downloadSelected, 'd' on the
// right-hand list) to get the real engine-picker experience instead,
// which reads this straight from the GRF's own data instead of asking.
func (a *App) promptInternalID(c *bananas.ContentInfo, it *Item) {
	form := tview.NewForm()
	form.AddTextView("", "This GRF hasn't been downloaded, so its real engine list isn't known\n"+
		"yet. \"Internal ID\" means the engine's position number within\n"+
		"the GRF file itself (0, 1, 2...), set by the GRF's author -- it's\n"+
		"unrelated to this save. Check the GRF's readme, or press Esc and\n"+
		"'d' on the right-hand list to download it and pick from a real list.",
		64, 5, false, false)
	idField := tview.NewInputField().SetLabel("Internal ID: ").SetText("0")
	form.AddFormItem(idField)
	form.AddButton("OK", func() {
		id, err := strconv.Atoi(strings.TrimSpace(idField.GetText()))
		if err != nil || id < 0 || id > 0xFFFF {
			a.setStatus("[red]Invalid internal ID[-]")
			a.closeModal(a.rightList)
			return
		}
		it.Match = &engine.TargetEngine{GRFID: c.GRFIDHex(), InternalID: uint16(id), Name: c.Name}
		a.dirty = true
		for _, w := range engine.CheckDateAvailability(a.model.Year, c.Desc) {
			a.setStatus(fmt.Sprintf("[orange]Warning:[-] %s", w.Message))
		}
		a.renderGRFDetail()
		a.populateVehicleList()
		a.refreshLeftItemLabel(a.leftList.GetCurrentItem())
		a.closeModal(a.leftList)
	})
	form.AddButton("Cancel", func() {
		a.closeModal(a.rightList)
	})
	form.SetBorder(true).SetTitle(" Match " + it.GRFID + " -> " + c.Name + " ")
	a.openModal(center(form, 66, 15), form, a.rightList, false)
}

func center(p tview.Primitive, width, height int) tview.Primitive {
	return tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(p, height, 0, true).
			AddItem(nil, 0, 1, false), width, 0, true).
		AddItem(nil, 0, 1, false)
}

// --- global keys / apply ------------------------------------------------

// globalKeys is the Application-level SetInputCapture: every key event
// passes through here first, before tview's normal focus-based routing.
// While a.modalOpen, it deliberately does almost nothing -- see openModal
// for the real bug this fixes: Tab used to be unconditionally hijacked to
// cycle the three main panels even while a modal (a match picker, the
// help screen) was open and using Tab for its own navigation, silently
// yanking focus back to the background left list, so the modal looked
// unresponsive to every keypress after that (they were actually landing
// on the panel behind it). Escape/Ctrl-X close just the modal; Ctrl-C is
// left to tview's own built-in hard-quit handling (untouched, and it
// fires regardless of modal state, same as any terminal app's Ctrl-C).
func (a *App) globalKeys(event *tcell.EventKey) *tcell.EventKey {
	if a.modalOpen {
		if a.modalDismissAnyKey {
			if a.modalClose != nil {
				a.modalClose()
			}
			return nil
		}
		switch event.Key() {
		case tcell.KeyEscape, tcell.KeyCtrlX:
			if a.modalClose != nil {
				a.modalClose()
			}
			return nil
		}
		return event
	}

	switch event.Rune() {
	case 'q':
		a.confirmQuit()
		return nil
	case 'd':
		if a.tapp.GetFocus() == a.rightList {
			a.downloadSelected()
			return nil
		}
	case 'A':
		a.applyAndSave()
		return nil
	case '?', 'h':
		// Guarded off the search box so typing a literal '?' or 'h' into
		// a search query still works.
		if a.tapp.GetFocus() != a.searchInput {
			a.showHelp()
			return nil
		}
	}
	switch event.Key() {
	case tcell.KeyTab:
		a.cycleFocus()
		return nil
	case tcell.KeyEscape, tcell.KeyCtrlX:
		a.confirmQuit()
		return nil
	}
	return event
}

// confirmQuit quits immediately if there's nothing unapplied to lose;
// otherwise it asks first ('y' to quit anyway, 'n'/Escape to go back) --
// standard "unsaved changes" behavior, since matching/removal work only
// becomes durable once 'A' (apply+lint+save) has run.
func (a *App) confirmQuit() {
	if !a.dirty {
		a.tapp.Stop()
		return
	}
	returnTo := a.tapp.GetFocus()
	text := tview.NewTextView().SetDynamicColors(true).SetWrap(true)
	text.SetBorder(true).SetTitle(" Quit without applying? ")
	text.SetText("[yellow]You have matches or removals set that haven't been applied yet[-]\n" +
		"(press 'A' on the main screen to apply, lint, and save them first).\n\n" +
		"Quit anyway and lose them?  [green]y[-]/[red]n[-]")
	text.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Rune() {
		case 'y', 'Y':
			a.tapp.Stop()
		case 'n', 'N':
			a.closeModal(returnTo)
		}
		return nil // swallow everything else while this prompt is up
	})
	a.openModal(center(text, 70, 9), text, returnTo, false)
}

func (a *App) cycleFocus() {
	order := []tview.Primitive{a.leftList, a.vehicleList, a.searchInput, a.rightList}
	cur := a.tapp.GetFocus()
	for i, p := range order {
		if p == cur {
			a.tapp.SetFocus(order[(i+1)%len(order)])
			return
		}
	}
	a.tapp.SetFocus(a.leftList)
}

// updateStatusHelp sets the status bar to a short hotkey reference for
// whichever panel currently has focus. Wired via SetFocusFunc in build(),
// so every path that moves focus -- Tab-cycling, initial startup, a modal
// closing -- gets the status bar updated for free, without each of those
// call sites needing its own explicit refresh.
func (a *App) updateStatusHelp() {
	const common = "  [yellow]?[-] help  [yellow]Esc/q[-] quit"
	switch a.tapp.GetFocus() {
	case a.leftList:
		a.setStatus("[yellow]Tab[-] next panel  [yellow]↑↓[-] browse GRFs  [yellow]A[-] apply+lint+save" + common)
	case a.vehicleList:
		a.setStatus("[yellow]Tab[-] next panel  [yellow]↑↓[-] browse vehicles  [yellow]Enter[-] toggle remove" + common)
	case a.searchInput:
		a.setStatus("[yellow]Tab[-] next panel  type to search candidates  [yellow]Esc/Ctrl-C[-] quit")
	case a.rightList:
		a.setStatus("[yellow]Tab[-] next panel  [yellow]↑↓[-] browse candidates  [yellow]Enter[-] match  [yellow]d[-] download" + common)
	default:
		a.setStatus("[yellow]?[-] help" + common)
	}
}

// helpText is the full-screen help screen's content (see showHelp).
const helpText = `[yellow::b]grfdoctor -- keybindings[-::-]

[yellow]Tab[-]              Switch between panels (left GRFs, affected vehicles/objects, search box, replacement candidates)
[yellow]Up/Down[-]         Move within the focused list (stops at the ends -- no wraparound)
[yellow]Enter[-]           Context-dependent:
                    - on a Replacement candidate: open the engine/object picker to match it
                      to the selected broken GRF
                    - on an Affected Vehicle: toggle it for removal instead of replacement
[yellow]d[-]                Download and parse the highlighted replacement candidate (right panel only) --
                    once parsed, a candidate with no engine on this vehicle's actual track
                    gets a "! no engine matches this track" tag (it stays in the list, just
                    marked, so downloading something to check it never makes it vanish)
[yellow]t[-]                Inside the engine picker only: toggle showing engines whose track doesn't
                    match (hidden by default)
[yellow]A[-]                Apply the current plan, lint the result, and write a new save file (never overwrites the original)
[yellow]?[-] or [yellow]h[-]           Show this help screen
[yellow]Esc[-]              Close a popup if one is open, otherwise quit (asks first if you have
                    unapplied matches/removals)
[yellow]Ctrl-C[-]            Quit immediately, no confirmation (the usual terminal "just stop" key)
[yellow]Ctrl-X[-], [yellow]q[-]        Quit (asks first if you have unapplied matches/removals)
Mouse               Click to select/focus; scroll to move within a list

[yellow::b]Fixing a broken GRF, step by step[-::-]

 1. Select the broken GRF on the left (red "!") -- its affected vehicles/instances and pool
    slots show in the centre and bottom-centre panes. The left list also says what kind it
    is (train, road vehicle, ship, aircraft, or [OBJ] object/scenery).
 2. Tab to Replacement candidates (right) and search/browse for a suitable GRF -- only GRFs
    tagged for that same kind are ever shown (a broken aircraft set won't offer trains).
 3. Press [yellow]d[-] to download and parse the candidate -- this reads its real engine/object list
    (names, dates, speed, power, and for trains, track type) straight from the .grf file.
 4. Press [yellow]Enter[-] on it to open the picker and choose a specific replacement. For trains,
    the picker also shows a comparison header (what the broken vehicle currently displays
    as -- its substitute default engine's stats) to help pick a real like-for-like
    replacement instead of guessing; this comparison isn't available for the other three
    vehicle kinds (this tool's default-engine data is train-only).
 5. Repeat for every broken GRF, then press [yellow]A[-] to apply, lint, and write a new save file --
    your original is never modified.

[yellow::b]Panels[-::-]

[yellow]NewGRFs[-] (left)             Every NewGRF this save references. Broken ones (referenced but not
                          loaded) are listed first with a red "!", turning to a green tick once
                          matched. [OBJ] marks an object/scenery GRF rather than a vehicle GRF.
[yellow]GRF Detail[-] (top centre)    Detail of whichever GRF is currently relevant: the selected broken
                          item on the left, or the highlighted candidate on the right -- this pane
                          follows whichever list has focus.
[yellow]Affected Trains/Road Vehicles/Ships/Aircraft/Objects[-]
                          Vehicles (or, for [OBJ] items, placed instances) that used the
                          selected broken GRF -- the title says which kind. Only trains
                          support removing an individual vehicle instead of replacing it
                          (road vehicles/ships/aircraft/objects only support matching).
[yellow]Vehicle Detail[-]             Detail of the highlighted vehicle/instance -- including what it
                          currently displays as for trains (see step 4 above) -- plus any
                          railtype or in-game-date compatibility warnings against the
                          current match.
[yellow]Replacement candidates[-]     The BaNaNaS catalog, filtered to GRFs tagged for the selected
                          item's exact kind -- so a broken train set won't offer ships,
                          road vehicles, aircraft, or scenery as replacements.

[yellow::b]Notes[-::-]

"Internal ID", if you're ever asked for one, means the engine or object's position number
within a NOT-YET-DOWNLOADED GRF file (set by that GRF's own author) -- it has nothing to do
with this save. Download the candidate first ('d') to get a real, named list to pick from
instead of typing that number blind.

Some older GRFs use "container format 1", which this tool's parser doesn't read -- you'll
see a clear popup explaining that if it happens, and can still match manually via internal ID.

[gray](press any key to close)[-]`

// showHelp shows a full-screen keybinding reference, dismissed by any
// key (see openModal's dismissAnyKey).
func (a *App) showHelp() {
	returnTo := a.tapp.GetFocus()
	text := tview.NewTextView().SetDynamicColors(true).SetWrap(true)
	text.SetBorder(true).SetTitle(" Help ")
	text.SetText(helpText)
	a.openModal(text, text, returnTo, true)
}

// showError shows a dismissible, full-detail error popup for failures
// with more to explain than the one-line status bar can hold clearly
// without truncating -- e.g. a container-format-1 GRF's explanation of
// what that means and what to do instead. Dismissed by any key.
func (a *App) showError(title, detail string) {
	returnTo := a.tapp.GetFocus()
	text := tview.NewTextView().SetDynamicColors(true).SetWrap(true)
	text.SetBorder(true).SetTitle(" " + title + " ")
	text.SetText("[red]" + detail + "[-]\n\n[gray](press any key to close)[-]")
	a.openModal(center(text, 90, 16), text, returnTo, true)
}

func shorten(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// --- apply/lint/save ------------------------------------------------------

// applyAndSave builds an engine.Plan from every Item's current match/
// removal state, applies it, lints the result, and writes a new save
// file (never overwriting the original). Any lint error aborts the write
// -- see internal/lint's doc comment on why that check exists.
func (a *App) applyAndSave() {
	m := a.model
	plan := &engine.Plan{}
	var unmatchedBroken []string
	for _, it := range m.Items {
		if !it.Broken || it.Kind == KindObjectGRF {
			continue
		}
		if len(it.OtherVehicles) > 0 {
			// Road vehicles/ships/aircraft support matching but not
			// removal -- see Item.OtherVehicles' doc comment.
			if it.Match == nil {
				unmatchedBroken = append(unmatchedBroken, it.GRFID)
				continue
			}
			var vids []int
			for _, v := range it.OtherVehicles {
				vids = append(vids, v.VehicleID)
			}
			plan.Assignments = append(plan.Assignments, engine.NewAssignment(vids, *it.Match))
			continue
		}
		var remainVehIDs []int
		var removeVehIDs []int
		for _, v := range it.Vehicles {
			if it.RemovedVehIDs[v.VehicleID] {
				removeVehIDs = append(removeVehIDs, v.VehicleID)
			} else {
				remainVehIDs = append(remainVehIDs, v.VehicleID)
			}
		}
		if len(removeVehIDs) > 0 {
			plan.Assignments = append(plan.Assignments, engine.NewRemoval(removeVehIDs))
		}
		if len(remainVehIDs) > 0 {
			if it.Match == nil {
				unmatchedBroken = append(unmatchedBroken, it.GRFID)
				continue
			}
			plan.Assignments = append(plan.Assignments, engine.NewAssignment(remainVehIDs, *it.Match))
		}
	}
	if len(unmatchedBroken) > 0 {
		a.setStatus(fmt.Sprintf("[red]Cannot apply: %d GRF(s) still unmatched: %s[-]",
			len(unmatchedBroken), strings.Join(unmatchedBroken, ", ")))
		return
	}

	// Object/scenery GRFs are fixed independently of the vehicle plan
	// above (see ObjectAssignment's doc comment for why no shared slot
	// allocation is needed). Best-effort by design: an item left
	// unmatched just stays broken rather than blocking the whole apply --
	// see the user-facing note in renderGRFDetail.
	var objectAssignments []engine.ObjectAssignment
	for _, it := range m.Items {
		if it.Kind == KindObjectGRF && it.ObjectMatch != nil {
			objectAssignments = append(objectAssignments, *it.ObjectMatch)
		}
	}

	if len(plan.Assignments) == 0 && len(objectAssignments) == 0 {
		a.setStatus("[yellow]Nothing to apply.[-]")
		return
	}

	var newPayload []byte
	if len(plan.Assignments) > 0 {
		res, err := engine.Apply(m.Analysis, plan)
		if err != nil {
			a.setStatus(fmt.Sprintf("[red]Plan error: %v[-]", err))
			return
		}
		newPayload, err = engine.ApplyToPayload(m.Payload, m.EIDS, m.NGRF, m.Vehicles, m.OtherVehicles, res, m.PendingGRFs, nil)
		if err != nil {
			a.setStatus(fmt.Sprintf("[red]Apply failed: %v[-]", err))
			return
		}
	} else {
		newPayload = m.Payload
	}

	if len(objectAssignments) > 0 {
		var err error
		newPayload, err = engine.ApplyObjectSwaps(newPayload, objectAssignments)
		if err != nil {
			a.setStatus(fmt.Sprintf("[red]Object swap failed: %v[-]", err))
			return
		}
	}

	outPath := outputPath(m.Path)
	out := *m.Save
	out.Payload = newPayload
	if err := out.SaveTo(outPath); err != nil {
		a.setStatus(fmt.Sprintf("[red]Write failed: %v[-]", err))
		return
	}
	a.dirty = false // the plan is now durably on disk at outPath regardless of the lint result below

	report, err := lint.Lint(outPath)
	if err != nil {
		a.setStatus(fmt.Sprintf("[red]Lint could not run: %v[-]", err))
		return
	}
	if report.HasErrors() {
		var msgs []string
		for _, f := range report.Findings {
			if f.Severity == "error" {
				msgs = append(msgs, f.Message)
			}
		}
		a.setStatus(fmt.Sprintf("[red]Wrote %s but it FAILED linting: %s[-]", outPath, strings.Join(msgs, "; ")))
		return
	}
	a.setStatus(fmt.Sprintf("[green]Applied, linted clean, and wrote %s[-]", outPath))
}

func outputPath(inPath string) string {
	if strings.HasSuffix(inPath, ".sav") {
		return inPath[:len(inPath)-4] + ".fixed.sav"
	}
	return inPath + ".fixed.sav"
}
