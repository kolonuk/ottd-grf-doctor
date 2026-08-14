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

	a.grfDetail = tview.NewTextView().SetDynamicColors(true).SetWrap(true)
	a.grfDetail.SetBorder(true).SetTitle(" GRF Detail ")

	a.vehicleList = tview.NewList().ShowSecondaryText(false)
	a.vehicleList.SetBorder(true).SetTitle(" Affected Vehicles (Enter: toggle remove) ")

	a.vehicleInfo = tview.NewTextView().SetDynamicColors(true).SetWrap(true)
	a.vehicleInfo.SetBorder(true).SetTitle(" Vehicle Detail ")

	a.searchInput = tview.NewInputField().SetLabel("Search: ")
	a.rightList = tview.NewList().ShowSecondaryText(true)
	a.rightList.SetBorder(true).SetTitle(" Replacement candidates ")

	a.statusBar = tview.NewTextView().SetDynamicColors(true)
	a.setStatus("[yellow]Tab[-] switch panel  [yellow]Enter[-] select  [yellow]m[-] set as replacement  [yellow]d[-] download  [yellow]r[-] remove instead  [yellow]A[-] apply+lint+save  [yellow]q[-] quit")

	a.populateLeftList()

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
	a.rightList.SetSelectedFunc(func(i int, main, sec string, sc rune) {
		a.matchSelectedTo(i)
	})

	a.tapp.SetInputCapture(a.globalKeys)

	if len(a.model.Items) > 0 {
		a.selectItem(0)
	}
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

func (a *App) populateLeftList() {
	a.leftList.Clear()
	for _, it := range a.model.Items {
		mark := "  "
		if it.Broken {
			mark = "[red]![-]"
			if it.Matched() {
				mark = "[green]✓[-]" // tick
			}
		} else {
			mark = "[gray]ok[-]"
		}
		label := fmt.Sprintf("%s %s", mark, it.GRFID)
		sec := ""
		if it.Broken {
			sec = fmt.Sprintf("%d slot(s), %d vehicle(s)", len(it.Slots), len(it.Vehicles))
		} else if it.Loaded != nil {
			sec = shorten(it.Loaded.Filename, 40)
		}
		a.leftList.AddItem(label, sec, 0, nil)
	}
}

func (a *App) selectItem(i int) {
	if i < 0 || i >= len(a.model.Items) {
		return
	}
	a.selectedItem = a.model.Items[i]
	a.renderGRFDetail()
	a.populateVehicleList()
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
	if it.Broken {
		fmt.Fprintf(&b, "[red]Status: BROKEN[-] -- this GRF is referenced by the save but not loaded.\n")
		fmt.Fprintf(&b, "Pool slots: %v\n", it.Slots)
		fmt.Fprintf(&b, "Affected vehicles: %d\n", len(it.Vehicles))
		if it.Match != nil {
			fmt.Fprintf(&b, "\n[green]Matched to:[-] %s (grfid=%s internal_id=%d)\n", it.Match.Name, it.Match.GRFID, it.Match.InternalID)
		} else {
			fmt.Fprintf(&b, "\n[gray]Not matched yet.[-] Select a replacement on the right, then press 'm'.\n")
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
	if it == nil || !it.Broken {
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
	if it == nil || i < 0 || i >= len(it.Vehicles) {
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

func (a *App) toggleRemoveVehicle(i int) {
	it := a.selectedItem
	if it == nil || i < 0 || i >= len(it.Vehicles) {
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
	a.refreshLeftLabel()
}

func (a *App) refreshLeftLabel() {
	cur := a.leftList.GetCurrentItem()
	a.populateLeftList()
	a.leftList.SetCurrentItem(cur)
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

func (a *App) filterCatalog(query string) {
	a.rightList.Clear()
	q := strings.ToLower(strings.TrimSpace(query))
	shown := 0
	for i := range a.catalog {
		c := &a.catalog[i]
		if q != "" && !strings.Contains(strings.ToLower(c.Name), q) &&
			!strings.Contains(strings.ToLower(c.Desc), q) {
			continue
		}
		sec := fmt.Sprintf("grfid=%s v%s", c.GRFIDHex(), c.Version)
		a.rightList.AddItem(c.Name, sec, 0, nil)
		shown++
		if shown >= 200 {
			break // keep the list responsive; narrow the search for more
		}
	}
}

// currentCandidate maps the right list's selection back into a.catalog,
// respecting the same filter filterCatalog applied.
func (a *App) currentCandidate(listIndex int) *bananas.ContentInfo {
	if len(a.catalog) == 0 {
		return nil
	}
	q := strings.ToLower(strings.TrimSpace(a.searchInput.GetText()))
	idx := 0
	for i := range a.catalog {
		c := &a.catalog[i]
		if q != "" && !strings.Contains(strings.ToLower(c.Name), q) &&
			!strings.Contains(strings.ToLower(c.Desc), q) {
			continue
		}
		if idx == listIndex {
			return c
		}
		idx++
	}
	return nil
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
	if parsed, ok := a.model.ParsedCandidates[c.GRFIDHex()]; ok && len(parsed.Engines) > 0 {
		a.promptEnginePicker(c, parsed, it)
		return
	}
	a.promptInternalID(c, it)
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

	pages := tview.NewPages()
	list.SetSelectedFunc(func(i int, main, sec string, sc rune) {
		e := engines[i]
		it.Match = &engine.TargetEngine{GRFID: c.GRFIDHex(), InternalID: e.LocalID, Name: e.Name}
		a.renderGRFDetail()
		a.populateVehicleList()
		a.refreshLeftLabel()
		a.tapp.SetRoot(a.rootLayout(), true).SetFocus(a.leftList)
	})
	list.SetBorder(true).SetTitle(fmt.Sprintf(" Match %s -> pick engine in %s (Esc to cancel) ", it.GRFID, c.Name))
	list.SetDoneFunc(func() {
		a.tapp.SetRoot(a.rootLayout(), true).SetFocus(a.rightList)
	})
	modal := center(list, 100, 24)
	pages.AddPage("background", a.rootLayout(), true, true)
	pages.AddPage("modal", modal, true, true)
	a.tapp.SetRoot(pages, true).SetFocus(list)
}

// promptInternalID is the fallback when a candidate GRF hasn't been
// downloaded (and therefore dynamically parsed) yet: it asks for the
// specific engine's internal GRF-local ID directly. Download the GRF
// first (see downloadSelected) to get the real engine-picker experience
// instead -- this is typically found in the GRF's own documentation, or
// by trial in-game.
func (a *App) promptInternalID(c *bananas.ContentInfo, it *Item) {
	form := tview.NewForm()
	idField := tview.NewInputField().SetLabel("Internal engine ID in " + c.Name + ": ").SetText("0")
	form.AddFormItem(idField)
	pages := tview.NewPages()
	form.AddButton("OK", func() {
		id, err := strconv.Atoi(strings.TrimSpace(idField.GetText()))
		if err != nil || id < 0 || id > 0xFFFF {
			a.setStatus("[red]Invalid internal engine ID[-]")
			pages.RemovePage("modal")
			a.tapp.SetRoot(a.rootLayout(), true).SetFocus(a.rightList)
			return
		}
		it.Match = &engine.TargetEngine{GRFID: c.GRFIDHex(), InternalID: uint16(id), Name: c.Name}
		for _, w := range engine.CheckDateAvailability(a.model.Year, c.Desc) {
			a.setStatus(fmt.Sprintf("[orange]Warning:[-] %s", w.Message))
		}
		a.renderGRFDetail()
		a.populateVehicleList()
		a.refreshLeftLabel()
		a.tapp.SetRoot(a.rootLayout(), true).SetFocus(a.leftList)
	})
	form.AddButton("Cancel", func() {
		a.tapp.SetRoot(a.rootLayout(), true).SetFocus(a.rightList)
	})
	form.SetBorder(true).SetTitle(" Match " + it.GRFID + " -> " + c.Name + " ")
	modal := center(form, 60, 9)
	pages.AddPage("background", a.rootLayout(), true, true)
	pages.AddPage("modal", modal, true, true)
	a.tapp.SetRoot(pages, true).SetFocus(form)
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

func (a *App) globalKeys(event *tcell.EventKey) *tcell.EventKey {
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
	}
	if event.Key() == tcell.KeyTab {
		a.cycleFocus()
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
	if len(plan.Assignments) == 0 {
		a.setStatus("[yellow]Nothing to apply.[-]")
		return
	}

	res, err := engine.Apply(m.Analysis, plan)
	if err != nil {
		a.setStatus(fmt.Sprintf("[red]Plan error: %v[-]", err))
		return
	}
	newPayload, err := engine.ApplyToPayload(m.Payload, m.EIDS, m.NGRF, m.Vehicles, res, m.PendingGRFs, nil)
	if err != nil {
		a.setStatus(fmt.Sprintf("[red]Apply failed: %v[-]", err))
		return
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
