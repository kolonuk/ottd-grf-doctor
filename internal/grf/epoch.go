package grf

// daysTill1920/daysInYear/isLeapYear mirror internal/engine's date math
// exactly (see internal/engine/date.go and defaults.go) -- duplicated
// here rather than imported so this package stays dependency-free and
// usable standalone; the two copies are small and unlikely to drift
// since both are directly derived from OpenTTD's own calendar constants
// (CalendarTime::DAYS_TILL_ORIGINAL_BASE_YEAR = day-count of 1 Jan 1920).
const daysInYear = 365

func isLeapYear(year int) bool {
	return year%4 == 0 && (year%100 != 0 || year%400 == 0)
}

var daysTill1920 = yearToDayCount(1920)

func yearToDayCount(year int) int {
	days := 0
	for y := 0; y < year; y++ {
		if isLeapYear(y) {
			days += daysInYear + 1
		} else {
			days += daysInYear
		}
	}
	return days
}

// DayCountToYear converts a raw day count (as stored in ParsedEngine.
// IntroDate / ParsedObject.IntroDate/EndOfLifeDate -- day 0 = 1 January
// year 0) to a calendar year, using the exact algorithm OpenTTD itself
// uses (see internal/engine/date.go's ParseInGameYear, which this
// mirrors so a save's current year and a GRF's parsed dates are directly
// comparable).
func DayCountToYear(days int32) int {
	return dayCountToYear(int(days))
}

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
