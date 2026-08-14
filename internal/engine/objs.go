package engine

import (
	"fmt"

	"github.com/kolonuk/ottd-grf-doctor/internal/sav"
)

// ObjectInstance is one fixed 18-byte record from the "OBJS" chunk: a
// placed object/scenery instance on the map (src/saveload/object_sl.cpp's
// _object_desc). Field presence is version-gated; at this save's version
// (194) colour (SLV_148), view (SLV_155), and type (SLV_186) are all
// present, giving a fixed 18-byte record -- verified against real
// savegame data (168 records, sane tile/date/colour values).
type ObjectInstance struct {
	Index      int
	Tile       uint32
	Width      uint16
	Height     uint16
	TownRef    uint32
	BuildDate  uint32
	Colour     uint8
	View       uint8
	ObjectType uint16
}

// ParseOBJS decodes every fixed 18-byte record in the save's OBJS chunk.
// This is a plain (non-sparse) Array chunk, so a destroyed object leaves a
// 0-length placeholder record at its old pool index to keep every later
// index aligned -- those are skipped, not errors (verified against real
// savegame data: dozens of such holes among the 168 raw records).
func ParseOBJS(payload []byte, chunk *sav.Chunk) ([]ObjectInstance, error) {
	instances := make([]ObjectInstance, 0, len(chunk.Records))
	for i, rec := range chunk.Records {
		if rec.Length == 0 {
			continue // deleted object, placeholder only
		}
		if rec.Length != 18 {
			return nil, fmt.Errorf("OBJS record %d: expected 18 bytes, got %d", i, rec.Length)
		}
		p := rec.Offset
		instances = append(instances, ObjectInstance{
			Index:      i,
			Tile:       beUint32(payload, p),
			Width:      uint16(payload[p+4]),
			Height:     uint16(payload[p+5]),
			TownRef:    beUint32(payload, p+6),
			BuildDate:  beUint32(payload, p+10),
			Colour:     payload[p+14],
			View:       payload[p+15],
			ObjectType: beUint16(payload, p+16),
		})
	}
	return instances, nil
}

func beUint32(b []byte, p int) uint32 {
	return uint32(b[p])<<24 | uint32(b[p+1])<<16 | uint32(b[p+2])<<8 | uint32(b[p+3])
}

func beUint16(b []byte, p int) uint16 {
	return uint16(b[p])<<8 | uint16(b[p+1])
}
