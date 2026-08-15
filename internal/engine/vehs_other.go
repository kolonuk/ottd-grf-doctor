package engine

import (
	"fmt"

	"github.com/kolonuk/ottd-grf-doctor/internal/sav"
)

// OtherVehicle is one parsed VEHS record for a road vehicle, ship, or
// aircraft (VEHS type byte 1, 2, or 3) -- the fields this tool needs to
// detect and fix a broken engine reference. These three types share an
// identical common-Vehicle prefix (SlVehicleCommon in
// src/saveload/vehicle_sl.cpp) up through cargo_cap, which is as far as
// this parser reads (matching TrainVehicle's "stop once we have what we
// need" approach) -- each type's own tail fields (state, crashed_ctr,
// targetairport, etc.) aren't needed for engine-swapping and are never
// read.
//
// Field offsets/order were verified against savegame version 194
// (this tool's target) both by reading src/saveload/vehicle_sl.cpp's
// SLE_CONDVAR version gates directly and, for the highest-risk part (the
// variable-length cargo.packets reference list sitting between cargo_cap
// and each type's tail), by a full byte-for-byte walk of every real
// road/ship/aircraft record in this project's test fixture confirming
// it lands exactly on the next record's boundary -- see project history
// for that validation.
type OtherVehicle struct {
	VehicleID    int
	Kind         VehicleType // VehRoad, VehShip, or VehAircraft
	RecordOffset int
	RecordLength int

	Subtype       uint8 // meaningful for road vehicles (GVSF bits, shared with trains via GroundVehicle); unused by ships/aircraft
	NextVehicleID int32 // -1 if none; for aircraft this chains to shadow/rotor helper vehicles, which callers should skip when reassigning engines
	NextRefOffset int
	UnitNumber    uint16
	Owner         uint8
	Tile          uint32
	EngineType    uint16
	CargoType     uint8
	CargoCap      uint16

	SubtypeOffset    int
	EngineTypeOffset int
}

// ParseOtherVehicles decodes every road-vehicle, ship, and aircraft
// (type byte 1, 2, or 3) record in the save's VEHS chunk. Train records
// (type 0, see ParseVEHS) and effect/disaster records (type 4+, which
// use a completely different, non-SlVehicleCommon layout) are skipped.
func ParseOtherVehicles(payload []byte, chunk *sav.Chunk) ([]OtherVehicle, error) {
	var out []OtherVehicle
	for _, rec := range chunk.Records {
		p := rec.Offset
		sidx, p2, err := sav.ReadGamma(payload, p)
		if err != nil {
			return nil, fmt.Errorf("VEHS record at %d: reading sparse index: %w", rec.Offset, err)
		}
		p = p2
		if p >= rec.Offset+rec.Length {
			continue
		}
		vtype := payload[p]
		p++
		var kind VehicleType
		switch vtype {
		case 1:
			kind = VehRoad
		case 2:
			kind = VehShip
		case 3:
			kind = VehAircraft
		default:
			continue // train (0) or effect/disaster (4+) -- not this function's concern
		}

		ov := OtherVehicle{
			VehicleID:    int(sidx),
			Kind:         kind,
			RecordOffset: p,
			RecordLength: rec.Offset + rec.Length - p,
		}

		ov.SubtypeOffset = p
		ov.Subtype = payload[p]
		p++

		ov.NextRefOffset = p
		nextRef := u32(payload[p:])
		p += 4
		if nextRef == 0 {
			ov.NextVehicleID = -1
		} else {
			ov.NextVehicleID = int32(nextRef - 1) // see TrainVehicle's identical handling
		}

		strlen, p2, err := sav.ReadGamma(payload, p)
		if err != nil {
			return nil, fmt.Errorf("vehicle %d: reading name length: %w", sidx, err)
		}
		p = p2 + int(strlen)

		ov.UnitNumber = u16(payload[p:])
		p += 2
		ov.Owner = payload[p]
		p++
		ov.Tile = u32(payload[p:])
		p += 4
		p += 4 // dest_tile
		p += 4 // x_pos
		p += 4 // y_pos
		p += 4 // z_pos
		p++    // direction
		p++    // spritenum

		ov.EngineTypeOffset = p
		ov.EngineType = u16(payload[p:])
		p += 2
		p += 2 // cur_speed
		p++    // subspeed
		p++    // acceleration
		p++    // progress
		p++    // vehstatus
		p += 2 // last_station_visited
		p += 2 // last_loading_station

		ov.CargoType = payload[p]
		p++
		p++ // cargo_subtype
		ov.CargoCap = u16(payload[p:])

		out = append(out, ov)
	}
	return out, nil
}

// SetOtherEngineType patches a single road vehicle/ship/aircraft's
// engine_type field in place (2-byte, fixed-size -- never changes the
// record's length).
func SetOtherEngineType(payload []byte, ov *OtherVehicle, newEngineID uint16) {
	payload[ov.EngineTypeOffset] = byte(newEngineID >> 8)
	payload[ov.EngineTypeOffset+1] = byte(newEngineID)
}
