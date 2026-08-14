package engine

import (
	"fmt"

	"github.com/kolonuk/ottd-grf-doctor/internal/sav"
)

// removeVehicles relinks around and then deletes the given train
// VehicleIDs from the VEHS chunk. Relinking must happen first (it patches
// bytes in place using each vehicle's own NextRefOffset) so that any
// surviving vehicle whose "next" pointer targeted a removed vehicle is
// repointed at that removed vehicle's own next, preserving the consist
// chain. Cached consist properties (length, power, weight, etc.) are not
// touched here -- OpenTTD recomputes them itself during AfterLoadGame.
func removeVehicles(payload []byte, byID map[int]*TrainVehicle, removedIDs []int) ([]byte, error) {
	out := append([]byte(nil), payload...)
	removed := make(map[int]bool, len(removedIDs))
	for _, id := range removedIDs {
		removed[id] = true
	}

	// Relink: for every vehicle whose next hop is being removed, walk
	// forward past however many consecutive removed vehicles there are
	// and point at the first surviving one (or "none").
	for _, tv := range byID {
		if removed[tv.VehicleID] {
			continue
		}
		next := tv.NextVehicleID
		for next >= 0 && removed[int(next)] {
			nextTV, ok := byID[int(next)]
			if !ok {
				return nil, fmt.Errorf("vehicle %d's next ref points at unknown vehicle %d", tv.VehicleID, next)
			}
			next = nextTV.NextVehicleID
		}
		if next != tv.NextVehicleID {
			var raw uint32
			if next >= 0 {
				raw = uint32(next) + 1
			}
			out[tv.NextRefOffset] = byte(raw >> 24)
			out[tv.NextRefOffset+1] = byte(raw >> 16)
			out[tv.NextRefOffset+2] = byte(raw >> 8)
			out[tv.NextRefOffset+3] = byte(raw)
		}
	}

	// Re-walk VEHS fresh (post-relink) so we operate on this call's own
	// up-to-date chunk offsets, and drop the records for removed vehicles.
	chunks, err := sav.WalkChunks(out)
	if err != nil {
		return nil, err
	}
	var vehsChunk *sav.Chunk
	for _, c := range chunks {
		if c.IDString() == "VEHS" {
			vehsChunk = c
			break
		}
	}
	if vehsChunk == nil {
		return nil, fmt.Errorf("VEHS chunk not found")
	}

	var kept [][]byte
	for _, rec := range vehsChunk.Records {
		sidx, _, err := sav.ReadGamma(out, rec.Offset)
		if err != nil {
			return nil, fmt.Errorf("re-reading VEHS sparse index: %w", err)
		}
		if removed[int(sidx)] {
			continue
		}
		kept = append(kept, append([]byte(nil), out[rec.Offset:rec.Offset+rec.Length]...))
	}

	newVehs := sav.EncodeSparseArrayChunk(vehsChunk.ID, kept)
	final := append([]byte(nil), out[:vehsChunk.Start]...)
	final = append(final, newVehs...)
	final = append(final, out[vehsChunk.End:]...)
	return final, nil
}
