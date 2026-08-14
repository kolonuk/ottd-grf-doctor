// Throwaway manual smoke test for the dynamic GRF parser against a real
// downloaded train GRF. Not part of the build; run with `go run`.
package main

import (
	"fmt"
	"os"
	"sort"

	"github.com/kolonuk/ottd-grf-doctor/internal/grf"
)

func main() {
	path := os.Args[1]
	parsed, err := grf.ParseGRF(path)
	if err != nil {
		fmt.Println("ERROR:", err)
		os.Exit(1)
	}
	fmt.Printf("Parsed %d engines, %d objects, %d warnings\n", len(parsed.Engines), len(parsed.Objects), len(parsed.Warnings))
	for _, w := range parsed.Warnings {
		fmt.Println("  warning:", w)
	}
	sort.Slice(parsed.Engines, func(i, j int) bool { return parsed.Engines[i].LocalID < parsed.Engines[j].LocalID })
	anyData := 0
	for _, e := range parsed.Engines {
		if e.HasTrackType || e.HasIntroDate || e.HasModelLife || e.HasSpeed || e.HasPower || e.HasCapacity {
			anyData++
			if anyData <= 20 {
				fmt.Printf("  #%d %q track=%d(has=%v) intro=%d(has=%v) life=%d(has=%v) speed=%d power=%d cap=%d wagon=%v\n",
					e.LocalID, e.Name, e.TrackType, e.HasTrackType, e.IntroDate, e.HasIntroDate,
					e.ModelLife, e.HasModelLife, e.Speed, e.Power, e.Capacity, e.IsWagon)
			}
		}
	}
	fmt.Printf("engines with at least one property set: %d / %d\n", anyData, len(parsed.Engines))
}
