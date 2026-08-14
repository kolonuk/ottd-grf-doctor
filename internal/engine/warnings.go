package engine

import (
	"fmt"
	"regexp"
	"strconv"
)

// Warning is one non-blocking compatibility note for a proposed
// replacement engine. Nothing in this package ever turns a Warning into
// a hard error -- per this tool's design, the user always has the final
// say (see README.md).
type Warning struct {
	Message string
}

// CheckRailtypeCompatibility compares the railtype actually built under
// the broken vehicles (derived from real tile data, not guessed) against
// what's known about the candidate replacement. knownCandidateRailtype
// may be RailtypeUnknown if the candidate is a third-party GRF this tool
// has no railtype data for (its own binary format isn't parsed -- see
// README.md) -- in that case this returns an informational warning
// rather than a false claim of (in)compatibility.
func CheckRailtypeCompatibility(actualTrackRailtype, candidateRailtype Railtype) []Warning {
	var out []Warning
	switch {
	case candidateRailtype == RailtypeUnknown:
		out = append(out, Warning{
			Message: fmt.Sprintf(
				"Track here is %s, but this tool doesn't know what railtype the replacement engine needs (third-party GRF binary properties aren't parsed) -- verify manually before applying.",
				actualTrackRailtype),
		})
	case actualTrackRailtype != RailtypeUnknown && actualTrackRailtype != candidateRailtype:
		out = append(out, Warning{
			Message: fmt.Sprintf(
				"Track here is %s, but the replacement engine is %s -- these are not compatible (e.g. a Monorail engine cannot run on Electrified Rail or a Vactrain tube). The vehicle will show up but likely won't move.",
				actualTrackRailtype, candidateRailtype),
		})
	}
	return out
}

// yearRangeRe pulls a trailing 4-digit year out of free text like "1850-2008
// year range" or "Available 1970-2010" -- the loose format BaNaNaS
// descriptions tend to use. Only a best-effort signal; see
// CheckDateAvailability's doc comment for why this can't be precise.
var yearRangeRe = regexp.MustCompile(`(1[89]\d{2}|20\d{2}|21\d{2})\s*[-\x{2013}]\s*(1[89]\d{2}|20\d{2}|21\d{2})`)

// CheckDateAvailability does a best-effort check of whether a candidate
// GRF's engines are likely to still be buildable at the save's current
// in-game year, based on a "startYear-endYear" pattern in its
// description text (common in BaNaNaS listings, e.g. SHARK's "1850-2008
// year range"). This is NOT the same as reading actual per-engine
// intro/retirement properties (that needs full GRF binary parsing, out
// of scope -- see README.md), so it only ever produces a soft warning,
// never a claim of certainty.
func CheckDateAvailability(currentYear int, descriptionText string) []Warning {
	m := yearRangeRe.FindStringSubmatch(descriptionText)
	if m == nil {
		return nil
	}
	endYear, err := strconv.Atoi(m[2])
	if err != nil {
		return nil
	}
	if currentYear > endYear {
		return []Warning{{Message: fmt.Sprintf(
			"This GRF's description mentions a year range ending %d; your save is at %d, %d year(s) later. Its newest vehicles may already be past their design lifespan and could show as unavailable/expired in the purchase list. (Best-effort guess from the description text, not exact per-vehicle data -- verify in-game.)",
			endYear, currentYear, currentYear-endYear)}}
	}
	return nil
}
