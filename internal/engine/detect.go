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

// MissingObjectGRF is the object/scenery equivalent of MissingGRF: one
// NewGRF that used to provide object specs (transmitters, statues,
// company-specific scenery, etc.) but is no longer loaded (its grfid
// appears in OBID but not in the current NGRF list).
//
// Unlike vehicles, ObjectType pool slots are stable identifiers referenced
// directly by every OBJS instance's `type` field, so fixing a broken object
// GRF never needs to move OBJS records or juggle a shared slot pool -- it's
// enough to repoint each broken OBID slot at a replacement (grfid,
// entity_id) pair (see ApplyObjectSwaps).
type MissingObjectGRF struct {
	GRFID     string
	Slots     []int // ObjectType pool slots (indices into OBID) sourced from this grfid
	Instances []ObjectInstance
}

// Analysis is the full picture of one savegame's broken-GRF situation,
// ready to drive a TUI list screen.
type Analysis struct {
	LoadedGRFIDs   map[string]bool
	Missing        []MissingGRF       // sorted by GRFID for stable display
	MissingObjects []MissingObjectGRF // sorted by GRFID for stable display
}

// Analyze cross-references the EIDS and OBID chunks' grfids against the
// currently loaded NGRF list to find every GRF that's referenced but not
// present, then attributes every train vehicle / object instance whose
// slot lands on one of those grfids. obid/objs may be nil if the save
// doesn't have those chunks (or the caller doesn't care about objects);
// object detection is simply skipped in that case.
func Analyze(eids []EIDSEntry, ngrf []NGRFEntry, vehicles []TrainVehicle, obid []ObjectTypeEntry, objs []ObjectInstance) *Analysis {
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

	objSlotsByGRFID := make(map[string][]int)
	objSlotGRFID := make(map[int]string, len(obid))
	for _, e := range obid {
		if e.GRFID == "00000000" && e.EntityID == 0 {
			continue // reserved/unused slot, not a real NewGRF-sourced object
		}
		if loaded[e.GRFID] {
			continue
		}
		objSlotsByGRFID[e.GRFID] = append(objSlotsByGRFID[e.GRFID], e.ObjectType)
		objSlotGRFID[e.ObjectType] = e.GRFID
	}

	instancesByGRFID := make(map[string][]ObjectInstance)
	for _, o := range objs {
		if grfid, ok := objSlotGRFID[int(o.ObjectType)]; ok {
			instancesByGRFID[grfid] = append(instancesByGRFID[grfid], o)
		}
	}

	for grfid, slots := range objSlotsByGRFID {
		sort.Ints(slots)
		a.MissingObjects = append(a.MissingObjects, MissingObjectGRF{
			GRFID:     grfid,
			Slots:     slots,
			Instances: instancesByGRFID[grfid],
		})
	}
	sort.Slice(a.MissingObjects, func(i, j int) bool { return a.MissingObjects[i].GRFID < a.MissingObjects[j].GRFID })

	return a
}
