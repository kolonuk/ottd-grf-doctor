package engine

import (
	"testing"

	"github.com/kolonuk/ottd-grf-doctor/internal/sav"
)

func TestParseInGameYearXpressways(t *testing.T) {
	s, err := sav.Load("../../testdata/xpressways-2082/broken.sav")
	if err != nil {
		t.Fatal(err)
	}
	chunks, err := sav.WalkChunks(s.Payload)
	if err != nil {
		t.Fatal(err)
	}
	cm := sav.ChunkMapOf(chunks)
	year, err := ParseInGameYear(s.Payload, cm["DATE"])
	if err != nil {
		t.Fatal(err)
	}
	if year != 2082 {
		t.Errorf("got year %d, want 2082", year)
	}
}
