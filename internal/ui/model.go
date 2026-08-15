// Package ui implements the interactive terminal UI: a left-hand list of
// every NewGRF the savegame references (broken ones first, ticked once
// matched to a replacement), a GRF/vehicle detail pair in the centre, and
// a searchable/downloadable replacement browser on the right.
package ui

import (
	"fmt"

	"github.com/kolonuk/ottd-grf-doctor/internal/bananas"
	"github.com/kolonuk/ottd-grf-doctor/internal/engine"
	"github.com/kolonuk/ottd-grf-doctor/internal/grf"
	"github.com/kolonuk/ottd-grf-doctor/internal/sav"
)

// ItemKind distinguishes what kind of content a left-list Item
// represents.
type ItemKind int

const (
	KindVehicleGRF ItemKind = iota // a train, road vehicle, ship, or aircraft GRF -- see VehicleKind
	KindObjectGRF                  // scenery/station-object support -- see ObjectSlots/ObjectInstances/ObjectMatch
)

// Item is one row in the left-hand list: either a missing GRF (broken,
// listed first) or a currently-loaded one (for context).
type Item struct {
	Kind   ItemKind
	GRFID  string
	Broken bool

	// VehicleKind says which of the four vehicle types this item is,
	// when Kind == KindVehicleGRF (VehTrain, VehRoad, VehShip, or
	// VehAircraft). A single GRF only ever defines one category in
	// practice, so exactly one of Vehicles/OtherVehicles below is
	// populated to match.
	VehicleKind engine.VehicleType

	// Populated for broken train items from engine.Analyze. Train
	// removal (deleting a wagon) and multiheaded-pairing fixup are
	// train-specific OpenTTD mechanics with no equivalent for the other
	// three vehicle types -- see RemovedVehIDs' doc comment.
	Slots    []int
	Vehicles []engine.TrainVehicle

	// Populated for broken road vehicle/ship/aircraft items from
	// engine.Analyze. These support matching (replacing the broken
	// engine reference) but not removal -- unlike trains, there's no
	// consist-relinking concern for a single road vehicle/ship/aircraft,
	// and articulated-road-vehicle trailer removal isn't implemented
	// (out of scope for now; see project notes).
	OtherSlots    []int
	OtherVehicles []engine.OtherVehicle

	// Populated for broken object items from engine.Analyze. Object pool
	// slots (ObjectType) are stable identifiers OBJS instances reference
	// directly, so fixing them is a direct OBID repoint -- see
	// engine.ApplyObjectSwaps -- rather than the vehicle-style
	// slot-reallocation Match/RemovedVehIDs below drive.
	ObjectSlots     []int
	ObjectInstances []engine.ObjectInstance
	ObjectMatch     *engine.ObjectAssignment

	// Populated for loaded items from engine.NGRFEntry.
	Loaded *engine.NGRFEntry

	// Match state, set by the matching workflow.
	Match         *engine.TargetEngine
	RemovedVehIDs map[int]bool // VehicleIDs from this item explicitly marked for removal instead of Match -- trains only, see Vehicles' doc comment
}

// VehicleCount is however many real vehicles/instances this item
// affects, regardless of which of Vehicles/OtherVehicles/ObjectInstances
// is the populated one.
func (it *Item) VehicleCount() int {
	return len(it.Vehicles) + len(it.OtherVehicles) + len(it.ObjectInstances)
}

func (it *Item) Matched() bool {
	if it.Kind == KindObjectGRF {
		return it.ObjectMatch != nil
	}
	if len(it.OtherVehicles) > 0 {
		return it.Match != nil
	}
	return it.Match != nil || (it.Broken && len(it.RemovedVehIDs) == len(it.Vehicles) && len(it.Vehicles) > 0)
}

// Model holds every piece of state the UI screens read from and write to.
// It owns the loaded savegame and every derived structure; screens never
// re-parse the payload themselves.
type Model struct {
	Path    string
	Save    *sav.Save
	Payload []byte

	EIDS          []engine.EIDSEntry
	NGRF          []engine.NGRFEntry
	Vehicles      []engine.TrainVehicle
	OtherVehicles []engine.OtherVehicle
	OBID          []engine.ObjectTypeEntry
	Objects       []engine.ObjectInstance
	Analysis      *engine.Analysis

	Year       int
	RailLabels []string
	Tiles      *engine.MapTiles

	Items []*Item

	// Replacement GRFs downloaded/selected during this session, pending
	// insertion into the save when the plan is applied.
	PendingGRFs []engine.NewGRFToInsert

	// Downloaded candidates' dynamically-parsed engine rosters, keyed by
	// GRFID -- this is what makes matching real instead of requiring the
	// user to type an internal ID blind: see internal/grf, which reads
	// each engine's actual track type/dates/speed/power straight from
	// the GRF binary's Action0 properties (no hardcoded per-GRF table).
	ParsedCandidates map[string]*grf.ParsedGRF

	Bananas *bananas.Client
}

// LoadModel reads and analyzes a savegame, building every Item this
// tool's TUI needs. It does not modify anything on disk.
func LoadModel(path string) (*Model, error) {
	s, err := sav.Load(path)
	if err != nil {
		return nil, fmt.Errorf("loading %s: %w", path, err)
	}
	chunks, err := sav.WalkChunks(s.Payload)
	if err != nil {
		return nil, fmt.Errorf("parsing chunks: %w", err)
	}
	cm := sav.ChunkMapOf(chunks)

	eids, err := engine.ParseEIDS(s.Payload, cm["EIDS"])
	if err != nil {
		return nil, fmt.Errorf("parsing EIDS: %w", err)
	}
	ngrf, err := engine.ParseNGRF(s.Payload, cm["NGRF"])
	if err != nil {
		return nil, fmt.Errorf("parsing NGRF: %w", err)
	}
	vehicles, err := engine.ParseVEHS(s.Payload, cm["VEHS"])
	if err != nil {
		return nil, fmt.Errorf("parsing VEHS: %w", err)
	}
	otherVehicles, err := engine.ParseOtherVehicles(s.Payload, cm["VEHS"])
	if err != nil {
		return nil, fmt.Errorf("parsing VEHS (road/ship/aircraft): %w", err)
	}
	// OBID/OBJS (object/scenery GRF mapping) are optional -- older or
	// unusual saves might lack them entirely; treat that as "no objects"
	// rather than failing the whole load.
	var obid []engine.ObjectTypeEntry
	var objs []engine.ObjectInstance
	if c, ok := cm["OBID"]; ok {
		if obid, err = engine.ParseOBID(s.Payload, c); err != nil {
			return nil, fmt.Errorf("parsing OBID: %w", err)
		}
	}
	if c, ok := cm["OBJS"]; ok {
		if objs, err = engine.ParseOBJS(s.Payload, c); err != nil {
			return nil, fmt.Errorf("parsing OBJS: %w", err)
		}
	}
	an := engine.Analyze(eids, ngrf, vehicles, otherVehicles, obid, objs)

	year, err := engine.ParseInGameYear(s.Payload, cm["DATE"])
	if err != nil {
		year = 0 // non-fatal -- date warnings just won't fire
	}
	railLabels, err := engine.ParseRailtypeLabels(s.Payload, cm["RAIL"])
	if err != nil {
		railLabels = nil
	}
	tiles, err := engine.LoadMapTiles(s.Payload, cm)
	if err != nil {
		tiles = nil
	}

	m := &Model{
		Path: path, Save: s, Payload: s.Payload,
		EIDS: eids, NGRF: ngrf, Vehicles: vehicles, OtherVehicles: otherVehicles,
		OBID: obid, Objects: objs, Analysis: an,
		Year: year, RailLabels: railLabels, Tiles: tiles,
		Bananas:          &bananas.Client{},
		ParsedCandidates: map[string]*grf.ParsedGRF{},
	}

	for _, missing := range an.Missing {
		if len(missing.OtherVehicles) > 0 {
			m.Items = append(m.Items, &Item{
				Kind: KindVehicleGRF, GRFID: missing.GRFID, Broken: true,
				VehicleKind:   missing.OtherVehicles[0].Kind,
				OtherSlots:    missing.Slots,
				OtherVehicles: missing.OtherVehicles,
			})
			continue
		}
		m.Items = append(m.Items, &Item{
			Kind: KindVehicleGRF, GRFID: missing.GRFID, Broken: true,
			VehicleKind: engine.VehTrain,
			Slots:       missing.Slots, Vehicles: missing.Vehicles,
			RemovedVehIDs: map[int]bool{},
		})
	}
	for _, missingObj := range an.MissingObjects {
		m.Items = append(m.Items, &Item{
			Kind: KindObjectGRF, GRFID: missingObj.GRFID, Broken: true,
			ObjectSlots: missingObj.Slots, ObjectInstances: missingObj.Instances,
		})
	}
	for i := range ngrf {
		m.Items = append(m.Items, &Item{
			Kind: KindVehicleGRF, GRFID: ngrf[i].GRFID, Broken: false,
			Loaded: &ngrf[i],
		})
	}

	return m, nil
}

// RailtypeOfParsedEngine maps a dynamically-parsed engine's raw track
// type property to this tool's Railtype enum, matching OpenTTD's own
// interpretation (src/newgrf/newgrf_act0_trains.cpp's RailVehicleChangeInfo,
// case 0x05): 0=rail (or electrified, depending on engine class -- this
// package doesn't track property 0x19 "engine traction type" yet, so it
// simplifies to plain Rail; see grf.ParsedEngine's doc comment), 1=mono,
// 2=maglev, anything else is an index into the GRF's own railtype
// translation table, which this package doesn't resolve -- reported as
// Unknown rather than guessed.
func RailtypeOfParsedEngine(e *grf.ParsedEngine) engine.Railtype {
	if !e.HasTrackType {
		return engine.RailtypeUnknown
	}
	switch e.TrackType {
	case 0:
		return engine.RailtypeRail
	case 1:
		return engine.RailtypeMono
	case 2:
		return engine.RailtypeMaglev
	default:
		return engine.RailtypeUnknown // translated table index -- not resolved (see doc comment)
	}
}

// RailtypeAtTile resolves the Railtype actually built under a vehicle's
// current position, or RailtypeUnknown if that can't be determined (no
// MAPS/MAPT/M3LO/RAIL data, e.g. a truncated or unsupported save).
func (m *Model) RailtypeAtTile(tile uint32) engine.Railtype {
	if m.Tiles == nil || m.RailLabels == nil {
		return engine.RailtypeUnknown
	}
	idx := m.Tiles.RailtypeIndex(tile)
	if int(idx) >= len(m.RailLabels) {
		return engine.RailtypeUnknown
	}
	return engine.RailtypeFromLabel(m.RailLabels[idx])
}
