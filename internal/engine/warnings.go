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

// CheckEngineDateAvailability checks whether currentYear falls within
// [introYear, retireYear) for a specific engine -- exact, not a guess.
// retireYear == 0 means the engine never retires (see DefaultTrainEngine
// and, for parsed third-party engines, ParsedEngine).
func CheckEngineDateAvailability(currentYear, introYear, retireYear int) []Warning {
	var out []Warning
	if currentYear < introYear {
		out = append(out, Warning{Message: fmt.Sprintf(
			"Not introduced until %d; your save is at %d (%d year(s) early). It won't appear in the purchase list yet.",
			introYear, currentYear, introYear-currentYear)})
	} else if retireYear != 0 && currentYear >= retireYear {
		out = append(out, Warning{Message: fmt.Sprintf(
			"Retires from the purchase list in %d; your save is at %d (%d year(s) past). It may no longer be buildable.",
			retireYear, currentYear, currentYear-retireYear+1)})
	}
	return out
}

// yearRangeRe pulls a "startYear-endYear" pattern out of free text like
// "1850-2008 year range" -- the loose format BaNaNaS descriptions tend
// to use. This is a LAST-RESORT fallback only, for when a candidate's
// exact per-engine dates aren't available (e.g. GRF binary parsing
// couldn't extract them for some property-encoding this tool doesn't
// handle yet -- see ParseGRF's doc comment); CheckEngineDateAvailability
// above is exact and should always be preferred when its inputs are known.
var yearRangeRe = regexp.MustCompile(`(1[89]\d{2}|20\d{2}|21\d{2})\s*[-\x{2013}]\s*(1[89]\d{2}|20\d{2}|21\d{2})`)

// CheckDateAvailability is the fallback described above.
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
			"Exact per-engine dates weren't available, but this GRF's description mentions a year range ending %d; your save is at %d, %d year(s) later. Its newest vehicles may already be past their design lifespan. (Best-effort guess from description text -- verify in-game.)",
			endYear, currentYear, currentYear-endYear)}}
	}
	return nil
}
