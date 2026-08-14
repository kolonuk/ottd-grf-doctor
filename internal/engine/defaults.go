package engine

// DefaultTrainEngine describes one base-game (non-GRF) train engine, for
// display and compatibility-warning purposes when a fix target is a
// default engine rather than a third-party NewGRF's, and for the
// "currently displayed as" comparison shown against a broken vehicle
// (see SubstituteEngineFor).
//
// Every field is decoded directly from OpenTTD's own tables (see
// src/table/engines.h's RVI/MT/MM/MW macros and src/engine_type.h's
// EngineInfo/RailVehicleInfo structs), not guessed:
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
//   - Speed/Power: RVI's `d`/`e` arguments (max_speed, power) -- raw
//     internal units, the same representation grf.ParsedEngine.Speed/
//     Power use, so the two are directly comparable without conversion.
//   - IsWagon: RVI's `b` argument == RAILVEH_WAGON ('W' in the source
//     table) -- wagons have no independent speed/power of their own
//     (both are 0), matching grf.ParsedEngine.IsWagon's meaning.
type DefaultTrainEngine struct {
	InternalID uint16
	Name       string
	Railtype   Railtype
	IntroYear  int
	RetireYear int // 0 means "never retires" (EngineInfo.base_life == 0xFF)
	Speed      uint16
	Power      uint16
	IsWagon    bool
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

// defaultEngineRow mirrors one MT/MW macro call's date/life arguments
// plus the matching RVI macro's speed/power/wagon-ness -- the two macro
// tables sit in the same declaration order in src/table/engines.h (both
// indexed 0-88), so row i here always describes the same engine as row i
// there. decaySpeed/lifelength are unused by this tool but kept for
// fidelity to the source's MT/MW arguments.
type defaultEngineRow struct {
	id            uint16
	name          string
	railtype      Railtype
	introDays     int
	baseLifeYears int
	speed         uint16
	power         uint16
	isWagon       bool
}

// DefaultTrainEngines is every base-game train engine this tool has data
// for: the "Rail" (0-26 locos, 27-53 wagons), "Monorail" (54-83), and
// "Maglev" (84-88) sections of the temperate default engine table.
// Road/ship/aircraft/toyland/arctic/tropic-specific entries are not
// included -- extend this table if warnings for those become useful.
var DefaultTrainEngines = buildDefaultTrainEngines()

func buildDefaultTrainEngines() map[uint16]DefaultTrainEngine {
	rows := []defaultEngineRow{
		{0, "Kirby Paul Tank (Steam)", RailtypeRail, 1827, 30, 64, 300, false},
		{1, "MJS 250 (Diesel)", RailtypeRail, 12784, 30, 80, 600, false},
		{2, "Ploddyphut Choo-Choo", RailtypeRail, 9497, 50, 72, 400, false},
		{3, "Powernaut Choo-Choo", RailtypeRail, 11688, 30, 96, 900, false},
		{4, "Mightymover Choo-Choo", RailtypeRail, 16802, 30, 112, 1000, false},
		{5, "Ploddyphut Diesel", RailtypeRail, 18993, 30, 120, 1400, false},
		{6, "Powernaut Diesel", RailtypeRail, 20820, 30, 152, 2000, false},
		{7, "Wills 2-8-0 (Steam)", RailtypeRail, 8766, 30, 88, 1100, false},
		{8, "Chaney 'Jubilee' (Steam)", RailtypeRail, 5114, 30, 112, 1000, false},
		{9, "Ginzu 'A4' (Steam)", RailtypeRail, 5479, 30, 128, 1200, false},
		{10, "SH '8P' (Steam)", RailtypeRail, 12419, 25, 144, 1600, false},
		{11, "Manley-Morel DMU (Diesel)", RailtypeRail, 13149, 30, 112, 600, false},
		{12, "'Dash' (Diesel)", RailtypeRail, 23376, 35, 120, 700, false},
		{13, "SH/Hendry '25' (Diesel)", RailtypeRail, 14976, 28, 128, 1250, false},
		{14, "UU '37' (Diesel)", RailtypeRail, 14245, 30, 144, 1750, false},
		{15, "Floss '47' (Diesel)", RailtypeRail, 15341, 33, 160, 2580, false},
		{16, "CS 4000 (Diesel)", RailtypeRail, 14976, 25, 96, 4000, false},
		{17, "CS 2400 (Diesel)", RailtypeRail, 16437, 30, 112, 2400, false},
		{18, "Centennial (Diesel)", RailtypeRail, 18993, 30, 112, 6600, false},
		{19, "Kelling 3100 (Diesel)", RailtypeRail, 13880, 30, 104, 1500, false},
		{20, "Turner Turbo (Diesel)", RailtypeRail, 20454, 30, 160, 3500, false},
		{21, "MJS 1000 (Diesel)", RailtypeRail, 16071, 30, 104, 2200, false},
		{22, "SH '125' (Diesel)", RailtypeRail, 20820, 25, 200, 4500, false},
		{23, "SH '30' (Electric)", RailtypeElectric, 16437, 30, 160, 3600, false},
		{24, "SH '40' (Electric)", RailtypeElectric, 19359, 80, 176, 5000, false},
		{25, "'T.I.M.' (Electric)", RailtypeElectric, 23376, 30, 240, 7000, false},
		{26, "'AsiaStar' (Electric)", RailtypeElectric, 26298, 50, 264, 8000, false},
		{27, "Passenger Carriage", RailtypeRail, 1827, 50, 0, 0, true},
		{28, "Mail Van", RailtypeRail, 1827, 50, 0, 0, true},
		{29, "Coal Truck", RailtypeRail, 1827, 50, 0, 0, true},
		{30, "Oil Tanker", RailtypeRail, 1827, 50, 0, 0, true},
		{31, "Livestock Van", RailtypeRail, 1827, 50, 0, 0, true},
		{32, "Goods Van", RailtypeRail, 1827, 50, 0, 0, true},
		{33, "Grain Hopper", RailtypeRail, 1827, 50, 0, 0, true},
		{34, "Wood Truck", RailtypeRail, 1827, 50, 0, 0, true},
		{35, "Iron Ore Hopper", RailtypeRail, 1827, 50, 0, 0, true},
		{36, "Steel Truck", RailtypeRail, 1827, 50, 0, 0, true},
		{37, "Armoured Van", RailtypeRail, 1827, 50, 0, 0, true},
		{38, "Food Van", RailtypeRail, 1827, 50, 0, 0, true},
		{39, "Paper Truck", RailtypeRail, 1827, 50, 0, 0, true},
		{40, "Copper Ore Hopper", RailtypeRail, 1827, 50, 0, 0, true},
		{41, "Water Tanker", RailtypeRail, 1827, 50, 0, 0, true},
		{42, "Fruit Truck", RailtypeRail, 1827, 50, 0, 0, true},
		{43, "Rubber Truck", RailtypeRail, 1827, 50, 0, 0, true},
		{44, "Sugar Truck", RailtypeRail, 1827, 50, 0, 0, true},
		{45, "Candyfloss Hopper", RailtypeRail, 1827, 50, 0, 0, true},
		{46, "Toffee Hopper", RailtypeRail, 1827, 50, 0, 0, true},
		{47, "Bubble Van", RailtypeRail, 1827, 50, 0, 0, true},
		{48, "Cola Tanker", RailtypeRail, 1827, 50, 0, 0, true},
		{49, "Sweet Van", RailtypeRail, 1827, 50, 0, 0, true},
		{50, "Toy Van", RailtypeRail, 1827, 50, 0, 0, true},
		{51, "Battery Truck", RailtypeRail, 1827, 50, 0, 0, true},
		{52, "Fizzy Drink Truck", RailtypeRail, 1827, 50, 0, 0, true},
		{53, "Plastic Truck", RailtypeRail, 1827, 50, 0, 0, true},
		{54, "X2001 (Electric)", RailtypeMono, 28490, 50, 304, 9000, false},
		{55, "Millennium Z1 (Electric)", RailtypeMono, 31047, 50, 336, 10000, false},
		{56, "Wizzowow Z99", RailtypeMono, 28855, 50, 320, 5000, false},
		{57, "Passenger Carriage", RailtypeMono, 1827, 50, 0, 0, true},
		{58, "Mail Van", RailtypeMono, 1827, 50, 0, 0, true},
		{59, "Coal Truck", RailtypeMono, 1827, 50, 0, 0, true},
		{60, "Oil Tanker", RailtypeMono, 1827, 50, 0, 0, true},
		{61, "Livestock Van", RailtypeMono, 1827, 50, 0, 0, true},
		{62, "Goods Van", RailtypeMono, 1827, 50, 0, 0, true},
		{63, "Grain Hopper", RailtypeMono, 1827, 50, 0, 0, true},
		{64, "Wood Truck", RailtypeMono, 1827, 50, 0, 0, true},
		{65, "Iron Ore Hopper", RailtypeMono, 1827, 50, 0, 0, true},
		{66, "Steel Truck", RailtypeMono, 1827, 50, 0, 0, true},
		{67, "Armoured Van", RailtypeMono, 1827, 50, 0, 0, true},
		{68, "Food Van", RailtypeMono, 1827, 50, 0, 0, true},
		{69, "Paper Truck", RailtypeMono, 1827, 50, 0, 0, true},
		{70, "Copper Ore Hopper", RailtypeMono, 1827, 50, 0, 0, true},
		{71, "Water Tanker", RailtypeMono, 1827, 50, 0, 0, true},
		{72, "Fruit Truck", RailtypeMono, 1827, 50, 0, 0, true},
		{73, "Rubber Truck", RailtypeMono, 1827, 50, 0, 0, true},
		{74, "Sugar Truck", RailtypeMono, 1827, 50, 0, 0, true},
		{75, "Candyfloss Hopper", RailtypeMono, 1827, 50, 0, 0, true},
		{76, "Toffee Hopper", RailtypeMono, 1827, 50, 0, 0, true},
		{77, "Bubble Van", RailtypeMono, 1827, 50, 0, 0, true},
		{78, "Cola Tanker", RailtypeMono, 1827, 50, 0, 0, true},
		{79, "Sweet Van", RailtypeMono, 1827, 50, 0, 0, true},
		{80, "Toy Van", RailtypeMono, 1827, 50, 0, 0, true},
		{81, "Battery Truck", RailtypeMono, 1827, 50, 0, 0, true},
		{82, "Fizzy Drink Truck", RailtypeMono, 1827, 50, 0, 0, true},
		{83, "Plastic Truck", RailtypeMono, 1827, 50, 0, 0, true},
		{84, "Lev1 'Leviathan' (Electric)", RailtypeMaglev, 36525, 50, 400, 10000, false},
		{85, "Lev2 'Cyclops' (Electric)", RailtypeMaglev, 39447, 50, 448, 12000, false},
		{86, "Lev3 'Pegasus' (Electric)", RailtypeMaglev, 42004, 50, 480, 15000, false},
		{87, "Lev4 'Chimaera' (Electric)", RailtypeMaglev, 42735, 50, 640, 20000, false},
		{88, "Wizzowow Rocketeer", RailtypeMaglev, 36891, 60, 480, 10000, false},
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
			Speed: r.speed, Power: r.power, IsWagon: r.isWagon,
		}
	}
	return m
}
