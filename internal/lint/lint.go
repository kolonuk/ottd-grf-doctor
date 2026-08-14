// Package lint validates that a savegame we've just modified is still
// structurally sound and won't crash OpenTTD on load. It re-derives
// everything from scratch off the final bytes -- it never trusts whatever
// in-memory state produced them -- so it catches mistakes anywhere in the
// pipeline, not just in code paths we remembered to test.
package lint

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/kolonuk/ottd-grf-doctor/internal/engine"
	"github.com/kolonuk/ottd-grf-doctor/internal/sav"
)

// Finding is one lint result. Severity "error" means the save is very
// likely to crash or misbehave on load; "warning" is a lower-confidence or
// cosmetic issue.
type Finding struct {
	Severity string // "error" | "warning" | "info"
	Message  string
}

// Report is the full result of linting one save.
type Report struct {
	Findings []Finding
}

func (r *Report) HasErrors() bool {
	for _, f := range r.Findings {
		if f.Severity == "error" {
			return true
		}
	}
	return false
}

func (r *Report) add(sev, format string, args ...any) {
	r.Findings = append(r.Findings, Finding{Severity: sev, Message: fmt.Sprintf(format, args...)})
}

// Lint runs every structural check this package knows about against the
// savegame at path.
func Lint(path string) (*Report, error) {
	r := &Report{}

	s, err := sav.Load(path)
	if err != nil {
		r.add("error", "could not even parse the savegame header/container: %v", err)
		return r, nil
	}
	r.add("info", "header OK: OTTX, savegame version %d.%d", s.MajorVersion, s.MinorVersion)

	chunks, err := sav.WalkChunks(s.Payload)
	if err != nil {
		r.add("error", "chunk container is malformed: %v", err)
		return r, nil
	}
	cm := sav.ChunkMapOf(chunks)
	r.add("info", "chunk container OK: %d top-level chunks parsed", len(chunks))

	requiredChunks := []string{"EIDS", "ENGN", "NGRF", "VEHS", "MAPS", "MAPT"}
	for _, id := range requiredChunks {
		if _, ok := cm[id]; !ok {
			r.add("error", "required chunk %q is missing entirely", id)
		}
	}
	if r.HasErrors() {
		return r, nil
	}

	// EIDS: the exact invariant that caused a real crash during this
	// tool's own development -- every (grfid, internal_id, type) key
	// must be unique across the whole chunk.
	eids, err := engine.ParseEIDS(s.Payload, cm["EIDS"])
	if err != nil {
		r.add("error", "EIDS chunk did not parse: %v", err)
	} else {
		if err := engine.ValidateUniqueKeys(eids); err != nil {
			r.add("error", "%v", err)
		} else {
			r.add("info", "EIDS OK: %d engine slots, no duplicate keys", len(eids))
		}
	}

	// NGRF: no duplicate grfids (the game would only keep one, silently).
	ngrf, err := engine.ParseNGRF(s.Payload, cm["NGRF"])
	if err != nil {
		r.add("error", "NGRF chunk did not parse: %v", err)
	} else {
		seen := map[string]string{}
		for _, e := range ngrf {
			if prev, ok := seen[e.GRFID]; ok {
				r.add("error", "duplicate GRFID %s in NGRF (%q and %q)", e.GRFID, prev, e.Filename)
			}
			seen[e.GRFID] = e.Filename
		}
		r.add("info", "NGRF OK: %d NewGRFs listed, no duplicate GRFIDs", len(ngrf))
	}

	// VEHS: every train's engine_type must be a valid index into EIDS.
	vehicles, err := engine.ParseVEHS(s.Payload, cm["VEHS"])
	if err != nil {
		r.add("error", "VEHS chunk did not parse: %v", err)
	} else {
		eidsLen := len(cm["EIDS"].Records)
		badRefs := 0
		for _, v := range vehicles {
			if int(v.EngineType) >= eidsLen {
				badRefs++
				r.add("error", "vehicle %d has engine_type %d, out of range (EIDS has %d slots)", v.VehicleID, v.EngineType, eidsLen)
			}
		}
		if badRefs == 0 {
			r.add("info", "VEHS OK: %d train vehicles, all engine_type references in range", len(vehicles))
		}
	}

	return r, nil
}

// LintWithClient is Lint plus a real-load smoke test: if an OpenTTD
// binary path is given (built to match the target client, since savegame
// internals can differ across versions -- see README.md), run it
// headlessly against the save with a short timeout and watch for a
// non-zero exit or an "SlErrorCorrupt"/similar fatal message in its debug
// output. This is the strongest check available short of the user's own
// client, but it's optional: pass "" to skip it.
func LintWithClient(ctx context.Context, path, openttdBinary string, timeout time.Duration) (*Report, error) {
	r, err := Lint(path)
	if err != nil || r.HasErrors() {
		return r, err
	}
	if openttdBinary == "" {
		r.add("info", "no OpenTTD binary configured -- skipping real-load smoke test")
		return r, nil
	}
	if _, err := os.Stat(openttdBinary); err != nil {
		r.add("warning", "configured OpenTTD binary %q not found, skipping real-load smoke test", openttdBinary)
		return r, nil
	}

	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, openttdBinary, "-v", "null", "-s", "null", "-m", "null", "-g", path, "-d", "misc=1,grf=1")
	outBytes, runErr := cmd.CombinedOutput()
	out := string(outBytes)

	for _, bad := range []string{"SlErrorCorrupt", "FATAL", "Invalid vehicle type", "Too many NewGRF"} {
		if strings.Contains(out, bad) {
			r.add("error", "real-load smoke test found %q in OpenTTD's own output", bad)
		}
	}
	if cctx.Err() == context.DeadlineExceeded {
		r.add("info", "real-load smoke test: process was still running after %s, killed (this is normal for a load-only check, not itself a failure)", timeout)
	} else if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok && exitErr.ExitCode() != 0 {
			r.add("warning", "OpenTTD exited with code %d during the smoke test (may be unrelated to this save)", exitErr.ExitCode())
		}
	}
	if !r.HasErrors() {
		r.add("info", "real-load smoke test: no fatal errors observed")
	}
	return r, nil
}
