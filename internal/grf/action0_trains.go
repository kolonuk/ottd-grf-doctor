package grf

// ParsedEngine is what this package can dynamically extract about one
// train engine a GRF defines, straight from its Action0 property blocks
// -- no hardcoded per-GRF data.
//
// Fields are left at their zero value when the GRF simply never sets
// that property (common -- e.g. a wagon has no Speed/Power). A missing
// value is NOT the same as "verified absent"; see the Has* flags.
type ParsedEngine struct {
	LocalID uint16
	Name    string // best-effort, see resolveNames; falls back to "Engine #<id>" if unresolved

	HasTrackType          bool
	TrackType             uint8 // raw property value: 0=rail/electric(engclass-dependent), 1=mono, 2=maglev, or an index into this GRF's own railtype translation table (see TrackTypeIsTranslated)
	TrackTypeIsTranslated bool

	HasIntroDate bool
	IntroDate    int32 // absolute day count (day 0 = 1 Jan year 0), same epoch as engine.ParseInGameYear

	HasModelLife bool
	ModelLife    uint8 // years; base game's "0xFF means infinite" convention applies

	HasSpeed     bool
	Speed        uint16
	HasPower     bool
	Power        uint16
	HasWeight    bool
	Weight       uint16 // low+high byte combined
	HasCapacity  bool
	Capacity     uint8
	HasCargoType bool
	CargoType    uint8 // raw translated-table index; see README.md for the caveat on interpreting this without the GRF's cargo translation table

	IsWagon      bool // inferred: Power property present and == 0, or Dual-headed absent and no power ever set
	IsDualHeaded bool
}

// trainPropSizes gives the exact on-disk size of every Action0 property
// this package knows how to skip/read for feature GSF_TRAINS (0x00),
// verified directly against OpenTTD's own property handler
// (src/newgrf/newgrf_act0_trains.cpp's RailVehicleChangeInfo) during this
// package's development -- not guessed. -1 means "variable size, handled
// specially" (see parseTrainProperty).
var trainPropSizes = map[uint8]int{
	// Common vehicle properties (src/newgrf/newgrf_act0.cpp CommonVehicleChangeInfo)
	0x00: 2, 0x02: 1, 0x03: 1, 0x04: 1, 0x06: 1, 0x07: 1,
	// Train-specific (newgrf_act0_trains.cpp RailVehicleChangeInfo)
	0x05: 1, 0x08: 1, 0x09: 2, 0x0B: 2, 0x0D: 1, 0x0E: 4, 0x12: 1, 0x13: 1,
	0x14: 1, 0x15: 1, 0x16: 1, 0x17: 1, 0x18: 1, 0x19: 1,
	0x1A: -1, // extended byte
	0x1B: 2, 0x1C: 1, 0x1D: 4, 0x1E: 1, 0x1F: 1, 0x20: 1, 0x21: 1, 0x22: 1,
	0x23: 1, 0x24: 1, 0x25: 1, 0x26: 1, 0x27: 1, 0x28: 2, 0x29: 2, 0x2A: 4,
	0x2B: 2,
	0x2C: -1, 0x2D: -1, // count-prefixed cargo lists
	0x2E: 2, 0x2F: 2, 0x30: 4, 0x31: 1, 0x32: 2,
	0x33: -1, // badge list
	0x34: -1, // count-prefixed track type list
}

// parseTrainAction0 applies one FeatureChangeInfo block's properties to
// every engine in [first, first+numinfo), creating entries in engines as
// needed. Property values for later IDs simply overwrite the same field
// on the same *ParsedEngine map entry seen across multiple Action0 calls
// (a GRF can and often does set different properties for the same engine
// in separate Action0 blocks).
func parseTrainAction0(r *byteReader, first, numinfo uint32, prop uint8, engines map[uint16]*ParsedEngine) error {
	for i := uint32(0); i < numinfo; i++ {
		id := uint16(first + i)
		e, ok := engines[id]
		if !ok {
			e = &ParsedEngine{LocalID: id}
			engines[id] = e
		}
		if err := applyTrainProperty(r, prop, e); err != nil {
			return err
		}
	}
	return nil
}

func applyTrainProperty(r *byteReader, prop uint8, e *ParsedEngine) error {
	switch prop {
	case 0x00: // short intro date (common) -- superseded by 0x2A if also present
		w, err := r.ReadWord()
		if err != nil {
			return err
		}
		if !e.HasIntroDate { // don't let an earlier 0x2A be overwritten by a later short-format 0x00
			e.HasIntroDate = true
			e.IntroDate = int32(daysTill1920) + int32(w)
		}
	case 0x04: // model life (common)
		b, err := r.ReadByte()
		if err != nil {
			return err
		}
		e.HasModelLife = true
		e.ModelLife = b
	case 0x05: // track type
		b, err := r.ReadByte()
		if err != nil {
			return err
		}
		e.HasTrackType = true
		e.TrackType = b
		e.TrackTypeIsTranslated = false // this package doesn't read the GRF's own railtype translation table (Action0 GSF_RAILTYPES) -- see README.md
	case 0x09: // speed
		w, err := r.ReadWord()
		if err != nil {
			return err
		}
		e.HasSpeed = true
		e.Speed = w
	case 0x0B: // power
		w, err := r.ReadWord()
		if err != nil {
			return err
		}
		e.HasPower = true
		e.Power = w
		e.IsWagon = w == 0
	case 0x13: // dual-headed
		b, err := r.ReadByte()
		if err != nil {
			return err
		}
		e.IsDualHeaded = b != 0
	case 0x14: // cargo capacity
		b, err := r.ReadByte()
		if err != nil {
			return err
		}
		e.HasCapacity = true
		e.Capacity = b
	case 0x15: // cargo type
		b, err := r.ReadByte()
		if err != nil {
			return err
		}
		e.HasCargoType = true
		e.CargoType = b
	case 0x16: // weight low byte
		b, err := r.ReadByte()
		if err != nil {
			return err
		}
		e.HasWeight = true
		e.Weight = (e.Weight &^ 0xFF) | uint16(b)
	case 0x24: // weight high byte
		b, err := r.ReadByte()
		if err != nil {
			return err
		}
		e.HasWeight = true
		e.Weight = (e.Weight & 0xFF) | (uint16(b) << 8)
	case 0x2A: // long intro date -- always wins over a short-format 0x00
		d, err := r.ReadDWord()
		if err != nil {
			return err
		}
		e.HasIntroDate = true
		e.IntroDate = int32(d)
	case 0x1A: // alter purchase list sort order -- extended byte
		if _, err := r.ReadExtendedByte(); err != nil {
			return err
		}
	case 0x2C, 0x2D: // CTT refit include/exclude list: count byte + count bytes
		count, err := r.ReadByte()
		if err != nil {
			return err
		}
		if err := r.Skip(int(count)); err != nil {
			return err
		}
	case 0x33: // badge list: word count + count words
		count, err := r.ReadWord()
		if err != nil {
			return err
		}
		if err := r.Skip(int(count) * 2); err != nil {
			return err
		}
	case 0x34: // list of track types: byte count + count bytes
		count, err := r.ReadByte()
		if err != nil {
			return err
		}
		if err := r.Skip(int(count)); err != nil {
			return err
		}
		// Multiple/translated track types: this package can't summarize
		// that as a single TrackType value confidently, so it leaves
		// HasTrackType as whatever a plain prop 0x05 already set (if
		// any) rather than guessing.
	default:
		size, ok := trainPropSizes[prop]
		if !ok || size < 0 {
			return errUnknownProperty(prop)
		}
		if err := r.Skip(size); err != nil {
			return err
		}
	}
	return nil
}
