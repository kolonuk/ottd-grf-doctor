package engine

// DefaultTrainEngine describes one base-game (non-GRF) train engine, for
// display and compatibility-warning purposes when a fix target is a
// default engine rather than a third-party NewGRF's.
//
// Railtype is the ONE piece of data this tool is confident about (it's
// directly encoded in each engine's RVI table entry -- see
// src/table/engines.h's RVI macro, `j` parameter). Intro/retirement
// dates are deliberately NOT modeled here: OpenTTD's EngineInfo struct
// packs them in a way (relative to a shifted epoch, combined with a
// separately-interpreted decay_speed/lifelength pair) this project
// couldn't confidently reverse-engineer in the time available, and a
// wrong-but-confident date warning is worse than none. Date-availability
// warnings (see warnings.go) only fire for third-party GRF candidates
// whose BaNaNaS description happens to mention a year range.
type DefaultTrainEngine struct {
	InternalID uint16
	Name       string
	Railtype   Railtype
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

// DefaultTrainEngines is every base-game train engine this tool has data
// for: the "Rail" (0-26 locos, 27-53 wagons) and "Monorail" (54-83)
// sections of the temperate default engine table. Aircraft/road/ship/
// maglev/toyland/arctic/tropic-specific entries are not included --
// extend this table if warnings for those become useful.
var DefaultTrainEngines = buildDefaultTrainEngines()

func buildDefaultTrainEngines() map[uint16]DefaultTrainEngine {
	type row struct {
		id       uint16
		name     string
		railtype Railtype
	}
	rows := []row{
		{0, "Kirby Paul Tank (Steam)", RailtypeRail},
		{1, "MJS 250 (Diesel)", RailtypeRail},
		{2, "Ploddyphut Choo-Choo", RailtypeRail},
		{3, "Powernaut Choo-Choo", RailtypeRail},
		{4, "Mightymover Choo-Choo", RailtypeRail},
		{5, "Ploddyphut Diesel", RailtypeRail},
		{6, "Powernaut Diesel", RailtypeRail},
		{7, "Wills 2-8-0 (Steam)", RailtypeRail},
		{8, "Chaney 'Jubilee' (Steam)", RailtypeRail},
		{9, "Ginzu 'A4' (Steam)", RailtypeRail},
		{10, "SH '8P' (Steam)", RailtypeRail},
		{11, "Manley-Morel DMU (Diesel)", RailtypeRail},
		{12, "'Dash' (Diesel)", RailtypeRail},
		{13, "SH/Hendry '25' (Diesel)", RailtypeRail},
		{14, "UU '37' (Diesel)", RailtypeRail},
		{15, "Floss '47' (Diesel)", RailtypeRail},
		{16, "CS 4000 (Diesel)", RailtypeRail},
		{17, "CS 2400 (Diesel)", RailtypeRail},
		{18, "Centennial (Diesel)", RailtypeRail},
		{19, "Kelling 3100 (Diesel)", RailtypeRail},
		{20, "Turner Turbo (Diesel)", RailtypeRail},
		{21, "MJS 1000 (Diesel)", RailtypeRail},
		{22, "SH '125' (Diesel)", RailtypeRail},
		{23, "SH '30' (Electric)", RailtypeElectric},
		{24, "SH '40' (Electric)", RailtypeElectric},
		{25, "'T.I.M.' (Electric)", RailtypeElectric},
		{26, "'AsiaStar' (Electric)", RailtypeElectric},
		{27, "Passenger Carriage", RailtypeRail},
		{28, "Mail Van", RailtypeRail},
		{29, "Coal Truck", RailtypeRail},
		{30, "Oil Tanker", RailtypeRail},
		{31, "Livestock Van", RailtypeRail},
		{32, "Goods Van", RailtypeRail},
		{33, "Grain Hopper", RailtypeRail},
		{34, "Wood Truck", RailtypeRail},
		{35, "Iron Ore Hopper", RailtypeRail},
		{36, "Steel Truck", RailtypeRail},
		{37, "Armoured Van", RailtypeRail},
		{38, "Food Van", RailtypeRail},
		{39, "Paper Truck", RailtypeRail},
		{40, "Copper Ore Hopper", RailtypeRail},
		{41, "Water Tanker", RailtypeRail},
		{42, "Fruit Truck", RailtypeRail},
		{43, "Rubber Truck", RailtypeRail},
		{44, "Sugar Truck", RailtypeRail},
		{45, "Candyfloss Hopper", RailtypeRail},
		{46, "Toffee Hopper", RailtypeRail},
		{47, "Bubble Van", RailtypeRail},
		{48, "Cola Tanker", RailtypeRail},
		{49, "Sweet Van", RailtypeRail},
		{50, "Toy Van", RailtypeRail},
		{51, "Battery Truck", RailtypeRail},
		{52, "Fizzy Drink Truck", RailtypeRail},
		{53, "Plastic Truck", RailtypeRail},
		{54, "X2001 (Electric)", RailtypeMono},
		{55, "Millennium Z1 (Electric)", RailtypeMono},
		{56, "Wizzowow Z99", RailtypeMono},
		{57, "Passenger Carriage", RailtypeMono},
		{58, "Mail Van", RailtypeMono},
		{59, "Coal Truck", RailtypeMono},
		{60, "Oil Tanker", RailtypeMono},
		{61, "Livestock Van", RailtypeMono},
		{62, "Goods Van", RailtypeMono},
		{63, "Grain Hopper", RailtypeMono},
		{64, "Wood Truck", RailtypeMono},
		{65, "Iron Ore Hopper", RailtypeMono},
		{66, "Steel Truck", RailtypeMono},
		{67, "Armoured Van", RailtypeMono},
		{68, "Food Van", RailtypeMono},
		{69, "Paper Truck", RailtypeMono},
		{70, "Copper Ore Hopper", RailtypeMono},
		{71, "Water Tanker", RailtypeMono},
		{72, "Fruit Truck", RailtypeMono},
		{73, "Rubber Truck", RailtypeMono},
		{74, "Sugar Truck", RailtypeMono},
		{75, "Candyfloss Hopper", RailtypeMono},
		{76, "Toffee Hopper", RailtypeMono},
		{77, "Bubble Van", RailtypeMono},
		{78, "Cola Tanker", RailtypeMono},
		{79, "Sweet Van", RailtypeMono},
		{80, "Toy Van", RailtypeMono},
		{81, "Battery Truck", RailtypeMono},
		{82, "Fizzy Drink Truck", RailtypeMono},
		{83, "Plastic Truck", RailtypeMono},
		{84, "Lev1 'Leviathan' (Electric)", RailtypeMaglev},
		{85, "Lev2 'Cyclops' (Electric)", RailtypeMaglev},
		{86, "Lev3 'Pegasus' (Electric)", RailtypeMaglev},
		{87, "Lev4 'Chimaera' (Electric)", RailtypeMaglev},
		{88, "Wizzowow Rocketeer", RailtypeMaglev},
	}
	m := make(map[uint16]DefaultTrainEngine, len(rows))
	for _, r := range rows {
		m[r.id] = DefaultTrainEngine{InternalID: r.id, Name: r.name, Railtype: r.railtype}
	}
	return m
}
