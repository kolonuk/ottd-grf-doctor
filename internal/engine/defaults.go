package engine

// DefaultTrainEngine describes one base-game (non-GRF) train engine, for
// display and compatibility-warning purposes when a fix target is a
// default engine rather than a third-party NewGRF's.
//
// All three fields are decoded directly from OpenTTD's own tables (see
// src/table/engines.h's RVI/MT/MM/MW macros and src/engine_type.h's
// EngineInfo struct), not guessed:
//   - Railtype: the RVI macro's `j` parameter.
//   - IntroYear: EngineInfo.base_intro, which the MT/MM/MW macros compute
//     as CalendarTime::DAYS_TILL_ORIGINAL_BASE_YEAR + a (a = the macro's
//     first argument) -- DAYS_TILL_ORIGINAL_BASE_YEAR is the day-count of
//     1 January 1920 (ORIGINAL_BASE_YEAR), converted to a year here via
//     the same calendar math as ParseInGameYear.
//   - RetireYear: IntroYear + base_life (EngineInfo.base_life, the MT/MM/MW
//     macro's `d` argument, in years -- how long the engine stays
//     purchasable; 0xFF/255 means it never retires and is represented
//     here as RetireYear == 0, meaning "no retirement").
type DefaultTrainEngine struct {
	InternalID uint16
	Name       string
	Railtype   Railtype
	IntroYear  int
	RetireYear int // 0 means "never retires" (EngineInfo.base_life == 0xFF)
}

// Railtype is this tool's own small enum for the base-game railtypes,
// matching the labels stored in a savegame's RAIL chunk (see
// ParseRailtypeLabels) -- RAIL/ELRL/MONO/MGLV. Third-party NewGRFs can
// define arbitrary additional railtypes (e.g. Vactrain's "VACT") that
// this enum has no slot for; those compare as RailtypeOther.
type Railtype uint8

const (
	RailtypeUnknown  Railtype = iota
	RailtypeRail              // "RAIL" -- plain, non-electrified rail
	RailtypeElectric          // "ELRL" -- electrified rail
	RailtypeMono              // "MONO" -- monorail
	RailtypeMaglev            // "MGLV" -- maglev
	RailtypeOther             // any other label (custom NewGRF railtype, e.g. Vactrain's "VACT")
)

func (r Railtype) String() string {
	switch r {
	case RailtypeRail:
		return "Rail"
	case RailtypeElectric:
		return "Electrified Rail"
	case RailtypeMono:
		return "Monorail"
	case RailtypeMaglev:
		return "Maglev"
	case RailtypeOther:
		return "Other/custom"
	default:
		return "Unknown"
	}
}

// RailtypeFromLabel maps a RAIL-chunk label (4 raw bytes, e.g. "MONO")
// to this tool's Railtype enum.
func RailtypeFromLabel(label string) Railtype {
	switch label {
	case "RAIL":
		return RailtypeRail
	case "ELRL":
		return RailtypeElectric
	case "MONO":
		return RailtypeMono
	case "MGLV":
		return RailtypeMaglev
	case "":
		return RailtypeUnknown
	default:
		return RailtypeOther
	}
}

// daysTill1920 is CalendarTime::DAYS_TILL_ORIGINAL_BASE_YEAR: the day
// count (in this package's epoch, day 0 = 1 January year 0) of 1 January
// 1920, computed the same way OpenTTD computes DateAtStartOfYear -- by
// summing each preceding year's length. This only ever runs once (a
// package-level var initializer over 1920 iterations), so the simple
// loop is fine; it doesn't need OpenTTD's fast bulk-arithmetic version.
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

// defaultEngineRow mirrors one MT/MW macro call's arguments: introDays is
// the macro's first argument (added to daysTill1920 to get base_intro),
// decaySpeed/lifelength are unused by this tool but kept for fidelity to
// the source, baseLifeYears is the macro's fourth argument
// (EngineInfo.base_life, in years; 255 means never retires).
type defaultEngineRow struct {
	id            uint16
	name          string
	railtype      Railtype
	introDays     int
	baseLifeYears int
}

// DefaultTrainEngines is every base-game train engine this tool has data
// for: the "Rail" (0-26 locos, 27-53 wagons), "Monorail" (54-83), and
// "Maglev" (84-88) sections of the temperate default engine table.
// Road/ship/aircraft/toyland/arctic/tropic-specific entries are not
// included -- extend this table if warnings for those become useful.
var DefaultTrainEngines = buildDefaultTrainEngines()

func buildDefaultTrainEngines() map[uint16]DefaultTrainEngine {
	rows := []defaultEngineRow{
		{0, "Kirby Paul Tank (Steam)", RailtypeRail, 1827, 30},
		{1, "MJS 250 (Diesel)", RailtypeRail, 12784, 30},
		{2, "Ploddyphut Choo-Choo", RailtypeRail, 9497, 50},
		{3, "Powernaut Choo-Choo", RailtypeRail, 11688, 30},
		{4, "Mightymover Choo-Choo", RailtypeRail, 16802, 30},
		{5, "Ploddyphut Diesel", RailtypeRail, 18993, 30},
		{6, "Powernaut Diesel", RailtypeRail, 20820, 30},
		{7, "Wills 2-8-0 (Steam)", RailtypeRail, 8766, 30},
		{8, "Chaney 'Jubilee' (Steam)", RailtypeRail, 5114, 30},
		{9, "Ginzu 'A4' (Steam)", RailtypeRail, 5479, 30},
		{10, "SH '8P' (Steam)", RailtypeRail, 12419, 25},
		{11, "Manley-Morel DMU (Diesel)", RailtypeRail, 13149, 30},
		{12, "'Dash' (Diesel)", RailtypeRail, 23376, 35},
		{13, "SH/Hendry '25' (Diesel)", RailtypeRail, 14976, 28},
		{14, "UU '37' (Diesel)", RailtypeRail, 14245, 30},
		{15, "Floss '47' (Diesel)", RailtypeRail, 15341, 33},
		{16, "CS 4000 (Diesel)", RailtypeRail, 14976, 25},
		{17, "CS 2400 (Diesel)", RailtypeRail, 16437, 30},
		{18, "Centennial (Diesel)", RailtypeRail, 18993, 30},
		{19, "Kelling 3100 (Diesel)", RailtypeRail, 13880, 30},
		{20, "Turner Turbo (Diesel)", RailtypeRail, 20454, 30},
		{21, "MJS 1000 (Diesel)", RailtypeRail, 16071, 30},
		{22, "SH '125' (Diesel)", RailtypeRail, 20820, 25},
		{23, "SH '30' (Electric)", RailtypeElectric, 16437, 30},
		{24, "SH '40' (Electric)", RailtypeElectric, 19359, 80},
		{25, "'T.I.M.' (Electric)", RailtypeElectric, 23376, 30},
		{26, "'AsiaStar' (Electric)", RailtypeElectric, 26298, 50},
		{27, "Passenger Carriage", RailtypeRail, 1827, 50},
		{28, "Mail Van", RailtypeRail, 1827, 50},
		{29, "Coal Truck", RailtypeRail, 1827, 50},
		{30, "Oil Tanker", RailtypeRail, 1827, 50},
		{31, "Livestock Van", RailtypeRail, 1827, 50},
		{32, "Goods Van", RailtypeRail, 1827, 50},
		{33, "Grain Hopper", RailtypeRail, 1827, 50},
		{34, "Wood Truck", RailtypeRail, 1827, 50},
		{35, "Iron Ore Hopper", RailtypeRail, 1827, 50},
		{36, "Steel Truck", RailtypeRail, 1827, 50},
		{37, "Armoured Van", RailtypeRail, 1827, 50},
		{38, "Food Van", RailtypeRail, 1827, 50},
		{39, "Paper Truck", RailtypeRail, 1827, 50},
		{40, "Copper Ore Hopper", RailtypeRail, 1827, 50},
		{41, "Water Tanker", RailtypeRail, 1827, 50},
		{42, "Fruit Truck", RailtypeRail, 1827, 50},
		{43, "Rubber Truck", RailtypeRail, 1827, 50},
		{44, "Sugar Truck", RailtypeRail, 1827, 50},
		{45, "Candyfloss Hopper", RailtypeRail, 1827, 50},
		{46, "Toffee Hopper", RailtypeRail, 1827, 50},
		{47, "Bubble Van", RailtypeRail, 1827, 50},
		{48, "Cola Tanker", RailtypeRail, 1827, 50},
		{49, "Sweet Van", RailtypeRail, 1827, 50},
		{50, "Toy Van", RailtypeRail, 1827, 50},
		{51, "Battery Truck", RailtypeRail, 1827, 50},
		{52, "Fizzy Drink Truck", RailtypeRail, 1827, 50},
		{53, "Plastic Truck", RailtypeRail, 1827, 50},
		{54, "X2001 (Electric)", RailtypeMono, 28490, 50},
		{55, "Millennium Z1 (Electric)", RailtypeMono, 31047, 50},
		{56, "Wizzowow Z99", RailtypeMono, 28855, 50},
		{57, "Passenger Carriage", RailtypeMono, 1827, 50},
		{58, "Mail Van", RailtypeMono, 1827, 50},
		{59, "Coal Truck", RailtypeMono, 1827, 50},
		{60, "Oil Tanker", RailtypeMono, 1827, 50},
		{61, "Livestock Van", RailtypeMono, 1827, 50},
		{62, "Goods Van", RailtypeMono, 1827, 50},
		{63, "Grain Hopper", RailtypeMono, 1827, 50},
		{64, "Wood Truck", RailtypeMono, 1827, 50},
		{65, "Iron Ore Hopper", RailtypeMono, 1827, 50},
		{66, "Steel Truck", RailtypeMono, 1827, 50},
		{67, "Armoured Van", RailtypeMono, 1827, 50},
		{68, "Food Van", RailtypeMono, 1827, 50},
		{69, "Paper Truck", RailtypeMono, 1827, 50},
		{70, "Copper Ore Hopper", RailtypeMono, 1827, 50},
		{71, "Water Tanker", RailtypeMono, 1827, 50},
		{72, "Fruit Truck", RailtypeMono, 1827, 50},
		{73, "Rubber Truck", RailtypeMono, 1827, 50},
		{74, "Sugar Truck", RailtypeMono, 1827, 50},
		{75, "Candyfloss Hopper", RailtypeMono, 1827, 50},
		{76, "Toffee Hopper", RailtypeMono, 1827, 50},
		{77, "Bubble Van", RailtypeMono, 1827, 50},
		{78, "Cola Tanker", RailtypeMono, 1827, 50},
		{79, "Sweet Van", RailtypeMono, 1827, 50},
		{80, "Toy Van", RailtypeMono, 1827, 50},
		{81, "Battery Truck", RailtypeMono, 1827, 50},
		{82, "Fizzy Drink Truck", RailtypeMono, 1827, 50},
		{83, "Plastic Truck", RailtypeMono, 1827, 50},
		{84, "Lev1 'Leviathan' (Electric)", RailtypeMaglev, 36525, 50},
		{85, "Lev2 'Cyclops' (Electric)", RailtypeMaglev, 39447, 50},
		{86, "Lev3 'Pegasus' (Electric)", RailtypeMaglev, 42004, 50},
		{87, "Lev4 'Chimaera' (Electric)", RailtypeMaglev, 42735, 50},
		{88, "Wizzowow Rocketeer", RailtypeMaglev, 36891, 60},
	}
	m := make(map[uint16]DefaultTrainEngine, len(rows))
	for _, r := range rows {
		introYear := dayCountToYear(daysTill1920 + r.introDays)
		retireYear := 0
		if r.baseLifeYears != 255 {
			retireYear = introYear + r.baseLifeYears
		}
		m[r.id] = DefaultTrainEngine{
			InternalID: r.id, Name: r.name, Railtype: r.railtype,
			IntroYear: introYear, RetireYear: retireYear,
		}
	}
	return m
}
