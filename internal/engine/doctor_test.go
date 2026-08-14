package engine

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/kolonuk/ottd-grf-doctor/internal/sav"
)

// fixturePairs discovers every testdata/<name>/{broken,fixed}.sav pair so
// new regression fixtures can be added later just by dropping in a new
// directory -- this loop doesn't need to change.
func fixturePairs(t *testing.T) []string {
	t.Helper()
	root := filepath.Join("..", "..", "testdata")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("reading testdata dir: %v", err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, e.Name(), "broken.sav")); err != nil {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, e.Name(), "fixed.sav")); err != nil {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

// TestFixturesLoadAndLint just confirms every fixture pair's files are
// structurally valid savegames on their own, independent of any specific
// fix policy -- a baseline sanity check that runs for every fixture found,
// present or future.
func TestFixturesLoadAndLint(t *testing.T) {
	for _, name := range fixturePairs(t) {
		name := name
		t.Run(name, func(t *testing.T) {
			for _, which := range []string{"broken.sav", "fixed.sav"} {
				path := filepath.Join("..", "..", "testdata", name, which)
				s, err := sav.Load(path)
				if err != nil {
					t.Fatalf("%s: %v", which, err)
				}
				if _, err := sav.WalkChunks(s.Payload); err != nil {
					t.Fatalf("%s: chunk container malformed: %v", which, err)
				}
			}
		})
	}
}

// TestXpressways2082FixMatchesKnownGood is the byte-exact regression test:
// it reconstructs, using ONLY this package's public API, the exact fix
// that was hand-derived and verified (against a from-source build of the
// user's real OpenTTD 15.3 client, under gdb, with a real crash repro and
// a clean re-run after the fix) during this project's original
// development. If this test ever fails, either a real regression was
// introduced, or the fix policy genuinely needs to change -- in which
// case testdata/xpressways-2082/fixed.sav must be regenerated and the
// reason documented in the commit that changes it.
//
// Note on "byte-for-byte": this compares the DECOMPRESSED chunk payload,
// not the compressed file bytes. Two different LZMA encoders (this
// project's Go xz writer vs. the Python lzma module used to produce the
// original fixed.sav fixture) are not guaranteed to produce identical
// compressed output for identical input, even at the same preset -- that
// has nothing to do with correctness. The payload is what OpenTTD actually
// reads; that's what must match exactly.
func TestXpressways2082FixMatchesKnownGood(t *testing.T) {
	const (
		oldSH5060GRFID = "42533032"
		oldX2014GRFID  = "42533036"

		slotX2001           = 328
		slotMillenniumZ1    = 329
		slotPassenger       = 54
		slotGoods           = 55
		slotSteel           = 3
		oldSH5060SingleSlot = 24

		internalX2001        = 54
		internalMillenniumZ1 = 55
		internalPassenger    = 57
		internalGoods        = 62
		internalSteel        = 66
	)

	brokenPath := filepath.Join("..", "..", "testdata", "xpressways-2082", "broken.sav")
	fixedPath := filepath.Join("..", "..", "testdata", "xpressways-2082", "fixed.sav")

	broken, err := sav.Load(brokenPath)
	if err != nil {
		t.Fatalf("loading broken.sav: %v", err)
	}
	fixed, err := sav.Load(fixedPath)
	if err != nil {
		t.Fatalf("loading fixed.sav: %v", err)
	}

	chunks, err := sav.WalkChunks(broken.Payload)
	if err != nil {
		t.Fatalf("walking broken.sav chunks: %v", err)
	}
	cm := sav.ChunkMapOf(chunks)

	eids, err := ParseEIDS(broken.Payload, cm["EIDS"])
	if err != nil {
		t.Fatalf("parsing EIDS: %v", err)
	}
	ngrf, err := ParseNGRF(broken.Payload, cm["NGRF"])
	if err != nil {
		t.Fatalf("parsing NGRF: %v", err)
	}
	vehicles, err := ParseVEHS(broken.Payload, cm["VEHS"])
	if err != nil {
		t.Fatalf("parsing VEHS: %v", err)
	}

	an := Analyze(eids, ngrf, vehicles)

	var slot328IDs, slot329FrontIDs, slot24IDs []int
	rearByCargo := map[uint8][]int{}
	vehByID := make(map[int]TrainVehicle, len(vehicles))
	for _, v := range vehicles {
		vehByID[v.VehicleID] = v
		switch v.EngineType {
		case slotX2001:
			slot328IDs = append(slot328IDs, v.VehicleID)
		case oldSH5060SingleSlot:
			slot24IDs = append(slot24IDs, v.VehicleID)
		}
	}

	// Mirrors the original by-hand Python analysis exactly: walk every
	// IsFront() vehicle's own consist; for any chain containing a rear
	// (engine_type==329, subtype==32) car, the majority cargo among that
	// SAME chain's other (non-328/329) cars decides which default wagon
	// the rear car becomes.
	for _, front := range vehicles {
		if !front.IsFront() {
			continue
		}
		if front.EngineType == slotMillenniumZ1 {
			slot329FrontIDs = append(slot329FrontIDs, front.VehicleID)
		}
		chain := walkConsistByID(front, vehByID)
		var rears []TrainVehicle
		var others []TrainVehicle
		for _, c := range chain {
			switch {
			case c.EngineType == slotMillenniumZ1 && c.Subtype == 32:
				rears = append(rears, c)
			case c.EngineType != slotX2001 && c.EngineType != slotMillenniumZ1:
				others = append(others, c)
			}
		}
		if len(rears) == 0 {
			continue
		}
		majority := majorityCargo(others)
		for _, r := range rears {
			rearByCargo[majority] = append(rearByCargo[majority], r.VehicleID)
		}
	}
	slot329FrontIDs = append(slot329FrontIDs, slot24IDs...)

	x2001 := TargetEngine{GRFID: InvalidGRFID, InternalID: internalX2001, Name: "X2001"}
	millenniumZ1 := TargetEngine{GRFID: InvalidGRFID, InternalID: internalMillenniumZ1, Name: "Millennium Z1"}
	passenger := TargetEngine{GRFID: InvalidGRFID, InternalID: internalPassenger, Name: "Passenger Carriage"}
	goods := TargetEngine{GRFID: InvalidGRFID, InternalID: internalGoods, Name: "Goods Van"}
	steel := TargetEngine{GRFID: InvalidGRFID, InternalID: internalSteel, Name: "Steel Truck"}

	// cargo codes: 0 = PASS, 5 = GOOD, 9 = STEL (see original by-hand
	// analysis; these are the CargoType byte values OpenTTD stores).
	plan := &Plan{Assignments: []Assignment{
		NewPinnedAssignment(slot328IDs, x2001, slotX2001),
		NewPinnedAssignment(slot329FrontIDs, millenniumZ1, slotMillenniumZ1),
		NewPinnedAssignment(rearByCargo[0], passenger, slotPassenger),
		NewPinnedAssignment(rearByCargo[5], goods, slotGoods),
		NewPinnedAssignment(rearByCargo[9], steel, slotSteel),
	}}

	if got, want := len(rearByCargo[0]), 12; got != want {
		t.Fatalf("PASS-majority rear carriages: got %d, want %d", got, want)
	}
	if got, want := len(rearByCargo[5]), 8; got != want {
		t.Fatalf("GOOD-majority rear carriages: got %d, want %d", got, want)
	}
	if got, want := len(rearByCargo[9]), 2; got != want {
		t.Fatalf("STEL-majority rear carriages: got %d, want %d", got, want)
	}
	if got, want := len(slot328IDs), 94; got != want {
		t.Fatalf("slot 328 (X2001) vehicles: got %d, want %d", got, want)
	}
	if got, want := len(slot329FrontIDs), 26; got != want { // 22 front-pair + 4 redirected from slot 24
		t.Fatalf("slot 329 (Millennium Z1) vehicles: got %d, want %d", got, want)
	}

	res, err := Apply(an, plan)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// v5's actual fix also dropped these two now-unused NewGRFs from the
	// list (nothing references them anymore once the fix is applied).
	unusedGRFs := []string{"4B523033", "42531320"}

	newPayload, err := ApplyToPayload(broken.Payload, eids, ngrf, vehicles, res, nil, unusedGRFs)
	if err != nil {
		t.Fatalf("ApplyToPayload: %v", err)
	}

	// The fix must not have introduced any duplicate EIDS key -- this is
	// the exact invariant whose violation caused a real crash during this
	// project's development.
	newChunks, err := sav.WalkChunks(newPayload)
	if err != nil {
		t.Fatalf("walking produced payload: %v", err)
	}
	newEIDS, err := ParseEIDS(newPayload, sav.ChunkMapOf(newChunks)["EIDS"])
	if err != nil {
		t.Fatalf("parsing produced EIDS: %v", err)
	}
	if err := ValidateUniqueKeys(newEIDS); err != nil {
		t.Fatalf("produced save has a duplicate-key bug: %v", err)
	}

	_ = oldSH5060GRFID
	_ = oldX2014GRFID

	if !bytes.Equal(newPayload, fixed.Payload) {
		t.Errorf("produced payload does not byte-for-byte match testdata/xpressways-2082/fixed.sav\n"+
			"produced: %d bytes, expected: %d bytes", len(newPayload), len(fixed.Payload))
		reportFirstDiff(t, newPayload, fixed.Payload)
	}
}

// walkConsistByID is the test-local, value-typed equivalent of the
// package's walkConsist (which operates on *TrainVehicle over a
// VehicleID->*TrainVehicle map built from a single immutable slice).
// Kept separate since the test builds its map as value-typed for
// simplicity.
func walkConsistByID(front TrainVehicle, byID map[int]TrainVehicle) []TrainVehicle {
	var chain []TrainVehicle
	seen := map[int]bool{}
	cur := front
	for {
		if seen[cur.VehicleID] {
			break
		}
		seen[cur.VehicleID] = true
		chain = append(chain, cur)
		if cur.NextVehicleID < 0 {
			break
		}
		nxt, ok := byID[int(cur.NextVehicleID)]
		if !ok {
			break
		}
		cur = nxt
	}
	return chain
}

func majorityCargo(vehicles []TrainVehicle) uint8 {
	counts := map[uint8]int{}
	for _, v := range vehicles {
		counts[v.CargoType]++
	}
	var best uint8
	bestCount := -1
	// Deterministic tie-break: lowest cargo type value wins, matching
	// Python's Counter.most_common() insertion-order behaviour closely
	// enough for this fixture (no ties occur in practice here).
	for ct := uint8(0); ct < 255; ct++ {
		if c, ok := counts[ct]; ok && c > bestCount {
			best, bestCount = ct, c
		}
	}
	return best
}

func reportFirstDiff(t *testing.T, a, b []byte) {
	t.Helper()
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			lo := i - 8
			if lo < 0 {
				lo = 0
			}
			hi := i + 8
			if hi > n {
				hi = n
			}
			t.Logf("first diff at payload offset %d: produced=%x expected=%x", i, a[lo:hi], b[lo:hi])
			return
		}
	}
}
