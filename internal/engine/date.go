package engine

import (
	"fmt"

	"github.com/kolonuk/ottd-grf-doctor/internal/sav"
)

// daysInYear/inLeapYear/epoch constants match OpenTTD's own calendar math
// (src/timer/timer_game_common.cpp CalendarConvertDateToYMD) -- day 0 is
// 1 January, year 0, in a proleptic Gregorian calendar.
const daysInYear = 365

func isLeapYear(year int) bool {
	return year%4 == 0 && (year%100 != 0 || year%400 == 0)
}

// ParseInGameYear reads the save's current in-game (calendar) year from
// the DATE chunk. At this tool's target savegame version (194), DATE is a
// RIFF chunk whose first field is `date` (i32 BE, a day count from the
// epoch above) -- see misc_sl.cpp's _date_desc for the full (version-
// conditional) field list; only the first field is needed here.
func ParseInGameYear(payload []byte, chunk *sav.Chunk) (int, error) {
	if chunk == nil {
		return 0, fmt.Errorf("DATE chunk not found")
	}
	if chunk.RiffLength < 4 {
		return 0, fmt.Errorf("DATE chunk too short (%d bytes)", chunk.RiffLength)
	}
	b := payload[chunk.RiffOffset : chunk.RiffOffset+4]
	days := int32(b[0])<<24 | int32(b[1])<<16 | int32(b[2])<<8 | int32(b[3])
	return dayCountToYear(int(days)), nil
}

// dayCountToYear ports the year-determination portion of
// CalendarConvertDateToYMD (the month/day breakdown is dropped -- this
// tool only ever needs the year, for date-availability warnings).
func dayCountToYear(days int) int {
	year := 400 * (days / (daysInYear*400 + 97))
	rem := days % (daysInYear*400 + 97)

	if rem >= daysInYear*100+25 {
		year += 100
		rem -= daysInYear*100 + 25
		year += 100 * (rem / (daysInYear*100 + 24))
		rem = rem % (daysInYear*100 + 24)
	}

	if !isLeapYear(year) && rem >= daysInYear*4 {
		year += 4
		rem -= daysInYear * 4
	}

	year += 4 * (rem / (daysInYear*4 + 1))
	rem = rem % (daysInYear*4 + 1)

	for {
		yearLen := daysInYear
		if isLeapYear(year) {
			yearLen = daysInYear + 1
		}
		if rem < yearLen {
			break
		}
		rem -= yearLen
		year++
	}
	return year
}
