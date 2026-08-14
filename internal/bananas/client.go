package bananas

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultServer is OpenTTD's own content server address (see
// NetworkContentServerConnectionString in src/network/core/config.cpp).
const DefaultServer = "content.openttd.org:3978"

// ContentInfo is one entry from the live BaNaNaS catalog, as returned by
// the content server itself -- this is the authoritative, always-current
// source (as opposed to the static metadata git clone, which is a slower-
// moving mirror useful for browsing descriptions offline).
type ContentInfo struct {
	ContentID uint32
	Filesize  uint32
	Name      string
	Version   string
	URL       string
	Desc      string
	UniqueID  uint32 // this is the GRFID for NewGRF content
	// MD5 is the content SERVER's package-integrity hash (verified against
	// a real download: it does NOT match the extracted .grf file's own
	// MD5 -- it's over the uploaded package as a whole, not any single
	// file inside it). Don't use this for a savegame NGRF record; compute
	// that from the extracted .grf file directly (see
	// internal/engine.MD5File), which is what the game itself checks.
	MD5  [16]byte
	Tags []string
}

// GRFIDHex returns UniqueID formatted the way this project displays
// grfids elsewhere (8 uppercase hex chars, memory order).
func (c ContentInfo) GRFIDHex() string {
	return fmt.Sprintf("%08X", c.UniqueID)
}

// Client talks to an OpenTTD content server over its native TCP protocol.
type Client struct {
	Server string // host:port, defaults to DefaultServer
}

func (c *Client) server() string {
	if c.Server == "" {
		return DefaultServer
	}
	return c.Server
}

// ListNewGRFs fetches the full current NewGRF catalog. This can be a few
// thousand entries and take several seconds; idleTimeout bounds how long
// to wait after the last received packet before considering the list
// complete (the protocol has no explicit "end of list" marker -- the
// real client relies on the same kind of idle/connection-close signal).
func (c *Client) ListNewGRFs(ctx context.Context, idleTimeout time.Duration) ([]ContentInfo, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", c.server())
	if err != nil {
		return nil, fmt.Errorf("connecting to %s: %w", c.server(), err)
	}
	defer conn.Close()

	w := newPacketWriter(PacketClientInfoList)
	w.u8(ContentTypeNewGRF)
	w.u32(0xffffffff)
	w.u8(1)
	w.str("vanilla")
	w.str(currentOpenTTDVersion)
	if _, err := conn.Write(w.Bytes()); err != nil {
		return nil, fmt.Errorf("sending list request: %w", err)
	}

	r := bufio.NewReader(conn)
	var out []ContentInfo
	for {
		conn.SetReadDeadline(time.Now().Add(idleTimeout))
		pkt, err := readPacket(r)
		if err != nil {
			if isTimeoutOrClosed(err) {
				break // no more data -- list is complete
			}
			return out, fmt.Errorf("reading list response: %w", err)
		}
		if pkt.Type != PacketServerInfo {
			continue
		}
		info, err := decodeServerInfo(pkt)
		if err != nil {
			return out, fmt.Errorf("decoding SERVER_INFO: %w", err)
		}
		if info.Filesize == 0 {
			continue // "does not exist" marker, per the real client
		}
		out = append(out, *info)
	}
	return out, nil
}

func decodeServerInfo(pkt *packetReader) (*ContentInfo, error) {
	if _, err := pkt.u8(); err != nil { // ContentType, unused here
		return nil, err
	}
	id, err := pkt.u32()
	if err != nil {
		return nil, err
	}
	filesize, err := pkt.u32()
	if err != nil {
		return nil, err
	}
	name, err := pkt.str()
	if err != nil {
		return nil, err
	}
	version, err := pkt.str()
	if err != nil {
		return nil, err
	}
	url, err := pkt.str()
	if err != nil {
		return nil, err
	}
	desc, err := pkt.str()
	if err != nil {
		return nil, err
	}
	uniqueID, err := pkt.u32()
	if err != nil {
		return nil, err
	}
	md5b, err := pkt.bytesN(16)
	if err != nil {
		return nil, err
	}
	depCount, err := pkt.u8()
	if err != nil {
		return nil, err
	}
	for i := 0; i < int(depCount); i++ {
		if _, err := pkt.u32(); err != nil {
			return nil, err
		}
	}
	tagCount, err := pkt.u8()
	if err != nil {
		return nil, err
	}
	tags := make([]string, 0, tagCount)
	for i := 0; i < int(tagCount); i++ {
		t, err := pkt.str()
		if err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}

	info := &ContentInfo{
		ContentID: id, Filesize: filesize, Name: name, Version: version,
		URL: url, Desc: desc, UniqueID: uniqueID, Tags: tags,
	}
	copy(info.MD5[:], md5b)
	return info, nil
}

// Download fetches one content item by ID, gunzips and untars it (the
// server always sends gzip-compressed tar data -- see AfterDownload in
// the real client), and writes the extracted files under destDir.
// Returns the paths of every file extracted.
func (c *Client) Download(ctx context.Context, contentID uint32, destDir string) ([]string, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", c.server())
	if err != nil {
		return nil, fmt.Errorf("connecting to %s: %w", c.server(), err)
	}
	defer conn.Close()

	w := newPacketWriter(PacketClientContent)
	w.u16(1)
	w.u32(contentID)
	if _, err := conn.Write(w.Bytes()); err != nil {
		return nil, fmt.Errorf("sending content request: %w", err)
	}

	r := bufio.NewReader(conn)
	pr, pw := io.Pipe()
	done := make(chan error, 1)
	go func() {
		defer pw.Close()
		var haveHeader bool
		for {
			if deadline, ok := ctx.Deadline(); ok {
				conn.SetReadDeadline(deadline)
			}
			pkt, err := readPacket(r)
			if err != nil {
				done <- fmt.Errorf("reading content stream: %w", err)
				return
			}
			if pkt.Type != PacketServerContent {
				continue
			}
			if !haveHeader {
				if _, err := pkt.u8(); err != nil { // ContentType
					done <- err
					return
				}
				if _, err := pkt.u32(); err != nil { // ContentID (echoed)
					done <- err
					return
				}
				if _, err := pkt.u32(); err != nil { // filesize
					done <- err
					return
				}
				if _, err := pkt.str(); err != nil { // filename
					done <- err
					return
				}
				haveHeader = true
				continue
			}
			data := pkt.remainingBytes()
			if len(data) == 0 {
				done <- nil // EOF marker, per the real client
				return
			}
			if _, err := pw.Write(data); err != nil {
				done <- err
				return
			}
		}
	}()

	extracted, extractErr := gunzipAndUntar(pr, destDir)
	streamErr := <-done
	if streamErr != nil {
		return nil, streamErr
	}
	if extractErr != nil {
		return nil, extractErr
	}
	return extracted, nil
}

func gunzipAndUntar(r io.Reader, destDir string) ([]string, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("opening gzip stream: %w", err)
	}
	defer gz.Close()

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, err
	}

	tr := tar.NewReader(gz)
	var extracted []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return extracted, fmt.Errorf("reading tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		// Guard against path traversal from a malicious/corrupt archive.
		cleanName := filepath.Clean(hdr.Name)
		if filepath.IsAbs(cleanName) || cleanName == ".." || len(cleanName) >= 2 && cleanName[:2] == ".." {
			return extracted, fmt.Errorf("tar entry has unsafe path: %q", hdr.Name)
		}
		outPath := filepath.Join(destDir, cleanName)
		if !isWithinDir(destDir, outPath) {
			return extracted, fmt.Errorf("tar entry escapes destination dir: %q", hdr.Name)
		}
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return extracted, err
		}
		f, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			return extracted, err
		}
		if _, err := io.Copy(f, tr); err != nil {
			f.Close()
			return extracted, err
		}
		f.Close()
		extracted = append(extracted, outPath)
	}
	return extracted, nil
}

func isWithinDir(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func isTimeoutOrClosed(err error) bool {
	var ne net.Error
	if e, ok := err.(net.Error); ok {
		ne = e
		return ne.Timeout()
	}
	return err == io.EOF
}

// currentOpenTTDVersion is sent to the content server as this client's
// version string. The content server uses it only to filter results by
// compatibility branch ("vanilla"); it does not need to be exact, but
// should track roughly-current OpenTTD releases.
const currentOpenTTDVersion = "15.3"
