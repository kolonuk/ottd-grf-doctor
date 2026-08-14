package engine

import (
	"fmt"

	"github.com/kolonuk/ottd-grf-doctor/internal/sav"
)

// Ground-vehicle subtype flag bits (GroundVehicleSubtypeFlags in
// src/ground_vehicle.hpp). Only the bits this tool needs to reason about
// are named here.
const (
	GVSFFront       = 0x01
	GVSFArticulated = 0x02
	GVSFWagon       = 0x04
	GVSFEngine      = 0x08
	GVSFFreeWagon   = 0x10
	GVSFMultiheaded = 0x20
)

// TrainVehicle is one parsed VEHS record for a train-type vehicle (the
// fields this tool needs, not the full ~80-field record).
type TrainVehicle struct {
	VehicleID    int // sparse array index, i.e. the game's persistent VehicleID
	RecordOffset int // absolute payload offset of the record's content start (right after the type byte)
	RecordLength int

	Subtype       uint8
	NextVehicleID int32 // -1 if none (raw ref value 0 means "no next vehicle")
	NextRefOffset int   // absolute payload offset of the raw 4-byte next-vehicle ref field
	UnitNumber    uint16
	Owner         uint8
	Tile          uint32
	Direction     uint8
	SpriteNum     uint8
	EngineType    uint16 // this is the EngineID -- the field this tool exists to fix
	CargoType     uint8
	CargoCap      uint16

	// Absolute payload offsets of specific fields, for in-place patching.
	SubtypeOffset    int
	EngineTypeOffset int
}

func (t *TrainVehicle) IsFront() bool       { return t.Subtype&GVSFFront != 0 }
func (t *TrainVehicle) IsEngine() bool      { return t.Subtype&GVSFEngine != 0 }
func (t *TrainVehicle) IsMultiheaded() bool { return t.Subtype&GVSFMultiheaded != 0 }
func (t *TrainVehicle) IsWagon() bool       { return t.Subtype&GVSFWagon != 0 }

// ParseVEHS decodes every train-type (type byte 0) record in the save's
// VEHS chunk. Non-train records are skipped. Field offsets follow the
// SlVehicleCommon layout in src/saveload/vehicle_sl.cpp AS IT APPLIES AT
// SAVEGAME VERSION 194 specifically: many of that struct's fields are
// conditional on savegame version (SLE_CONDVAR), so this offset table is
// only correct for saves at that exact version. Supporting a range of
// versions would require evaluating each field's version bounds against
// the save's actual major version, the way OpenTTD's own loader does --
// out of scope for now (this tool targets one known save format).
func ParseVEHS(payload []byte, chunk *sav.Chunk) ([]TrainVehicle, error) {
	var out []TrainVehicle
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
		if vtype != 0 {
			continue // not a train
		}
		contentStart := p
		tv := TrainVehicle{
			VehicleID:    int(sidx),
			RecordOffset: contentStart,
			RecordLength: rec.Offset + rec.Length - contentStart,
		}

		tv.SubtypeOffset = p
		tv.Subtype = payload[p]
		p++

		tv.NextRefOffset = p
		nextRef := u32(payload[p:])
		p += 4
		if nextRef == 0 {
			tv.NextVehicleID = -1
		} else {
			// OpenTTD stores vehicle references as index+1 (0 means "no
			// reference"); IntToReference subtracts 1 during the Ptrs
			// fixup pass. Verified against the working Python analysis
			// this project's savegame investigation used.
			tv.NextVehicleID = int32(nextRef - 1)
		}

		strlen, p2, err := sav.ReadGamma(payload, p)
		if err != nil {
			return nil, fmt.Errorf("vehicle %d: reading name length: %w", sidx, err)
		}
		p = p2 + int(strlen) // skip name bytes

		tv.UnitNumber = u16(payload[p:])
		p += 2
		tv.Owner = payload[p]
		p++
		tv.Tile = u32(payload[p:])
		p += 4
		p += 4 // dest_tile
		p += 4 // x_pos
		p += 4 // y_pos
		p += 4 // z_pos
		tv.Direction = payload[p]
		p++
		tv.SpriteNum = payload[p]
		p++

		tv.EngineTypeOffset = p
		tv.EngineType = u16(payload[p:])
		p += 2
		p += 2 // cur_speed
		p++    // subspeed
		p++    // acceleration
		// NOTE: motion_counter (u32) only exists from SLV VehMotionCounter
		// (288) onward -- absent at this tool's target savegame version
		// (194), so it is deliberately NOT skipped here. If this package
		// ever needs to support newer savegames, this offset table needs
		// to become version-conditional like OpenTTD's own SLE_COND*
		// macros (see doc comment on ParseVEHS).
		p++    // progress
		p++    // vehstatus
		p += 2 // last_station_visited
		p += 2 // last_loading_station

		tv.CargoType = payload[p]
		p++
		p++ // cargo_subtype
		tv.CargoCap = u16(payload[p:])
		// (fields after cargo_cap are not needed by this tool)

		out = append(out, tv)
	}
	return out, nil
}

// SetEngineType patches a single train vehicle's engine_type field in
// place (2-byte, fixed-size -- never changes the record's length).
func SetEngineType(payload []byte, tv *TrainVehicle, newEngineID uint16) {
	payload[tv.EngineTypeOffset] = byte(newEngineID >> 8)
	payload[tv.EngineTypeOffset+1] = byte(newEngineID)
}

// SetSubtype patches a single train vehicle's subtype byte in place.
//
// IMPORTANT: if you reassign a vehicle's engine_type away from a
// multiheaded-pair partner's engine_type, you MUST also clear
// GVSFMultiheaded (and GVSFEngine/GVSFFront if not genuinely a front
// engine) here. OpenTTD's ConnectMultiheadedTrains() (afterload.cpp, runs
// on every load) walks each consist looking for cars with
// IsMultiheaded() && !IsEngine(): for those it force-calls SetEngine()
// and decrements spritenum while searching for a partner with a
// *matching* engine_type. If you broke the pairing without clearing this
// flag, that mutation runs against a plain wagon engine that was never
// designed for it, which is exactly what caused a real crash during this
// project's development (see commit history). Prefer plain GVSFWagon
// (0x04) for any vehicle no longer part of a genuine pair.
func SetSubtype(payload []byte, tv *TrainVehicle, newSubtype uint8) {
	payload[tv.SubtypeOffset] = newSubtype
	tv.Subtype = newSubtype
}

func u16(b []byte) uint16 { return uint16(b[0])<<8 | uint16(b[1]) }
func u32(b []byte) uint32 {
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}
