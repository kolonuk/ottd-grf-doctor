package engine

import (
	"fmt"

	"github.com/kolonuk/ottd-grf-doctor/internal/sav"
)

// ObjectTypeEntry is one fixed 6-byte record in the OBID chunk: the mapping
// from an ObjectType pool slot (its index into this array -- the same value
// every OBJS record's `type` field stores) to the (GRFID, entity_id) pair
// identifying which object spec it represents. This is the scenery/object
// equivalent of EIDSEntry (see src/newgrf_object.cpp's _object_mngr and
// src/saveload/newgrf_sl.h's NewGRFMappingChunkHandler).
//
// Field sizes are version-gated: SLV_EXTEND_ENTITY_MAPPING (311) widened
// entity_id/substitute_id from 1 to 2 bytes. This package targets saves at
// or before that version, where each record is a fixed 6 bytes -- verified
// against real savegame data (a dense 64000-record array matching
// src/object_type.h's NUM_OBJECTS, with slots 0..NEW_OBJECT_OFFSET-1
// reserved/zero for built-in objects).
type ObjectTypeEntry struct {
	ObjectType   int
	GRFID        string // 8 hex chars, memory order
	EntityID     uint8
	SubstituteID uint8
}

// ParseOBID decodes every fixed 6-byte record in the save's OBID chunk.
func ParseOBID(payload []byte, chunk *sav.Chunk) ([]ObjectTypeEntry, error) {
	entries := make([]ObjectTypeEntry, 0, len(chunk.Records))
	for i, rec := range chunk.Records {
		if rec.Length != 6 {
			return nil, fmt.Errorf("OBID record %d: expected 6 bytes, got %d", i, rec.Length)
		}
		p := rec.Offset
		entries = append(entries, ObjectTypeEntry{
			ObjectType:   i,
			GRFID:        diskToGRFID(payload[p : p+4]),
			EntityID:     payload[p+4],
			SubstituteID: payload[p+5],
		})
	}
	return entries, nil
}

// EncodeOBIDEntry produces the 6 on-disk bytes for one OBID record.
func EncodeOBIDEntry(grfidHex string, entityID, substituteID uint8) ([6]byte, error) {
	var out [6]byte
	disk, err := grfidToDisk(grfidHex)
	if err != nil {
		return out, fmt.Errorf("bad grfid %q: %w", grfidHex, err)
	}
	copy(out[0:4], disk[:])
	out[4] = entityID
	out[5] = substituteID
	return out, nil
}

// ValidateUniqueObjectKeys is the object-chunk analogue of
// ValidateUniqueKeys: two OBID slots claiming the same (grfid, entity_id)
// pair collide in the same kind of NewGRF-mapping lookup EIDS uses, risking
// the same silent-overwrite-then-pool-hole failure mode (see
// ValidateUniqueKeys' doc comment for the mechanism). Call after any OBID
// edit, before writing the save out.
func ValidateUniqueObjectKeys(entries []ObjectTypeEntry) error {
	type key struct {
		grfid string
		id    uint8
	}
	seen := make(map[key]int, len(entries))
	for _, e := range entries {
		if e.GRFID == "00000000" && e.EntityID == 0 {
			continue // reserved/unused slot, not a real mapping
		}
		k := key{e.GRFID, e.EntityID}
		if prevSlot, ok := seen[k]; ok {
			return fmt.Errorf(
				"DUPLICATE object key detected: slots %d and %d both claim (grfid=%s, entity_id=%d) -- "+
					"this risks the same load-time crash ValidateUniqueKeys guards against for vehicles",
				prevSlot, e.ObjectType, e.GRFID, e.EntityID)
		}
		seen[k] = e.ObjectType
	}
	return nil
}
