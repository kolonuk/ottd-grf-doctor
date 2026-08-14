package engine

import (
	"fmt"

	"github.com/kolonuk/ottd-grf-doctor/internal/sav"
)

// removeNGRFEntries drops every NGRF record whose grfid is in remove,
// leaving all others byte-for-byte untouched. Used to clean up a NewGRF
// that no vehicle references anymore after a fix (e.g. the GRF that used
// to be loaded before the user swapped it for a replacement).
func removeNGRFEntries(payload []byte, remove map[string]bool) ([]byte, error) {
	if len(remove) == 0 {
		return payload, nil
	}
	chunks, err := sav.WalkChunks(payload)
	if err != nil {
		return nil, err
	}
	var ngrfChunk *sav.Chunk
	for _, c := range chunks {
		if c.IDString() == "NGRF" {
			ngrfChunk = c
			break
		}
	}
	if ngrfChunk == nil {
		return nil, fmt.Errorf("NGRF chunk not found")
	}

	existing, err := ParseNGRF(payload, ngrfChunk)
	if err != nil {
		return nil, err
	}

	var records [][]byte
	for _, e := range existing {
		if remove[e.GRFID] {
			continue
		}
		records = append(records, e.Raw)
	}

	newNGRF := sav.EncodeArrayChunk(ngrfChunk.ID, records)
	out := append([]byte(nil), payload[:ngrfChunk.Start]...)
	out = append(out, newNGRF...)
	out = append(out, payload[ngrfChunk.End:]...)
	return out, nil
}
