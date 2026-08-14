package engine

import (
	"testing"

	"github.com/kolonuk/ottd-grf-doctor/internal/sav"
)

func loadFixedFixture(t *testing.T) (*sav.Save, sav.ChunkMap) {
	t.Helper()
	s, err := sav.Load("../../testdata/xpressways-2082/fixed.sav")
	if err != nil {
		t.Fatal(err)
	}
	chunks, err := sav.WalkChunks(s.Payload)
	if err != nil {
		t.Fatal(err)
	}
	return s, sav.ChunkMapOf(chunks)
}

// TestParseOBIDRealSave verifies the OBID parser against real savegame
// data: a dense 64000-record array (src/object_type.h's NUM_OBJECTS), with
// slots 0..4 (NEW_OBJECT_OFFSET) reserved/zero for built-in objects and a
// handful of real GRF-sourced entries beyond that -- confirmed by hand
// during investigation (grfid 10994D4D on-disk == 4D4D9910 display order,
// "the_lighthouse_set", starting at slot 5).
func TestParseOBIDRealSave(t *testing.T) {
	s, cm := loadFixedFixture(t)
	obid, err := ParseOBID(s.Payload, cm["OBID"])
	if err != nil {
		t.Fatalf("ParseOBID: %v", err)
	}
	if len(obid) != 64000 {
		t.Fatalf("expected 64000 OBID records (NUM_OBJECTS), got %d", len(obid))
	}
	for i := 0; i < 5; i++ {
		if obid[i].GRFID != "00000000" || obid[i].EntityID != 0 {
			t.Errorf("slot %d: expected reserved/zero built-in object slot, got grfid=%s entity=%d", i, obid[i].GRFID, obid[i].EntityID)
		}
	}
	if got := obid[5].GRFID; got != "4D4D9910" {
		t.Errorf("slot 5: expected the_lighthouse_set grfid 4D4D9910, got %s", got)
	}
	if obid[5].EntityID != 0 {
		t.Errorf("slot 5: expected entity_id 0, got %d", obid[5].EntityID)
	}

	nonzero := 0
	for _, e := range obid {
		if e.GRFID != "00000000" || e.EntityID != 0 {
			nonzero++
		}
	}
	if nonzero == 0 {
		t.Fatal("expected at least some populated OBID entries")
	}
	t.Logf("%d/%d OBID slots populated", nonzero, len(obid))
}

// TestParseOBJSRealSave verifies the OBJS parser against real savegame
// data, including that a 0-length placeholder record (a destroyed object
// leaving a hole in this non-sparse Array chunk -- found at record 42
// during development) is skipped rather than erroring.
func TestParseOBJSRealSave(t *testing.T) {
	s, cm := loadFixedFixture(t)
	objChunk := cm["OBJS"]
	if len(objChunk.Records) != 168 {
		t.Fatalf("expected 168 raw OBJS records, got %d", len(objChunk.Records))
	}

	wantLive := 0
	for _, r := range objChunk.Records {
		if r.Length != 0 {
			wantLive++
		}
	}

	instances, err := ParseOBJS(s.Payload, objChunk)
	if err != nil {
		t.Fatalf("ParseOBJS: %v", err)
	}
	if len(instances) != wantLive {
		t.Fatalf("expected %d live object instances (168 records minus deleted placeholders), got %d", wantLive, len(instances))
	}
	if len(instances) == len(objChunk.Records) {
		t.Fatal("expected at least one deleted-object placeholder record to have been skipped (found none) -- fixture assumption may be stale")
	}
	for _, o := range instances {
		if o.Width == 0 || o.Height == 0 {
			t.Errorf("instance %d: expected nonzero width/height, got %dx%d", o.Index, o.Width, o.Height)
		}
		if o.BuildDate == 0 {
			t.Errorf("instance %d: expected nonzero build_date", o.Index)
		}
	}
}

// TestAnalyzeObjectsRealSave confirms the object side of Analyze finds no
// missing object GRFs in this fixture -- every OBID grfid it references
// really is loaded (verified by hand during investigation: the only
// missing GRFs in this save were the two train sets, not any object set).
func TestAnalyzeObjectsRealSave(t *testing.T) {
	s, cm := loadFixedFixture(t)
	eids, err := ParseEIDS(s.Payload, cm["EIDS"])
	if err != nil {
		t.Fatal(err)
	}
	ngrf, err := ParseNGRF(s.Payload, cm["NGRF"])
	if err != nil {
		t.Fatal(err)
	}
	vehicles, err := ParseVEHS(s.Payload, cm["VEHS"])
	if err != nil {
		t.Fatal(err)
	}
	obid, err := ParseOBID(s.Payload, cm["OBID"])
	if err != nil {
		t.Fatal(err)
	}
	objs, err := ParseOBJS(s.Payload, cm["OBJS"])
	if err != nil {
		t.Fatal(err)
	}

	an := Analyze(eids, ngrf, vehicles, obid, objs)
	if len(an.MissingObjects) != 0 {
		t.Errorf("expected no missing object GRFs in this fixture, got %d: %+v", len(an.MissingObjects), an.MissingObjects)
	}
}

// TestApplyObjectSwaps exercises the object-repoint path end to end: patch
// a real (currently-valid) OBID slot to a synthetic replacement and
// confirm the byte-level round trip, then confirm ValidateUniqueObjectKeys
// actually rejects a swap that collides with an existing entry -- the
// object-chunk analogue of the exact bug ValidateUniqueKeys exists to
// catch for vehicles (see that function's doc comment).
func TestApplyObjectSwaps(t *testing.T) {
	s, cm := loadFixedFixture(t)
	obidBefore, err := ParseOBID(s.Payload, cm["OBID"])
	if err != nil {
		t.Fatal(err)
	}

	// Slot 5 is the_lighthouse_set's first entity; repoint it to an
	// obviously-synthetic (grfid, entity_id) that nothing else uses.
	out, err := ApplyObjectSwaps(s.Payload, []ObjectAssignment{
		{Slots: []int{5}, TargetGRFID: "DEADBEEF", TargetEntity: 42},
	})
	if err != nil {
		t.Fatalf("ApplyObjectSwaps: %v", err)
	}

	chunks, err := sav.WalkChunks(out)
	if err != nil {
		t.Fatal(err)
	}
	obidAfter, err := ParseOBID(out, sav.ChunkMapOf(chunks)["OBID"])
	if err != nil {
		t.Fatal(err)
	}
	if obidAfter[5].GRFID != "DEADBEEF" || obidAfter[5].EntityID != 42 {
		t.Fatalf("slot 5 after swap: got grfid=%s entity=%d, want DEADBEEF/42", obidAfter[5].GRFID, obidAfter[5].EntityID)
	}
	for i := range obidBefore {
		if i == 5 {
			continue
		}
		if obidBefore[i] != obidAfter[i] {
			t.Fatalf("slot %d changed unexpectedly by a swap targeting only slot 5", i)
		}
	}

	// Now force a collision: slot 6 already exists with a real
	// (grfid, entity_id); repointing slot 5 to the exact same pair must
	// be rejected before the save is written.
	collideGRFID := obidBefore[6].GRFID
	collideEntity := obidBefore[6].EntityID
	_, err = ApplyObjectSwaps(s.Payload, []ObjectAssignment{
		{Slots: []int{5}, TargetGRFID: collideGRFID, TargetEntity: collideEntity},
	})
	if err == nil {
		t.Fatal("expected ApplyObjectSwaps to reject a swap colliding with an existing (grfid, entity_id) pair")
	}
}
