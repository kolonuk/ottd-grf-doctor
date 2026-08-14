package engine

import "sort"

// MissingGRF groups together the EIDS pool slots and affected vehicles for
// one NewGRF that used to be loaded but no longer is (its grfid appears in
// EIDS but not in the current NGRF list).
type MissingGRF struct {
	GRFID    string
	Slots    []int // EngineID pool slots (indices into EIDS) sourced from this grfid
	Vehicles []TrainVehicle
}

// Analysis is the full picture of one savegame's broken-GRF situation,
// ready to drive a TUI list screen.
type Analysis struct {
	LoadedGRFIDs map[string]bool
	Missing      []MissingGRF // sorted by GRFID for stable display
}

// Analyze cross-references the EIDS chunk's grfids against the currently
// loaded NGRF list to find every GRF that's referenced but not present,
// then attributes every train vehicle whose engine_type lands on one of
// those slots.
func Analyze(eids []EIDSEntry, ngrf []NGRFEntry, vehicles []TrainVehicle) *Analysis {
	loaded := make(map[string]bool, len(ngrf))
	for _, e := range ngrf {
		loaded[e.GRFID] = true
	}

	slotsByGRFID := make(map[string][]int)
	slotGRFID := make(map[int]string, len(eids))
	for _, e := range eids {
		if e.GRFID == InvalidGRFID || e.GRFID == "00000000" {
			continue // default/base-game engine, not a missing NewGRF
		}
		if loaded[e.GRFID] {
			continue
		}
		slotsByGRFID[e.GRFID] = append(slotsByGRFID[e.GRFID], e.EngineID)
		slotGRFID[e.EngineID] = e.GRFID
	}

	vehiclesByGRFID := make(map[string][]TrainVehicle)
	for _, v := range vehicles {
		if grfid, ok := slotGRFID[int(v.EngineType)]; ok {
			vehiclesByGRFID[grfid] = append(vehiclesByGRFID[grfid], v)
		}
	}

	a := &Analysis{LoadedGRFIDs: loaded}
	for grfid, slots := range slotsByGRFID {
		sort.Ints(slots)
		a.Missing = append(a.Missing, MissingGRF{
			GRFID:    grfid,
			Slots:    slots,
			Vehicles: vehiclesByGRFID[grfid],
		})
	}
	sort.Slice(a.Missing, func(i, j int) bool { return a.Missing[i].GRFID < a.Missing[j].GRFID })
	return a
}
