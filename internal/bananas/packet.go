// Package bananas implements just enough of OpenTTD's content-service
// protocol to search the live BaNaNaS catalog and download a specific
// NewGRF, matching what the real game client does (see
// src/network/core/tcp_content.h, src/network/network_content.cpp, and
// src/network/core/packet.cpp in the OpenTTD source for the reference
// implementation this package was derived from).
//
// Packet wire format (see Packet::PrepareToSend/Send_uintN in
// packet.cpp): 2-byte little-endian total size (including these 2 bytes
// and the 1-byte type that follows), 1-byte packet type, then payload.
// All multi-byte integers are little-endian. Strings are UTF-8 with a
// trailing NUL, no length prefix.
package bananas

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
)

// PacketContentType mirrors OpenTTD's PacketContentType enum.
type PacketContentType uint8

const (
	PacketClientInfoList     PacketContentType = 0
	PacketClientInfoID       PacketContentType = 1
	PacketClientInfoExtID    PacketContentType = 2
	PacketClientInfoExtIDMD5 PacketContentType = 3
	PacketServerInfo         PacketContentType = 4
	PacketClientContent      PacketContentType = 5
	PacketServerContent      PacketContentType = 6
)

// ContentType mirrors OpenTTD's ContentType enum (only the value this
// package uses is named; others are passed through as raw bytes where
// needed). See src/network/core/tcp_content_type.h.
const ContentTypeNewGRF uint8 = 2

// packetWriter builds one outgoing packet's payload; call Bytes() to get
// the final framed packet (size + type + payload).
type packetWriter struct {
	ptype PacketContentType
	buf   []byte
}

func newPacketWriter(ptype PacketContentType) *packetWriter {
	return &packetWriter{ptype: ptype}
}

func (w *packetWriter) u8(v uint8)     { w.buf = append(w.buf, v) }
func (w *packetWriter) u16(v uint16)   { w.buf = binary.LittleEndian.AppendUint16(w.buf, v) }
func (w *packetWriter) u32(v uint32)   { w.buf = binary.LittleEndian.AppendUint32(w.buf, v) }
func (w *packetWriter) str(s string)   { w.buf = append(append(w.buf, s...), 0) }
func (w *packetWriter) bytes(b []byte) { w.buf = append(w.buf, b...) }

func (w *packetWriter) Bytes() []byte {
	size := 2 + 1 + len(w.buf)
	out := make([]byte, 0, size)
	out = binary.LittleEndian.AppendUint16(out, uint16(size))
	out = append(out, byte(w.ptype))
	out = append(out, w.buf...)
	return out
}

// packetReader reads one incoming packet's fields in order. The caller
// must know how many bytes remain (Remaining) to distinguish "no more
// fields" from "raw trailing data" for streaming packet types like
// PacketServerContent's file-data continuation packets.
type packetReader struct {
	Type PacketContentType
	data []byte
	pos  int
}

// readPacket reads exactly one framed packet from r.
func readPacket(r *bufio.Reader) (*packetReader, error) {
	var sizeBuf [2]byte
	if _, err := io.ReadFull(r, sizeBuf[:]); err != nil {
		return nil, err
	}
	size := binary.LittleEndian.Uint16(sizeBuf[:])
	if size < 3 {
		return nil, fmt.Errorf("invalid packet size %d", size)
	}
	rest := make([]byte, size-2)
	if _, err := io.ReadFull(r, rest); err != nil {
		return nil, err
	}
	return &packetReader{Type: PacketContentType(rest[0]), data: rest[1:]}, nil
}

func (p *packetReader) Remaining() int { return len(p.data) - p.pos }

func (p *packetReader) u8() (uint8, error) {
	if p.Remaining() < 1 {
		return 0, io.ErrUnexpectedEOF
	}
	v := p.data[p.pos]
	p.pos++
	return v, nil
}

func (p *packetReader) u32() (uint32, error) {
	if p.Remaining() < 4 {
		return 0, io.ErrUnexpectedEOF
	}
	v := binary.LittleEndian.Uint32(p.data[p.pos:])
	p.pos += 4
	return v, nil
}

func (p *packetReader) bytesN(n int) ([]byte, error) {
	if p.Remaining() < n {
		return nil, io.ErrUnexpectedEOF
	}
	v := p.data[p.pos : p.pos+n]
	p.pos += n
	return v, nil
}

func (p *packetReader) str() (string, error) {
	start := p.pos
	for p.pos < len(p.data) {
		if p.data[p.pos] == 0 {
			s := string(p.data[start:p.pos])
			p.pos++
			return s, nil
		}
		p.pos++
	}
	return "", fmt.Errorf("unterminated string in packet")
}

// remainingBytes returns every byte left in the packet, without
// interpreting it as a field -- used for PacketServerContent's raw
// file-data continuation packets.
func (p *packetReader) remainingBytes() []byte {
	b := p.data[p.pos:]
	p.pos = len(p.data)
	return b
}
