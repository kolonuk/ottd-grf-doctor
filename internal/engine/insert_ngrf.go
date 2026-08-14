package engine

import (
	"fmt"

	"github.com/kolonuk/ottd-grf-doctor/internal/sav"
)

// insertNGRF appends one new record to the save's NGRF chunk. Refuses to
// add a grfid that's already present (the caller should check first if it
// wants a friendlier error, but this is the last line of defense).
func insertNGRF(payload []byte, filename, grfidHex string, md5Sum [16]byte, version uint32, palette byte, params []uint32) ([]byte, error) {
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
	for _, e := range existing {
		if e.GRFID == grfidHex {
			return nil, fmt.Errorf("GRF %s is already present in this save (as %s)", grfidHex, e.Filename)
		}
	}

	newRecord, err := BuildNGRFRecord(filename, grfidHex, md5Sum, version, palette, params)
	if err != nil {
		return nil, err
	}

	var records [][]byte
	for _, e := range existing {
		records = append(records, e.Raw)
	}
	records = append(records, newRecord)

	newNGRF := sav.EncodeArrayChunk(ngrfChunk.ID, records)
	out := append([]byte(nil), payload[:ngrfChunk.Start]...)
	out = append(out, newNGRF...)
	out = append(out, payload[ngrfChunk.End:]...)
	return out, nil
}
