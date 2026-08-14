// Package grf implements a partial, pragmatic parser for the NewGRF
// binary format ("container format 2", used by virtually all NewGRFs
// released in the last decade -- see below), extracting exactly what
// this tool needs to make matching and warnings dynamic instead of
// relying on a hardcoded table: for each vehicle a GRF defines, its
// local ID, name (best-effort), track type, introduction date, model
// life (retirement), speed, power, weight, capacity and cargo type --
// and, for object/scenery GRFs, the object IDs and names they define.
//
// This is NOT a full NewGRF interpreter. Real GRFs can compute
// properties conditionally via variational actions (varaction2) and
// callbacks; this parser reads the STATIC property values set by
// Action0, which is what the vast majority of real-world GRFs actually
// use for the properties this tool cares about (introduction date, model
// life, track type, base stats). Where a GRF genuinely computes one of
// these dynamically, the parsed value may be a placeholder/default
// rather than the true in-game value -- see ParsedEngine's doc comment.
//
// Format reference: OpenTTD's own reader (src/newgrf.cpp,
// src/newgrf/newgrf_act0*.cpp), read directly from source during this
// package's development rather than from secondary documentation.
package grf

import (
	"encoding/binary"
	"fmt"
	"os"
)

// container2Signature is the fixed byte sequence identifying NewGRF
// "container format 2" (in use since OpenTTD 1.2 / NML's default output
// for many years now -- every GRF this tool has encountered uses it).
var container2Signature = []byte{0x47, 0x52, 0x46, 0x82, 0x0D, 0x0A, 0x1A, 0x0A} // "GRF" + magic

// PseudoSprite is one Action-block (type byte 0xFF) found while walking
// the container; Data is the raw bytes starting with the action number.
type PseudoSprite struct {
	Data []byte
}

// walkContainer2 reads every pseudo-sprite (type 0xFF block) in a
// container-2 GRF file, skipping real graphic sprites (any other type
// byte) by their declared size without decoding them -- this tool never
// needs pixel data.
func walkContainer2(data []byte) ([]PseudoSprite, error) {
	if len(data) < 14 {
		return nil, fmt.Errorf("file too short to be a container-2 GRF")
	}
	if data[0] != 0 || data[1] != 0 {
		return nil, fmt.Errorf("not container format 2 (missing 00 00 preamble) -- container format 1 is not supported")
	}
	if string(data[2:10]) != string(container2Signature) {
		return nil, fmt.Errorf("not a valid NewGRF: signature mismatch")
	}
	pos := 10
	pos += 4 // offset-to-data field (only meaningful for the legacy single-pass sprite cache; unused here)
	if pos >= len(data) {
		return nil, fmt.Errorf("truncated header")
	}
	compression := data[pos]
	pos++
	if compression != 0 {
		return nil, fmt.Errorf("compressed sprite data (compression=%d) is not supported", compression)
	}

	var out []PseudoSprite
	for pos+4 <= len(data) {
		size := binary.LittleEndian.Uint32(data[pos:])
		pos += 4
		if size == 0 {
			break // end of pseudo/real sprite section
		}
		if pos >= len(data) {
			return out, fmt.Errorf("truncated sprite block")
		}
		typ := data[pos]
		pos++
		if typ == 0xFF {
			if pos+int(size) > len(data) {
				return out, fmt.Errorf("truncated pseudo-sprite (declared size %d)", size)
			}
			out = append(out, PseudoSprite{Data: append([]byte(nil), data[pos:pos+int(size)]...)})
			pos += int(size)
		} else if typ == 0xFD {
			// Reference to the data section (container v2 only): the
			// whole declared size is skipped as-is.
			if pos+int(size) > len(data) {
				return out, fmt.Errorf("truncated sprite data-section reference")
			}
			pos += int(size)
		} else {
			// Real graphic sprite. Matches OpenTTD's own scan-and-skip
			// path exactly (src/newgrf.cpp's LoadNewGRFFileFromFile loop
			// + src/spritecache.cpp's SkipSpriteData, read directly from
			// source during this package's development): a fixed 7-byte
			// sub-header follows the type byte, then the remaining
			// (size-8) bytes are either a flat skip (type bit 1 set,
			// "uncompressed") or an RLE stream that must be walked
			// control-byte by control-byte to skip exactly that many
			// logical bytes -- a flat skip desyncs the whole rest of the
			// file for compressed sprites, which is most of them.
			if pos+7 > len(data) {
				return out, fmt.Errorf("truncated real sprite sub-header")
			}
			pos += 7
			remaining := int(size) - 8
			if remaining < 0 {
				return out, fmt.Errorf("real sprite declared size %d too small for its own sub-header", size)
			}
			newPos, err := skipSpriteData(data, pos, typ, remaining)
			if err != nil {
				return out, err
			}
			pos = newPos
		}
	}
	return out, nil
}

func loadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// skipSpriteData ports SkipSpriteData from src/spritecache.cpp exactly:
// if typ's bit 1 (0x02, "uncompressed") is set, num is a flat byte
// count to skip. Otherwise the data is RLE-encoded and must be walked
// control-byte by control-byte -- there is no way to know where it ends
// without doing so. Returns the position just past the skipped sprite.
func skipSpriteData(data []byte, pos int, typ uint8, num int) (int, error) {
	if typ&2 != 0 {
		if pos+num > len(data) {
			return 0, fmt.Errorf("truncated uncompressed real sprite data")
		}
		return pos + num, nil
	}
	for num > 0 {
		if pos >= len(data) {
			return 0, fmt.Errorf("truncated compressed real sprite data")
		}
		i := int8(data[pos])
		pos++
		if i >= 0 {
			size := int(i)
			if size == 0 {
				size = 0x80
			}
			if size > num {
				return 0, fmt.Errorf("compressed real sprite run (size %d) exceeds remaining data (%d)", size, num)
			}
			num -= size
			pos += size
			if pos > len(data) {
				return 0, fmt.Errorf("truncated compressed real sprite run")
			}
		} else {
			run := int(-(int(i) >> 3))
			num -= run
			if pos >= len(data) {
				return 0, fmt.Errorf("truncated compressed real sprite backref")
			}
			pos++ // the single "offset" byte following a backref control byte
		}
	}
	return pos, nil
}
