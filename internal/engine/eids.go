package engine

import (
	"fmt"

	"github.com/kolonuk/ottd-grf-doctor/internal/sav"
)

// VehicleType mirrors OpenTTD's VehicleType enum values as stored in EIDS.
type VehicleType uint8

const (
	VehTrain    VehicleType = 0
	VehRoad     VehicleType = 1
	VehShip     VehicleType = 2
	VehAircraft VehicleType = 3
)

// InvalidGRFID is the on-disk sentinel OpenTTD uses in EIDS/NGRF records to
// mean "not from any NewGRF" (a default/base-game engine).
const InvalidGRFID = "FFFFFFFF"

// EIDSEntry is one fixed-size (8-byte) record from the "EIDS" chunk: the
// mapping from a pool slot (EngineID, i.e. its position in the chunk) to
// the (GRFID, internal_id) pair that identifies which engine it represents.
type EIDSEntry struct {
	EngineID     int
	GRFID        string // 8 hex chars, memory order
	InternalID   uint16
	Type         VehicleType
	SubstituteID uint8
}

// ParseEIDS decodes every fixed 8-byte record in the save's EIDS chunk.
func ParseEIDS(payload []byte, chunk *sav.Chunk) ([]EIDSEntry, error) {
	entries := make([]EIDSEntry, 0, len(chunk.Records))
	for i, rec := range chunk.Records {
		if rec.Length != 8 {
			return nil, fmt.Errorf("EIDS record %d: expected 8 bytes, got %d", i, rec.Length)
		}
		p := rec.Offset
		grfidBytes := payload[p : p+4]
		internalID := uint16(payload[p+4])<<8 | uint16(payload[p+5])
		vtype := VehicleType(payload[p+6])
		sub := payload[p+7]
		entries = append(entries, EIDSEntry{
			EngineID:     i,
			GRFID:        diskToGRFID(grfidBytes),
			InternalID:   internalID,
			Type:         vtype,
			SubstituteID: sub,
		})
	}
	return entries, nil
}

// EncodeEIDSEntry produces the 8 on-disk bytes for one EIDS record.
// substitute_id is always set equal to internal_id&0xFF, matching the
// pattern OpenTTD itself uses for every genuine "engine whose GRF is
// missing, fall back to a default" entry (verified against real
// savegames): substitute_id == internal_id for every FFFFFFFF-grfid
// record actually written by the game.
func EncodeEIDSEntry(grfidHex string, internalID uint16, vtype VehicleType) ([8]byte, error) {
	var out [8]byte
	disk, err := grfidToDisk(grfidHex)
	if err != nil {
		return out, fmt.Errorf("bad grfid %q: %w", grfidHex, err)
	}
	copy(out[0:4], disk[:])
	out[4] = byte(internalID >> 8)
	out[5] = byte(internalID)
	out[6] = byte(vtype)
	out[7] = byte(internalID & 0xFF)
	return out, nil
}

// ValidateUniqueKeys is THE critical safety check this tool exists to
// enforce. OpenTTD's engine manager (EngineOverrideManager::SetID, see
// src/engine.cpp) keys its (grfid, internal_id) -> EngineID mapping table
// by (grfid, internal_id) alone. If two different EngineID pool slots are
// given the *same* (grfid, internal_id) pair, the second SetID() call
// silently overwrites the first instead of erroring -- so SetupEngines()
// (which does exactly one placement-new per surviving map entry) only
// constructs an Engine object for ONE of the colliding slots. Every other
// colliding slot becomes a hole in the engine pool. The game does not
// notice until CommitVehicleListOrderChanges() sorts the full engine list
// on the very next load, at which point Engine::Get() on a hole returns
// nullptr and EnginePreSort() dereferences it -- an
// EXCEPTION_ACCESS_VIOLATION crash, on load, before the game even reaches
// the menu. (This is exactly the bug found and fixed by hand earlier in
// this project's development -- see the session history / commit log.)
//
// Call this after any modification to the EIDS chunk, before writing the
// save out, for every vehicle type independently (the key space is
// per-type: mappings[VEH_TRAIN] is separate from mappings[VEH_ROAD] etc.).
func ValidateUniqueKeys(entries []EIDSEntry) error {
	type key struct {
		grfid string
		id    uint16
		vtype VehicleType
	}
	seen := make(map[key]int, len(entries))
	for _, e := range entries {
		k := key{e.GRFID, e.InternalID, e.Type}
		if prevSlot, ok := seen[k]; ok {
			return fmt.Errorf(
				"DUPLICATE engine key detected: slots %d and %d both claim (grfid=%s, internal_id=%d, type=%d) -- "+
					"this WILL crash the game on load (see ValidateUniqueKeys doc comment for why)",
				prevSlot, e.EngineID, e.GRFID, e.InternalID, e.Type)
		}
		seen[k] = e.EngineID
	}
	return nil
}
