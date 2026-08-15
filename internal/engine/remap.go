package engine

import "fmt"

// TargetEngine identifies the engine a group of broken vehicles should be
// reassigned to: either a base-game default engine (GRFID == InvalidGRFID)
// or an engine from a newly- or already-loaded NewGRF.
type TargetEngine struct {
	GRFID      string
	InternalID uint16
	Name       string // display only
}

// Assignment is one user decision: some subset of a MissingGRF's vehicles
// should end up on TargetEngine, or be removed from their consist entirely.
type Assignment struct {
	VehicleIDs []int // VehicleID values (TrainVehicle.VehicleID), not EngineID slots
	Target     TargetEngine
	Remove     bool // true: delete these vehicles instead of reassigning

	// PreferredSlot pins Target to a specific EngineID pool slot instead
	// of letting the allocator pick the next free one. The slot must be
	// one of the broken slots being freed up by this Analysis. Use -1
	// (the zero value via NoPreferredSlot) to let the allocator choose.
	// Mainly useful for reproducing a specific known-good result
	// deterministically (see doctor_test.go) or for a TUI "advanced"
	// override; ordinary use doesn't need this.
	PreferredSlot int
}

// NoPreferredSlot is the sentinel meaning "let the allocator pick". This is
// Assignment's PreferredSlot zero-value equivalent -- always construct
// Assignment via NewAssignment/NewPinnedAssignment/NewRemoval rather than
// a bare struct literal, since Go would otherwise silently default
// PreferredSlot to 0, which is a real (if unlikely) slot number, not "no
// preference".
const NoPreferredSlot = -1

// NewAssignment builds an ordinary (auto-allocated slot) Assignment.
func NewAssignment(vehicleIDs []int, target TargetEngine) Assignment {
	return Assignment{VehicleIDs: vehicleIDs, Target: target, PreferredSlot: NoPreferredSlot}
}

// NewPinnedAssignment builds an Assignment that must land on a specific
// EngineID slot (which must be one of the broken slots being freed up).
func NewPinnedAssignment(vehicleIDs []int, target TargetEngine, slot int) Assignment {
	return Assignment{VehicleIDs: vehicleIDs, Target: target, PreferredSlot: slot}
}

// NewRemoval builds an Assignment that deletes the given vehicles instead
// of reassigning them.
func NewRemoval(vehicleIDs []int) Assignment {
	return Assignment{VehicleIDs: vehicleIDs, Remove: true, PreferredSlot: NoPreferredSlot}
}

// Plan is a full set of assignments the user has built in the matching UI,
// ready to be applied to a save.
type Plan struct {
	Assignments []Assignment
}

// slotAllocator hands out unique EngineID pool slots for each distinct
// TargetEngine a Plan needs, reusing the broken slots being freed up so no
// new pool space is required, while guaranteeing the EIDS uniqueness
// invariant ValidateUniqueKeys checks for.
type slotAllocator struct {
	freeSlots   []int // broken slots available to repurpose, consumed front-to-back
	targetSlot  map[TargetEngine]int
	usedFillers map[uint16]bool // filler internal_ids already handed out, for TRAIN type
	nextFiller  uint16
}

func newSlotAllocator(freeSlots []int) *slotAllocator {
	cp := append([]int(nil), freeSlots...)
	return &slotAllocator{
		freeSlots:   cp,
		targetSlot:  make(map[TargetEngine]int),
		usedFillers: make(map[uint16]bool),
	}
}

func (a *slotAllocator) slotFor(t TargetEngine, preferred int) (int, error) {
	if slot, ok := a.targetSlot[t]; ok {
		return slot, nil
	}
	if preferred != NoPreferredSlot {
		idx := -1
		for i, s := range a.freeSlots {
			if s == preferred {
				idx = i
				break
			}
		}
		if idx == -1 {
			return 0, fmt.Errorf("preferred slot %d for target %+v is not an available free slot", preferred, t)
		}
		a.freeSlots = append(a.freeSlots[:idx], a.freeSlots[idx+1:]...)
		a.targetSlot[t] = preferred
		return preferred, nil
	}
	if len(a.freeSlots) == 0 {
		return 0, fmt.Errorf("ran out of free EngineID slots to repurpose for target %+v", t)
	}
	slot := a.freeSlots[0]
	a.freeSlots = a.freeSlots[1:]
	a.targetSlot[t] = slot
	return slot, nil
}

// fillerInternalID returns a train internal_id guaranteed not to collide
// with any target this allocator has already handed out, for use on
// leftover broken slots nothing in the Plan touches (they still need a
// valid, unique EIDS entry to keep the chunk dense).
func (a *slotAllocator) fillerInternalID(usedByTargets map[uint16]bool) uint16 {
	for {
		id := a.nextFiller
		a.nextFiller++
		if !usedByTargets[id] && !a.usedFillers[id] {
			a.usedFillers[id] = true
			return id
		}
	}
}

// ApplyResult summarizes what Apply did, for display/logging.
type ApplyResult struct {
	SlotsRepointed  map[int]TargetEngine // EngineID -> new target
	SlotTypes       map[int]VehicleType  // EngineID -> the VehicleType of the vehicles now assigned to it (SlotsRepointed keys only -- see doc comment on why this can't just be read back from the original EIDS entry)
	FillerSlots     map[int]uint16       // EngineID -> filler internal_id assigned
	VehiclesMoved   map[int]int          // VehicleID -> new EngineID slot
	VehiclesRemoved []int                // VehicleID values deleted
}

// Apply computes the concrete slot/vehicle changes for plan against the
// given Analysis, WITHOUT touching any bytes -- BuildEIDSPatch and the
// VEHS/removal helpers in doctor.go consume this result to actually edit
// the payload. Keeping this pure makes it easy to preview a plan in the
// TUI before committing it.
//
// A slot freed up by one broken GRF can end up hosting a replacement
// engine for a *different* vehicle type than whatever used to occupy it
// (e.g. a slot freed by a broken train GRF reused for a new aircraft
// engine) -- this is not a bug to avoid, it's how OpenTTD's own engine
// pool actually works: SetupEngines() (src/engine.cpp) constructs each
// pool slot's Engine object using whichever VehicleType bucket its EIDS
// entry belongs to at load time, not anything fixed to the slot number
// itself (verified directly against that function during this tool's
// development). SlotTypes on the result therefore records each
// repointed slot's *new* type (the type of the vehicles actually being
// reassigned to it) rather than the type of whatever it used to be --
// getting this backwards was a real bug caught by this package's own
// tests, since writing the *old* type into EIDS for a slot now hosting a
// different vehicle type would misclassify it exactly the way
// ValidateUniqueKeys' doc comment warns about.
func Apply(an *Analysis, plan *Plan) (*ApplyResult, error) {
	var allBroken []int
	for _, m := range an.Missing {
		allBroken = append(allBroken, m.Slots...)
	}
	alloc := newSlotAllocator(allBroken)

	res := &ApplyResult{
		SlotsRepointed: make(map[int]TargetEngine),
		SlotTypes:      make(map[int]VehicleType),
		FillerSlots:    make(map[int]uint16),
		VehiclesMoved:  make(map[int]int),
	}

	vehicleType := make(map[int]VehicleType)
	for _, m := range an.Missing {
		for _, v := range m.Vehicles {
			vehicleType[v.VehicleID] = VehTrain
		}
		for _, v := range m.OtherVehicles {
			vehicleType[v.VehicleID] = v.Kind
		}
	}

	usedTargetIDs := make(map[uint16]bool)
	for _, a := range plan.Assignments {
		if !a.Remove {
			usedTargetIDs[a.Target.InternalID] = true
		}
	}

	for _, a := range plan.Assignments {
		if a.Remove {
			res.VehiclesRemoved = append(res.VehiclesRemoved, a.VehicleIDs...)
			continue
		}
		if len(a.VehicleIDs) == 0 {
			return nil, fmt.Errorf("assignment for target %+v has no vehicles", a.Target)
		}
		slot, err := alloc.slotFor(a.Target, a.PreferredSlot)
		if err != nil {
			return nil, err
		}
		res.SlotsRepointed[slot] = a.Target
		for _, vid := range a.VehicleIDs {
			vt, ok := vehicleType[vid]
			if !ok {
				return nil, fmt.Errorf("assignment references unknown vehicle id %d", vid)
			}
			if existing, set := res.SlotTypes[slot]; set && existing != vt {
				return nil, fmt.Errorf("target %+v is claimed by vehicles of two different types (%d and %d) -- a single target engine can't serve both", a.Target, existing, vt)
			}
			res.SlotTypes[slot] = vt
			res.VehiclesMoved[vid] = slot
		}
	}

	// Every broken slot not chosen as a canonical target above still
	// needs a valid, unique EIDS entry -- give it a harmless filler.
	claimedSlots := make(map[int]bool, len(res.SlotsRepointed))
	for slot := range res.SlotsRepointed {
		claimedSlots[slot] = true
	}
	for _, m := range an.Missing {
		for _, slot := range m.Slots {
			if claimedSlots[slot] {
				continue
			}
			res.FillerSlots[slot] = alloc.fillerInternalID(usedTargetIDs)
		}
	}

	return res, nil
}
