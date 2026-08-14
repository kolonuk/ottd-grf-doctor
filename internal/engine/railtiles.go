package engine

import (
	"fmt"

	"github.com/kolonuk/ottd-grf-doctor/internal/sav"
)

// ParseRailtypeLabels decodes the save's "RAIL" chunk: index = the small
// integer railtype ID stored per map tile, value = the 4-character label
// (e.g. "MONO"). Unused slots are all-zero and returned as "".
func ParseRailtypeLabels(payload []byte, chunk *sav.Chunk) ([]string, error) {
	if chunk == nil {
		return nil, fmt.Errorf("RAIL chunk not found")
	}
	labels := make([]string, len(chunk.Records))
	for i, rec := range chunk.Records {
		if rec.Length != 4 {
			return nil, fmt.Errorf("RAIL record %d: expected 4 bytes, got %d", i, rec.Length)
		}
		b := payload[rec.Offset : rec.Offset+rec.Length]
		if b[0] == 0 {
			continue // unused slot
		}
		labels[i] = string(b)
	}
	return labels, nil
}

// TileType mirrors the subset of OpenTTD's TileType enum this package
// cares about for railtype lookups (see src/tile_type.h).
type TileType uint8

const (
	TileTypeClear TileType = iota
	TileTypeRailway
	TileTypeRoad
	TileTypeHouse
	TileTypeTrees
	TileTypeStation
	TileTypeWater
	TileTypeVoid
	TileTypeIndustry
	TileTypeTunnelBridge
	TileTypeObject
)

// MapTiles gives direct access to the per-tile MAPT (tile type) and M3LO
// (railtype-bearing byte, at this tool's target savegame version) planes,
// for looking up what railtype is actually built under a given vehicle.
type MapTiles struct {
	DimX, DimY int
	mapt       []byte
	m3lo       []byte
}

// LoadMapTiles reads the MAPS/MAPT/M3LO chunks needed to answer "what
// railtype is tile N". NOTE: at savegame versions >= SLV_ExtendRailtypes
// (not this tool's target version, 194), railtype moves from the low
// nibble of m3 to its own m8 plane -- see afterload.cpp's comment "Railtype
// moved from m3 to m8". This function only implements the pre-move (m3)
// layout; a newer-savegame caller would need to read MAP8 instead.
func LoadMapTiles(payload []byte, cm sav.ChunkMap) (*MapTiles, error) {
	mapsChunk, ok := cm["MAPS"]
	if !ok {
		return nil, fmt.Errorf("MAPS chunk not found")
	}
	if mapsChunk.RiffLength < 8 {
		return nil, fmt.Errorf("MAPS chunk too short")
	}
	b := payload[mapsChunk.RiffOffset : mapsChunk.RiffOffset+8]
	dimX := int(u32(b[0:4]))
	dimY := int(u32(b[4:8]))

	maptChunk, ok := cm["MAPT"]
	if !ok {
		return nil, fmt.Errorf("MAPT chunk not found")
	}
	m3loChunk, ok := cm["M3LO"]
	if !ok {
		return nil, fmt.Errorf("M3LO chunk not found")
	}

	return &MapTiles{
		DimX: dimX,
		DimY: dimY,
		mapt: payload[maptChunk.RiffOffset : maptChunk.RiffOffset+maptChunk.RiffLength],
		m3lo: payload[m3loChunk.RiffOffset : m3loChunk.RiffOffset+m3loChunk.RiffLength],
	}, nil
}

// TileType returns the tile type at the given raw tile index (as stored
// in a Vehicle's `tile` field: y*DimX+x).
func (m *MapTiles) TileType(tile uint32) TileType {
	if int(tile) >= len(m.mapt) {
		return TileTypeVoid
	}
	return TileType((m.mapt[tile] >> 4) & 0xF)
}

// RailtypeIndex returns the small railtype ID (an index into
// ParseRailtypeLabels' result) stored for the given tile, valid only when
// TileType is one that can carry rail (Railway, or Road with a level
// crossing, or Station, or TunnelBridge -- this function does not
// distinguish those sub-cases, matching how OpenTTD itself applies the
// same GB(m3,0,4) read across all of them; see afterload.cpp).
func (m *MapTiles) RailtypeIndex(tile uint32) uint8 {
	if int(tile) >= len(m.m3lo) {
		return 0
	}
	return m.m3lo[tile] & 0xF
}
