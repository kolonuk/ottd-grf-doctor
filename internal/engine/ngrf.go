package engine

import (
	"crypto/md5"
	"fmt"
	"os"

	"github.com/kolonuk/ottd-grf-doctor/internal/sav"
)

// NGRFEntry is one loaded-NewGRF record from the "NGRF" chunk.
type NGRFEntry struct {
	Index    int // position within the NGRF chunk (not a stable id -- order can change)
	Filename string
	GRFID    string // 8 hex chars, memory-order (matches what OpenTTD/BaNaNaS display)
	MD5      string // 32 hex chars
	Version  uint32
	Palette  byte
	Raw      []byte // full original record bytes, for entries we don't touch
}

// grfidToDisk reverses a memory-order grfid hex string into the 4 disk
// bytes OpenTTD's LABEL_REVERSE encoding uses (see EIDS/NGRF grfid fields).
func grfidToDisk(hex string) ([4]byte, error) {
	b, err := hexDecode4(hex)
	if err != nil {
		return [4]byte{}, err
	}
	return [4]byte{b[3], b[2], b[1], b[0]}, nil
}

func diskToGRFID(b []byte) string {
	return fmt.Sprintf("%02X%02X%02X%02X", b[3], b[2], b[1], b[0])
}

// ParseNGRF decodes every record in the save's NGRF chunk. Only the leading
// fields needed for identification (filename, grfid) plus md5/version/
// palette are decoded; the full record bytes are kept in Raw so untouched
// entries can be round-tripped byte-for-byte.
func ParseNGRF(payload []byte, chunk *sav.Chunk) ([]NGRFEntry, error) {
	entries := make([]NGRFEntry, 0, len(chunk.Records))
	for i, rec := range chunk.Records {
		p := rec.Offset
		strlen, p2, err := sav.ReadGamma(payload, p)
		if err != nil {
			return nil, fmt.Errorf("NGRF record %d: reading filename length: %w", i, err)
		}
		p = p2
		filename := string(payload[p : p+int(strlen)])
		p += int(strlen)
		grfidBytes := payload[p : p+4]
		p += 4
		md5Bytes := payload[p : p+16]
		p += 16
		// version (u32) is only absent on savegames older than SLV
		// StoreNewGRFVersion(151); every save this tool targets is newer.
		version := uint32(payload[p])<<24 | uint32(payload[p+1])<<16 | uint32(payload[p+2])<<8 | uint32(payload[p+3])
		palette := payload[rec.Offset+rec.Length-1]

		entries = append(entries, NGRFEntry{
			Index:    i,
			Filename: filename,
			GRFID:    diskToGRFID(grfidBytes),
			MD5:      fmt.Sprintf("%X", md5Bytes),
			Version:  version,
			Palette:  palette,
			Raw:      append([]byte(nil), payload[rec.Offset:rec.Offset+rec.Length]...),
		})
	}
	return entries, nil
}

// BuildNGRFRecord encodes one full NGRF record for a brand-new GRF entry
// (128-slot zero-padded param array, matching the fixed size this
// savegame's field layout uses -- verified empirically against real save
// data; see README.md).
func BuildNGRFRecord(filename, grfidHex string, md5Sum [16]byte, version uint32, palette byte, params []uint32) ([]byte, error) {
	if len(params) > 128 {
		return nil, fmt.Errorf("too many params: %d (max 128)", len(params))
	}
	grfidDisk, err := grfidToDisk(grfidHex)
	if err != nil {
		return nil, fmt.Errorf("bad grfid %q: %w", grfidHex, err)
	}
	var buf []byte
	fname := []byte(filename)
	buf = append(buf, sav.WriteGamma(uint32(len(fname)))...)
	buf = append(buf, fname...)
	buf = append(buf, grfidDisk[:]...)
	buf = append(buf, md5Sum[:]...)
	buf = append(buf, byte(version>>24), byte(version>>16), byte(version>>8), byte(version))
	padded := make([]uint32, 128)
	copy(padded, params)
	for _, p := range padded {
		buf = append(buf, byte(p>>24), byte(p>>16), byte(p>>8), byte(p))
	}
	buf = append(buf, byte(len(params)))
	buf = append(buf, palette)
	return buf, nil
}

// MD5File computes the MD5 checksum of a local .grf file for embedding
// into a new NGRF record.
func MD5File(path string) ([16]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return [16]byte{}, err
	}
	return md5.Sum(data), nil
}

func hexDecode4(s string) ([4]byte, error) {
	var out [4]byte
	if len(s) != 8 {
		return out, fmt.Errorf("expected 8 hex chars, got %q", s)
	}
	for i := 0; i < 4; i++ {
		v, err := hexByte(s[i*2 : i*2+2])
		if err != nil {
			return out, err
		}
		out[i] = v
	}
	return out, nil
}

func hexByte(s string) (byte, error) {
	var v byte
	_, err := fmt.Sscanf(s, "%02X", &v)
	return v, err
}
