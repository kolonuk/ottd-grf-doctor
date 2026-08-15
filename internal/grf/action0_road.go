package grf

// roadPropSizes mirrors newgrf_act0_roadvehs.cpp's RoadVehicleChangeInfo
// switch (feature GSF_ROADVEHICLES, 0x01), verified directly against
// that source -- not guessed. -1 means "variable size, handled
// specially" (see applyRoadProperty). Common vehicle properties
// (0x00,0x02,0x03,0x04,0x06,0x07 -- CommonVehicleChangeInfo in
// src/newgrf/newgrf_act0.cpp, shared verbatim by every vehicle feature)
// are included directly, same as trainPropSizes does.
var roadPropSizes = map[uint8]int{
	0x00: 2, 0x02: 1, 0x03: 1, 0x04: 1, 0x06: 1, 0x07: 1,
	0x05: 1, 0x08: 1, 0x09: 1, 0x0A: 4, 0x0E: 1, 0x0F: 1, 0x10: 1, 0x11: 1,
	0x12: 1, 0x13: 1, 0x14: 1, 0x15: 1, 0x16: 4, 0x17: 1, 0x18: 1, 0x19: 1,
	0x1A: 1, 0x1B: 1, 0x1C: 1, 0x1D: 2, 0x1E: 2, 0x1F: 4,
	0x20: -1, // extended byte (alter purchase list sort order)
	0x21: 1, 0x22: 2, 0x23: 1,
	0x24: -1, 0x25: -1, // CTT refit include/exclude lists
	0x26: 2, 0x27: 4, 0x28: 1, 0x29: 2,
	0x2A: -1, // badge list
}

func parseRoadAction0(r *byteReader, first, numinfo uint32, prop uint8, engines map[uint16]*ParsedEngine) error {
	for i := uint32(0); i < numinfo; i++ {
		id := uint16(first + i)
		e, ok := engines[id]
		if !ok {
			e = &ParsedEngine{LocalID: id, Feature: gsfRoadVehicles}
			engines[id] = e
		}
		if err := applyRoadProperty(r, prop, e); err != nil {
			return err
		}
	}
	return nil
}

func applyRoadProperty(r *byteReader, prop uint8, e *ParsedEngine) error {
	switch prop {
	case 0x00: // short intro date (common)
		w, err := r.ReadWord()
		if err != nil {
			return err
		}
		if !e.HasIntroDate {
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
	case 0x05: // road/tram type
		b, err := r.ReadByte()
		if err != nil {
			return err
		}
		e.HasTrackType = true
		e.TrackType = b
		e.TrackTypeIsTranslated = false
	case 0x08: // speed (1 unit = 0.5 km-ish/h)
		b, err := r.ReadByte()
		if err != nil {
			return err
		}
		e.HasSpeed = true
		e.Speed = uint16(b)
	case 0x0F: // cargo capacity (PROP_ROADVEH_CARGO_CAPACITY)
		b, err := r.ReadByte()
		if err != nil {
			return err
		}
		e.HasCapacity = true
		e.Capacity = uint16(b)
	case 0x10: // cargo type
		b, err := r.ReadByte()
		if err != nil {
			return err
		}
		e.HasCargoType = true
		e.CargoType = b
	case 0x13: // power, in 10 HP (PROP_ROADVEH_POWER)
		b, err := r.ReadByte()
		if err != nil {
			return err
		}
		e.HasPower = true
		e.Power = uint16(b)
	case 0x14: // weight, in 1/4 tons (PROP_ROADVEH_WEIGHT)
		b, err := r.ReadByte()
		if err != nil {
			return err
		}
		e.HasWeight = true
		e.Weight = uint16(b)
	case 0x1F: // long intro date -- always wins over the short-format 0x00
		d, err := r.ReadDWord()
		if err != nil {
			return err
		}
		e.HasIntroDate = true
		e.IntroDate = int32(d)
	case 0x20: // alter purchase list sort order -- extended byte
		if _, err := r.ReadExtendedByte(); err != nil {
			return err
		}
	case 0x24, 0x25: // CTT refit include/exclude list: count byte + count bytes
		count, err := r.ReadByte()
		if err != nil {
			return err
		}
		if err := r.Skip(int(count)); err != nil {
			return err
		}
	case 0x2A: // badge list: word count + count words
		count, err := r.ReadWord()
		if err != nil {
			return err
		}
		if err := r.Skip(int(count) * 2); err != nil {
			return err
		}
	default:
		size, ok := roadPropSizes[prop]
		if !ok || size < 0 {
			return errUnknownProperty(prop)
		}
		if err := r.Skip(size); err != nil {
			return err
		}
	}
	return nil
}
