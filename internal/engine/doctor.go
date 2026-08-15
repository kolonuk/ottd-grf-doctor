package engine

import (
	"fmt"

	"github.com/kolonuk/ottd-grf-doctor/internal/sav"
)

// NewGRFToInsert describes a GRF that must be appended to the NGRF chunk
// because a Plan assigns vehicles to one of its engines and it isn't
// already loaded.
type NewGRFToInsert struct {
	LocalPath string
	Filename  string // as it should be recorded in the save (e.g. "SHARK.grf")
	GRFID     string
	Version   uint32
	Palette   byte
	Params    []uint32
}

// ApplyToPayload performs every byte-level edit ApplyResult implies against
// a copy of the save's payload, plus any needed NGRF insertions and
// removals, and returns the new payload. It never mutates the input.
//
// otherVehicles carries the road vehicle/ship/aircraft records a plan's
// assignments might reference (see Item.OtherVehicles) -- pass nil if
// the plan only ever touches trains. Removal and multiheaded-pairing
// fixup are train-only mechanics with no equivalent for the other three
// types (see Item.OtherVehicles' doc comment), so VehiclesRemoved only
// ever contains train VehicleIDs.
//
// removeGRFIDs drops NGRF entries (e.g. a NewGRF that no vehicle
// references anymore after this fix, such as one being swapped out) --
// pass nil if there's nothing to remove.
func ApplyToPayload(payload []byte, eids []EIDSEntry, ngrf []NGRFEntry, vehicles []TrainVehicle, otherVehicles []OtherVehicle,
	res *ApplyResult, newGRFs []NewGRFToInsert, removeGRFIDs []string) ([]byte, error) {

	out := append([]byte(nil), payload...)

	chunks, err := sav.WalkChunks(out)
	if err != nil {
		return nil, err
	}
	cm := sav.ChunkMapOf(chunks)

	// --- 1. Patch EIDS: repointed canonical slots + fillers ---
	// A repointed slot is written with the VehicleType of whatever it's
	// now hosting (res.SlotTypes -- see Apply's doc comment on why this
	// can differ from the slot's previous occupant, e.g. a slot freed by
	// a broken train GRF reused for a new aircraft engine). A filler
	// slot keeps the ORIGINAL EIDS entry's type: it isn't being
	// reassigned to host a different vehicle type, just marked unused.
	// EIDS's uniqueness key is (grfid, internal_id, type), so getting
	// either of these wrong risks exactly the kind of silent-collision-
	// then-pool-hole bug ValidateUniqueKeys exists to catch.
	origSlotType := make(map[int]VehicleType, len(eids))
	for _, e := range eids {
		origSlotType[e.EngineID] = e.Type
	}
	eidsChunk, ok := cm["EIDS"]
	if !ok {
		return nil, fmt.Errorf("EIDS chunk not found")
	}
	for slot, target := range res.SlotsRepointed {
		if slot >= len(eidsChunk.Records) {
			return nil, fmt.Errorf("target slot %d out of range (EIDS has %d records)", slot, len(eidsChunk.Records))
		}
		enc, err := EncodeEIDSEntry(target.GRFID, target.InternalID, res.SlotTypes[slot])
		if err != nil {
			return nil, err
		}
		rec := eidsChunk.Records[slot]
		copy(out[rec.Offset:rec.Offset+rec.Length], enc[:])
	}
	for slot, fillerID := range res.FillerSlots {
		if slot >= len(eidsChunk.Records) {
			return nil, fmt.Errorf("filler slot %d out of range", slot)
		}
		enc, err := EncodeEIDSEntry(InvalidGRFID, fillerID, origSlotType[slot])
		if err != nil {
			return nil, err
		}
		rec := eidsChunk.Records[slot]
		copy(out[rec.Offset:rec.Offset+rec.Length], enc[:])
	}

	// --- 2. Verify uniqueness before touching VEHS -- fail loudly rather
	//        than write a save that will crash on load. ---
	reparsedEIDS, err := ParseEIDS(out, mustChunk(out, "EIDS"))
	if err != nil {
		return nil, err
	}
	if err := ValidateUniqueKeys(reparsedEIDS); err != nil {
		return nil, fmt.Errorf("refusing to write save: %w", err)
	}

	// --- 3. Patch VEHS engine_type for every moved vehicle, fixing up
	//        multiheaded pairing along the way (trains only). ---
	byID := make(map[int]*TrainVehicle, len(vehicles))
	vehiclesCopy := append([]TrainVehicle(nil), vehicles...)
	for i := range vehiclesCopy {
		byID[vehiclesCopy[i].VehicleID] = &vehiclesCopy[i]
	}
	otherByID := make(map[int]*OtherVehicle, len(otherVehicles))
	otherVehiclesCopy := append([]OtherVehicle(nil), otherVehicles...)
	for i := range otherVehiclesCopy {
		otherByID[otherVehiclesCopy[i].VehicleID] = &otherVehiclesCopy[i]
	}

	for vid, newSlot := range res.VehiclesMoved {
		if tv, ok := byID[vid]; ok {
			SetEngineType(out, tv, uint16(newSlot))
			continue
		}
		if ov, ok := otherByID[vid]; ok {
			SetOtherEngineType(out, ov, uint16(newSlot))
			continue
		}
		return nil, fmt.Errorf("moved vehicle %d not found in VEHS", vid)
	}

	for _, fix := range findBrokenMultiheadPairs(vehiclesCopy, res.VehiclesMoved) {
		tv := byID[fix.VehicleID]
		SetSubtype(out, tv, fix.NewSubtype)
	}

	// --- 4. Remove vehicles marked for deletion (trains only). ---
	if len(res.VehiclesRemoved) > 0 {
		out, err = removeVehicles(out, byID, res.VehiclesRemoved)
		if err != nil {
			return nil, err
		}
	}

	// --- 5. Drop any now-unused NGRF entries. ---
	if len(removeGRFIDs) > 0 {
		set := make(map[string]bool, len(removeGRFIDs))
		for _, id := range removeGRFIDs {
			set[id] = true
		}
		out, err = removeNGRFEntries(out, set)
		if err != nil {
			return nil, err
		}
	}

	// --- 6. Insert any new NGRF entries the plan needs. ---
	for _, g := range newGRFs {
		md5Sum, err := MD5File(g.LocalPath)
		if err != nil {
			return nil, fmt.Errorf("hashing %s: %w", g.LocalPath, err)
		}
		out, err = insertNGRF(out, g.Filename, g.GRFID, md5Sum, g.Version, g.Palette, g.Params)
		if err != nil {
			return nil, err
		}
	}

	return out, nil
}

// ObjectAssignment says: repoint this missing object GRF's ObjectType
// slots to a specific replacement (grfid, entity_id) pair -- e.g. an
// equivalent object spec from a newly-inserted or already-loaded GRF.
//
// Unlike vehicles, ObjectType pool slots are stable identifiers every
// OBJS instance references directly by value, so fixing a broken object
// GRF never needs to reallocate slots, move OBJS records, or do anything
// like the multiheaded-pairing fixup vehicles need -- repointing the OBID
// entry in place is the whole fix.
type ObjectAssignment struct {
	Slots        []int // OBID slots (ObjectType values), e.g. a MissingObjectGRF's Slots
	TargetGRFID  string
	TargetEntity uint8
}

// ApplyObjectSwaps patches the given OBID slots in place to point at each
// assignment's replacement (grfid, entity_id), returning the new payload.
// It never mutates the input. OBJS instances need no changes (see
// ObjectAssignment's doc comment).
//
// substitute_id is always written as 0: real savegames observed during
// this tool's development have substitute_id == 0 for every populated
// OBID entry regardless of entity_id, unlike EIDS's substitute_id (which
// mirrors internal_id) -- see EncodeEIDSEntry's doc comment for contrast.
func ApplyObjectSwaps(payload []byte, assignments []ObjectAssignment) ([]byte, error) {
	out := append([]byte(nil), payload...)

	chunks, err := sav.WalkChunks(out)
	if err != nil {
		return nil, err
	}
	cm := sav.ChunkMapOf(chunks)
	obidChunk, ok := cm["OBID"]
	if !ok {
		return nil, fmt.Errorf("OBID chunk not found")
	}

	for _, a := range assignments {
		enc, err := EncodeOBIDEntry(a.TargetGRFID, a.TargetEntity, 0)
		if err != nil {
			return nil, err
		}
		for _, slot := range a.Slots {
			if slot >= len(obidChunk.Records) {
				return nil, fmt.Errorf("object slot %d out of range (OBID has %d records)", slot, len(obidChunk.Records))
			}
			rec := obidChunk.Records[slot]
			copy(out[rec.Offset:rec.Offset+rec.Length], enc[:])
		}
	}

	reparsed, err := ParseOBID(out, mustChunk(out, "OBID"))
	if err != nil {
		return nil, err
	}
	if err := ValidateUniqueObjectKeys(reparsed); err != nil {
		return nil, fmt.Errorf("refusing to write save: %w", err)
	}

	return out, nil
}

func mustChunk(payload []byte, id string) *sav.Chunk {
	chunks, err := sav.WalkChunks(payload)
	if err != nil {
		panic(err)
	}
	for _, c := range chunks {
		if c.IDString() == id {
			return c
		}
	}
	panic("chunk not found: " + id)
}

type subtypeFix struct {
	VehicleID  int
	NewSubtype uint8
}

// findBrokenMultiheadPairs identifies every REAR half of a multiheaded
// pair (multiheaded but not an engine -- the exact condition
// ConnectMultiheadedTrains uses to decide whether to force-mutate a
// vehicle when it can't find a partner) whose consist no longer contains
// an earlier multiheaded engine with a matching (post-move) engine_type,
// and returns the subtype fix needed to remove it from that pairing logic
// entirely (see SetSubtype's doc comment for why this is required, not
// optional -- this is the exact mechanism that caused a real crash during
// this tool's own development).
//
// Consists are identified by walking forward from every IsFront() vehicle,
// mirroring both OpenTTD's own ConnectMultiheadedTrains traversal and the
// consist-walking this project's original by-hand savegame analysis used
// (see the project history for the manual Python equivalent).
func findBrokenMultiheadPairs(vehicles []TrainVehicle, moved map[int]int) []subtypeFix {
	byID := make(map[int]*TrainVehicle, len(vehicles))
	for i := range vehicles {
		byID[vehicles[i].VehicleID] = &vehicles[i]
	}
	effectiveEngineType := func(v *TrainVehicle) uint16 {
		if slot, ok := moved[v.VehicleID]; ok {
			return uint16(slot)
		}
		return v.EngineType
	}

	var fixes []subtypeFix
	for i := range vehicles {
		front := &vehicles[i]
		if !front.IsFront() {
			continue
		}
		chain := walkConsist(front, byID)

		for _, v := range chain {
			if !v.IsMultiheaded() || v.IsEngine() {
				continue // only the REAR (multiheaded, non-engine) half is at risk
			}
			paired := false
			for _, other := range chain {
				if other.VehicleID == v.VehicleID || !other.IsMultiheaded() || !other.IsEngine() {
					continue
				}
				if effectiveEngineType(v) == effectiveEngineType(other) {
					paired = true
					break
				}
			}
			if !paired {
				fixes = append(fixes, subtypeFix{VehicleID: v.VehicleID, NewSubtype: GVSFWagon})
			}
		}
	}
	return fixes
}

// walkConsist follows NextVehicleID from front to the end of its train,
// returning every car in order (front included). Guards against malformed
// cyclic chains.
func walkConsist(front *TrainVehicle, byID map[int]*TrainVehicle) []*TrainVehicle {
	var chain []*TrainVehicle
	seen := map[int]bool{}
	cur := front
	for cur != nil && !seen[cur.VehicleID] {
		seen[cur.VehicleID] = true
		chain = append(chain, cur)
		if cur.NextVehicleID < 0 {
			break
		}
		cur = byID[int(cur.NextVehicleID)]
	}
	return chain
}
