package grf

import "fmt"

// byteReader is a small sequential reader over one pseudo-sprite's bytes,
// mirroring the subset of OpenTTD's ByteReader this package needs
// (src/newgrf/newgrf_bytereader.h).
type byteReader struct {
	data []byte
	pos  int
}

func newByteReader(data []byte) *byteReader { return &byteReader{data: data} }

func (r *byteReader) HasData(n int) bool { return r.pos+n <= len(r.data) }

func (r *byteReader) ReadByte() (byte, error) {
	if !r.HasData(1) {
		return 0, fmt.Errorf("unexpected end of data reading byte")
	}
	v := r.data[r.pos]
	r.pos++
	return v, nil
}

func (r *byteReader) ReadWord() (uint16, error) {
	if !r.HasData(2) {
		return 0, fmt.Errorf("unexpected end of data reading word")
	}
	v := uint16(r.data[r.pos]) | uint16(r.data[r.pos+1])<<8
	r.pos += 2
	return v, nil
}

func (r *byteReader) ReadDWord() (uint32, error) {
	if !r.HasData(4) {
		return 0, fmt.Errorf("unexpected end of data reading dword")
	}
	v := uint32(r.data[r.pos]) | uint32(r.data[r.pos+1])<<8 | uint32(r.data[r.pos+2])<<16 | uint32(r.data[r.pos+3])<<24
	r.pos += 4
	return v, nil
}

// ReadExtendedByte matches OpenTTD's "E" encoding: a single byte, unless
// it's 0xFF, in which case it's followed by a 2-byte little-endian word
// that is the real value.
func (r *byteReader) ReadExtendedByte() (uint32, error) {
	b, err := r.ReadByte()
	if err != nil {
		return 0, err
	}
	if b != 0xFF {
		return uint32(b), nil
	}
	w, err := r.ReadWord()
	if err != nil {
		return 0, err
	}
	return uint32(w), nil
}

func (r *byteReader) Skip(n int) error {
	if !r.HasData(n) {
		return fmt.Errorf("unexpected end of data skipping %d bytes", n)
	}
	r.pos += n
	return nil
}

func (r *byteReader) ReadBytes(n int) ([]byte, error) {
	if !r.HasData(n) {
		return nil, fmt.Errorf("unexpected end of data reading %d bytes", n)
	}
	v := r.data[r.pos : r.pos+n]
	r.pos += n
	return v, nil
}

// ReadString0 reads a NUL-terminated string (used by Action4).
func (r *byteReader) ReadString0() (string, error) {
	start := r.pos
	for r.pos < len(r.data) {
		if r.data[r.pos] == 0 {
			s := string(r.data[start:r.pos])
			r.pos++
			return s, nil
		}
		r.pos++
	}
	return "", fmt.Errorf("unterminated string")
}
