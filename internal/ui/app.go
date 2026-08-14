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
			fmt.Fprintf(&b, "\n[gray]Not matched yet.[-] Select a replacement on the right and press Enter.\n")
			fmt.Fprintf(&b, "[gray]Object matching is best-effort: unresolved slots are left as-is rather than blocking the rest of the fix.[-]\n")
		}
	} else if it.Broken {
		fmt.Fprintf(&b, "[red]Status: BROKEN[-] -- this GRF is referenced by the save but not loaded.\n")
		fmt.Fprintf(&b, "Pool slots: %v\n", it.Slots)
		fmt.Fprintf(&b, "Affected vehicles: %d\n", len(it.Vehicles))
		if it.Match != nil {
			fmt.Fprintf(&b, "\n[green]Matched to:[-] %s (grfid=%s internal_id=%d)\n", it.Match.Name, it.Match.GRFID, it.Match.InternalID)
		} else {
			fmt.Fprintf(&b, "\n[gray]Not matched yet.[-] Select a replacement on the right and press Enter.\n")
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
		return
	}
	if it.Kind == KindObjectGRF {
		for _, o := range it.ObjectInstances {
			a.vehicleList.AddItem(fmt.Sprintf("tile %d", o.Tile), "", 0, nil)
		}
		if len(it.ObjectInstances) > 0 {
			a.showVehicleDetail(0)
		}
		return
	}
	if !it.Broken {
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
	if it.RemovedVehIDs[v.VehicleID] {
		fmt.Fprint(&b, "\n[red]Marked for removal[-] (Enter to undo)\n")
	} else {
		fmt.Fprint(&b, "\n(Enter to mark this vehicle for removal instead of replacement)\n")
	}
	if it.Match != nil {
		candidateRT, hasDate, introYear, retireYear := a.replacementInfo(it.Match)
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
	a.populateVehicleList()
	a.vehicleList.SetCurrentItem(i)
	a.renderGRFDetail()
	a.refreshLeftItemLabel(a.leftList.GetCurrentItem())
}

// replacementInfo looks up everything known about a chosen target
// engine: the default-engine table if it's a base-game engine, the
// dynamically-parsed candidate roster if it's a downloaded third-party
// GRF this session has parsed (see internal/grf), or nothing if neither
// -- e.g. an internal ID typed in manually without downloading first.
func (a *App) replacementInfo(t *engine.TargetEngine) (railtype engine.Railtype, hasDate bool, introYear, retireYear int) {
	if t.GRFID == engine.InvalidGRFID {
		if d, ok := engine.DefaultTrainEngines[t.InternalID]; ok {
			return d.Railtype, true, d.IntroYear, d.RetireYear
		}
		return engine.RailtypeUnknown, false, 0, 0
	}
	if parsed, ok := a.model.ParsedCandidates[t.GRFID]; ok {
		for _, e := range parsed.Engines {
			if e.LocalID != t.InternalID {
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

// candidateMatches decides whether one catalog entry is worth offering as
// a replacement for the currently-selected broken item, and whether it
// matches the search box's text. Without the tag check, every one of the
// ~1200 catalog NewGRFs was offered as a candidate for every broken item
// -- signal sets, town name generators, ships as a train replacement --
// which is the "appear to be ALL grfs, even ones not suitable" complaint
// this fixes. The tag vocabulary (verified against the live catalog: see
// project history) includes "train"/"road-vehicle"/"ship"/"aircraft" for
// vehicles and "object" for scenery -- this tool only detects/fixes
// broken train and object GRFs (see internal/engine's scope), so those
// are the only two categories ever requested here.
func (a *App) candidateMatches(c *bananas.ContentInfo, query string) bool {
	wantTag := "train"
	if a.selectedItem != nil && a.selectedItem.Kind == KindObjectGRF {
		wantTag = "object"
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
	if q == "" {
		return true
	}
	return strings.Contains(strings.ToLower(c.Name), q) || strings.Contains(strings.ToLower(c.Desc), q)
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
				a.setStatus(fmt.Sprintf("[red]Download failed: %v[-]", err))
				return
			}
			grfPath := findGRFFile(files)
			if grfPath == "" {
				a.setStatus("[red]Downloaded, but no .grf file found in the package[-]")
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
				a.setStatus(fmt.Sprintf("[green]Downloaded %s[-] but [yellow]couldn't parse its engine list (%v)[-] -- you'll need to enter the internal ID manually when matching", c.Name, perr))
				return
			}
			a.model.ParsedCandidates[c.GRFIDHex()] = parsed
			a.setStatus(fmt.Sprintf("[green]Downloaded and parsed %s: %d engine(s) found[-] -- ready to match", c.Name, len(parsed.Engines)))
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
	if parsed, ok := a.model.ParsedCandidates[c.GRFIDHex()]; ok && len(parsed.Engines) > 0 {
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
		a.renderGRFDetail()
		a.refreshLeftItemLabel(a.leftList.GetCurrentItem())
		a.closeModal(a.leftList)
	})
	list.SetBorder(true).SetTitle(fmt.Sprintf(" Match %s -> pick object in %s (Esc to cancel) ", it.GRFID, c.Name))
	a.openModal(center(list, 100, 24), list, a.rightList, false)
}

// promptEnginePicker shows the replacement GRF's actual, dynamically-
// parsed engine roster (name, track type, dates, speed/power) and lets
// the user pick one directly -- no blind internal-ID entry, no
// hardcoded per-GRF table (see internal/grf). Each row shows a
// railtype/date warning inline when this tool has enough data to know
// there's a mismatch (see internal/engine/warnings.go); these are
// informational only, matching every other warning in this tool.
func (a *App) promptEnginePicker(c *bananas.ContentInfo, parsed *grf.ParsedGRF, it *Item) {
	var trackAt engine.Railtype
	if len(it.Vehicles) > 0 {
		trackAt = a.model.RailtypeAtTile(it.Vehicles[0].Tile)
	}

	list := tview.NewList().ShowSecondaryText(true)
	list.SetWrapAround(false)
	engines := append([]grf.ParsedEngine(nil), parsed.Engines...)
	sort.Slice(engines, func(i, j int) bool { return engines[i].LocalID < engines[j].LocalID })
	for i := range engines {
		e := &engines[i]
		var warn []string
		candidateRT := RailtypeOfParsedEngine(e)
		for _, w := range engine.CheckRailtypeCompatibility(trackAt, candidateRT) {
			warn = append(warn, w.Message)
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
		if e.HasTrackType {
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

	list.SetSelectedFunc(func(i int, main, sec string, sc rune) {
		e := engines[i]
		it.Match = &engine.TargetEngine{GRFID: c.GRFIDHex(), InternalID: e.LocalID, Name: e.Name}
		a.renderGRFDetail()
		a.populateVehicleList()
		a.refreshLeftItemLabel(a.leftList.GetCurrentItem())
		a.closeModal(a.leftList)
	})
	list.SetBorder(true).SetTitle(fmt.Sprintf(" Match %s -> pick engine in %s (Esc to cancel) ", it.GRFID, c.Name))
	a.openModal(center(list, 100, 24), list, a.rightList, false)
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
		a.tapp.Stop()
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
		a.tapp.Stop()
		return nil
	}
	return event
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
                    - on a Replacement candidate: match it to the selected broken GRF
                    - on an Affected Vehicle: toggle it for removal instead of replacement
[yellow]d[-]                Download and parse the highlighted replacement candidate (right panel only)
[yellow]A[-]                Apply the current plan, lint the result, and write a new save file (never overwrites the original)
[yellow]?[-] or [yellow]h[-]           Show this help screen
[yellow]Esc[-]              Close a popup if one is open, otherwise quit
[yellow]Ctrl-C[-], [yellow]Ctrl-X[-], [yellow]q[-]  Quit

[yellow::b]Panels[-::-]

[yellow]NewGRFs[-] (left)             Every NewGRF this save references. Broken ones (referenced but not
                          loaded) are listed first with a red "!", turning to a green tick once
                          matched. [OBJ] marks an object/scenery GRF rather than a vehicle GRF.
[yellow]GRF Detail[-] (top centre)    Detail of whichever GRF is currently relevant: the selected broken
                          item on the left, or the highlighted candidate on the right -- this pane
                          follows whichever list has focus.
[yellow]Affected Vehicles[-]         Vehicles (or, for [OBJ] items, placed instances) that used the
                          selected broken GRF.
[yellow]Vehicle Detail[-]             Detail of the highlighted vehicle/instance, plus any railtype or
                          in-game-date compatibility warnings against the current match.
[yellow]Replacement candidates[-]     The BaNaNaS catalog, filtered to GRFs tagged for the selected
                          item's kind (trains for a vehicle GRF, objects for an object GRF) --
                          so a broken train set won't offer ships or signal sets as replacements.

[yellow::b]Notes[-::-]

"Internal ID", if you're ever asked for one, means the engine or object's position number
within a NOT-YET-DOWNLOADED GRF file (set by that GRF's own author) -- it has nothing to do
with this save. Download the candidate first ('d') to get a real, named list to pick from
instead of typing that number blind.

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
		if !it.Broken {
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
		newPayload, err = engine.ApplyToPayload(m.Payload, m.EIDS, m.NGRF, m.Vehicles, res, m.PendingGRFs, nil)
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
