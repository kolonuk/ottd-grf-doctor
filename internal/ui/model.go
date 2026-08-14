// Package ui implements the interactive terminal UI: a left-hand list of
// every NewGRF the savegame references (broken ones first, ticked once
// matched to a replacement), a GRF/vehicle detail pair in the centre, and
// a searchable/downloadable replacement browser on the right.
package ui

import (
	"fmt"

	"github.com/kolonuk/ottd-grf-doctor/internal/bananas"
	"github.com/kolonuk/ottd-grf-doctor/internal/engine"
	"github.com/kolonuk/ottd-grf-doctor/internal/sav"
)

// ItemKind distinguishes what kind of content a left-list Item
// represents. Only KindVehicleGRF is actually detectable/fixable today
// (see internal/engine's train-only scope); KindObjectGRF exists so the
// data model and UI don't need reshaping if/when object or station
// support is added -- it is never populated by LoadModel yet.
type ItemKind int

const (
	KindVehicleGRF ItemKind = iota
	KindObjectGRF           // reserved for future scenery/station-object support
)

// Item is one row in the left-hand list: either a missing GRF (broken,
// listed first) or a currently-loaded one (for context).
type Item struct {
	Kind   ItemKind
	GRFID  string
	Broken bool

	// Populated for broken items from engine.Analyze.
	Slots    []int
	Vehicles []engine.TrainVehicle

	// Populated for loaded items from engine.NGRFEntry.
	Loaded *engine.NGRFEntry

	// Match state, set by the matching workflow.
	Match         *engine.TargetEngine
	RemovedVehIDs map[int]bool // VehicleIDs from this item explicitly marked for removal instead of Match
}

func (it *Item) Matched() bool {
	return it.Match != nil || (it.Broken && len(it.RemovedVehIDs) == len(it.Vehicles) && len(it.Vehicles) > 0)
}

// Model holds every piece of state the UI screens read from and write to.
// It owns the loaded savegame and every derived structure; screens never
// re-parse the payload themselves.
type Model struct {
	Path    string
	Save    *sav.Save
	Payload []byte

	EIDS     []engine.EIDSEntry
	NGRF     []engine.NGRFEntry
	Vehicles []engine.TrainVehicle
	Analysis *engine.Analysis

	Year       int
	RailLabels []string
	Tiles      *engine.MapTiles

	Items []*Item

	// Replacement GRFs downloaded/selected during this session, pending
	// insertion into the save when the plan is applied.
	PendingGRFs []engine.NewGRFToInsert

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
	an := engine.Analyze(eids, ngrf, vehicles)

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
		EIDS: eids, NGRF: ngrf, Vehicles: vehicles, Analysis: an,
		Year: year, RailLabels: railLabels, Tiles: tiles,
		Bananas: &bananas.Client{},
	}

	for _, missing := range an.Missing {
		m.Items = append(m.Items, &Item{
			Kind: KindVehicleGRF, GRFID: missing.GRFID, Broken: true,
			Slots: missing.Slots, Vehicles: missing.Vehicles,
			RemovedVehIDs: map[int]bool{},
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
