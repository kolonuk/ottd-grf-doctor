// Package sav implements structural (field-agnostic) reading and writing of
// OpenTTD savegame (.sav) files: header, LZMA/xz payload, and the chunk
// container format (RIFF-style blobs and gamma-length-prefixed record
// arrays). It does not know the meaning of any specific chunk's fields --
// higher-level packages (internal/engine) interpret specific chunks.
//
// Format reference: OpenTTD src/saveload/saveload.cpp. See README.md for a
// summary of the byte layout this package implements.
package sav

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"github.com/ulikunitz/xz"
)

func readFile(path string) ([]byte, error)     { return os.ReadFile(path) }
func writeFile(path string, data []byte) error { return os.WriteFile(path, data, 0o644) }

// ChunkType mirrors OpenTTD's saveload.h ChunkType enum (low nibble of the
// per-chunk type byte).
type ChunkType uint8

const (
	ChunkRiff        ChunkType = 0
	ChunkArray       ChunkType = 1
	ChunkSparseArray ChunkType = 2
	ChunkTable       ChunkType = 3
	ChunkSparseTable ChunkType = 4
)

// Record is one length-prefixed record within an Array/SparseArray/
// Table/SparseTable chunk. Offset/Length are relative to the decompressed
// Payload. For SparseArray/SparseTable chunks, Data includes the leading
// gamma-encoded sparse index as OpenTTD's own format does.
type Record struct {
	Offset int
	Length int
}

// Chunk is one top-level chunk found while walking the payload.
type Chunk struct {
	ID    [4]byte
	Type  ChunkType
	Start int // offset of the chunk id byte in Payload
	End   int // offset just past the chunk's content

	// Riff chunks only:
	RiffOffset int
	RiffLength int

	// Array/SparseArray/Table/SparseTable chunks only:
	Records []Record
}

func (c *Chunk) IDString() string { return string(c.ID[:]) }

// Save holds a parsed savegame: the raw 8-byte header and the decompressed
// chunk payload.
type Save struct {
	Header       [8]byte
	MajorVersion uint16
	MinorVersion uint16
	Payload      []byte
}

// Load reads and decompresses an OTTX (LZMA/xz) savegame file into memory.
// Other compression tags (OTTD/LZO, OTTZ/zlib, OTTN/none) are not
// implemented since every save this tool targets uses OTTX.
func Load(path string) (*Save, error) {
	data, err := readFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) < 8 {
		return nil, fmt.Errorf("file too short to be a savegame: %d bytes", len(data))
	}
	tag := string(data[0:4])
	if tag != "OTTX" {
		return nil, fmt.Errorf("unsupported savegame tag %q (only OTTX/lzma is implemented)", tag)
	}
	s := &Save{}
	copy(s.Header[:], data[0:8])
	s.MajorVersion = uint16(data[4])<<8 | uint16(data[5])
	s.MinorVersion = uint16(data[6])<<8 | uint16(data[7])

	r, err := xz.NewReader(bytes.NewReader(data[8:]))
	if err != nil {
		return nil, fmt.Errorf("opening xz stream: %w", err)
	}
	payload, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("decompressing payload: %w", err)
	}
	s.Payload = payload
	return s, nil
}

// Save writes the header followed by an LZMA-compressed (xz-container)
// payload, matching what OpenTTD's lzma_easy_encoder produces (a standard
// .xz stream with CRC32 check), to path.
func (s *Save) SaveTo(path string) error {
	var buf bytes.Buffer
	buf.Write(s.Header[:])

	w, err := xz.WriterConfig{CheckSum: xz.CRC32}.NewWriter(&buf)
	if err != nil {
		return fmt.Errorf("creating xz writer: %w", err)
	}
	if _, err := w.Write(s.Payload); err != nil {
		return fmt.Errorf("writing payload: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("closing xz writer: %w", err)
	}
	return writeFile(path, buf.Bytes())
}

// ReadGamma decodes one OpenTTD "simple gamma" variable-length value
// starting at payload[pos]. Returns the value and the position just past it.
func ReadGamma(payload []byte, pos int) (uint32, int, error) {
	if pos >= len(payload) {
		return 0, pos, io.ErrUnexpectedEOF
	}
	i := uint32(payload[pos])
	pos++
	if i&0x80 != 0 {
		i &^= 0x80
		if i&0x40 != 0 {
			i &^= 0x40
			if i&0x20 != 0 {
				i &^= 0x20
				if i&0x10 != 0 {
					i &^= 0x10
					if i&0x08 != 0 {
						return 0, pos, fmt.Errorf("unsupported 5-byte gamma at offset %d", pos)
					}
					if pos >= len(payload) {
						return 0, pos, io.ErrUnexpectedEOF
					}
					i = uint32(payload[pos])
					pos++
				}
				if pos >= len(payload) {
					return 0, pos, io.ErrUnexpectedEOF
				}
				i = (i << 8) | uint32(payload[pos])
				pos++
			}
			if pos >= len(payload) {
				return 0, pos, io.ErrUnexpectedEOF
			}
			i = (i << 8) | uint32(payload[pos])
			pos++
		}
		if pos >= len(payload) {
			return 0, pos, io.ErrUnexpectedEOF
		}
		i = (i << 8) | uint32(payload[pos])
		pos++
	}
	return i, pos, nil
}

// WriteGamma encodes value using OpenTTD's simple-gamma variable length
// scheme (see SlWriteSimpleGamma in saveload.cpp).
func WriteGamma(value uint32) []byte {
	var out []byte
	switch {
	case value >= (1 << 28):
		out = append(out, 0xF0, byte(value>>24))
	case value >= (1 << 21):
		out = append(out, 0xE0|byte(value>>24))
	case value >= (1 << 14):
		out = append(out, 0xC0|byte(value>>16))
	case value >= (1 << 7):
		out = append(out, 0x80|byte(value>>8))
	}
	switch {
	case value >= (1 << 21):
		out = append(out, byte(value>>16))
		out = append(out, byte(value>>8))
	case value >= (1 << 14):
		out = append(out, byte(value>>8))
	}
	out = append(out, byte(value))
	return out
}

// WalkChunks parses payload into a sequence of top-level Chunks, in file
// order, stopping at the all-zero terminator chunk id.
func WalkChunks(payload []byte) ([]*Chunk, error) {
	var chunks []*Chunk
	pos := 0
	n := len(payload)
	for {
		if pos+4 > n {
			return nil, fmt.Errorf("truncated payload while reading chunk id at offset %d", pos)
		}
		var id [4]byte
		copy(id[:], payload[pos:pos+4])
		pos += 4
		if id == ([4]byte{0, 0, 0, 0}) {
			break
		}
		if pos >= n {
			return nil, fmt.Errorf("truncated payload reading chunk type byte for %q", id)
		}
		m := payload[pos]
		pos++
		ct := ChunkType(m & 0xF)
		chunk := &Chunk{ID: id, Type: ct, Start: pos - 5}

		switch ct {
		case ChunkRiff:
			if pos+3 > n {
				return nil, fmt.Errorf("truncated RIFF header for chunk %q", id)
			}
			length := (uint32(payload[pos]) << 16) | (uint32(m>>4) << 24)
			pos++
			length += uint32(payload[pos])<<8 | uint32(payload[pos+1])
			pos += 2
			if pos+int(length) > n {
				return nil, fmt.Errorf("RIFF chunk %q length %d exceeds payload", id, length)
			}
			chunk.RiffOffset = pos
			chunk.RiffLength = int(length)
			pos += int(length)
		case ChunkArray, ChunkSparseArray, ChunkTable, ChunkSparseTable:
			for {
				glen, p2, err := ReadGamma(payload, pos)
				if err != nil {
					return nil, fmt.Errorf("reading record length in chunk %q: %w", id, err)
				}
				if glen == 0 {
					pos = p2
					break
				}
				recOff := p2
				recLen := int(glen) - 1
				if recOff+recLen > n {
					return nil, fmt.Errorf("record in chunk %q overruns payload", id)
				}
				chunk.Records = append(chunk.Records, Record{Offset: recOff, Length: recLen})
				pos = recOff + recLen
			}
		default:
			return nil, fmt.Errorf("unknown chunk type %d for chunk %q at offset %d", ct, id, chunk.Start)
		}
		chunk.End = pos
		chunks = append(chunks, chunk)
	}
	return chunks, nil
}

// ChunkMap is a convenience alias for looking up chunks by 4-byte id string.
type ChunkMap map[string]*Chunk

// ChunkMapOf builds a ChunkMap from the result of WalkChunks.
func ChunkMapOf(chunks []*Chunk) ChunkMap {
	m := make(ChunkMap, len(chunks))
	for _, c := range chunks {
		m[c.IDString()] = c
	}
	return m
}

// EncodeArrayChunk rebuilds a full Array-type chunk's bytes (id + type byte
// + gamma-length-prefixed records + zero terminator) from a list of raw
// record payloads, in order.
func EncodeArrayChunk(id [4]byte, records [][]byte) []byte {
	return encodeChunk(id, ChunkArray, records)
}

// EncodeSparseArrayChunk is like EncodeArrayChunk but for SparseArray-type
// chunks (e.g. VEHS): each record's raw bytes must already include its own
// leading gamma-encoded sparse index, exactly as WalkChunks captured it.
func EncodeSparseArrayChunk(id [4]byte, records [][]byte) []byte {
	return encodeChunk(id, ChunkSparseArray, records)
}

func encodeChunk(id [4]byte, ct ChunkType, records [][]byte) []byte {
	var buf bytes.Buffer
	buf.Write(id[:])
	buf.WriteByte(byte(ct))
	for _, rec := range records {
		buf.Write(WriteGamma(uint32(len(rec) + 1)))
		buf.Write(rec)
	}
	buf.Write(WriteGamma(0))
	return buf.Bytes()
}
