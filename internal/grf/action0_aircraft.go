package grf

// aircraftPropSizes mirrors newgrf_act0_aircraft.cpp's
// AircraftVehicleChangeInfo switch (feature GSF_AIRCRAFT, 0x03),
// verified directly against that source -- not guessed. -1 means
// "variable size, handled specially" (see applyAircraftProperty).
// Common vehicle properties are included directly, same as
// trainPropSizes. Aircraft have no track-type or power property (they
// have speed, range, and separate passenger/mail capacities instead --
// this package only reads passenger capacity into Capacity, matching
// what's actually useful for comparing replacement candidates).
var aircraftPropSizes = map[uint8]int{
	0x00: 2, 0x02: 1, 0x03: 1, 0x04: 1, 0x06: 1, 0x07: 1,
	0x08: 1, 0x09: 1, 0x0A: 1, 0x0B: 1, 0x0C: 1, 0x0D: 1, 0x0E: 1, 0x0F: 2,
	0x11: 1, 0x12: 1, 0x13: 4, 0x14: 1, 0x15: 1, 0x16: 1, 0x17: 1,
	0x18: 2, 0x19: 2, 0x1A: 4,
	0x1B: -1, // extended byte (alter purchase list sort order)
	0x1C: 2,
	0x1D: -1, 0x1E: -1, // CTT refit include/exclude lists
	0x1F: 2, 0x20: 2, 0x21: 4, 0x22: 1, 0x23: 2,
	0x24: -1, // badge list
}

func parseAircraftAction0(r *byteReader, first, numinfo uint32, prop uint8, engines map[uint16]*ParsedEngine) error {
	for i := uint32(0); i < numinfo; i++ {
		id := uint16(first + i)
		e, ok := engines[id]
		if !ok {
			e = &ParsedEngine{LocalID: id, Feature: gsfAircraft}
			engines[id] = e
		}
		if err := applyAircraftProperty(r, prop, e); err != nil {
			return err
		}
	}
	return nil
}

func applyAircraftProperty(r *byteReader, prop uint8, e *ParsedEngine) error {
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
	case 0x0C: // speed: raw unit is 8 mph, OpenTTD itself rescales to
		// "1 unit is roughly 1 km/h" via *128/10 (see PROP_AIRCRAFT_SPEED
		// in newgrf_act0_aircraft.cpp) -- applied here too so this
		// package's Speed values stay comparable to the other three
		// vehicle types' (which are already in that same display unit).
		b, err := r.ReadByte()
		if err != nil {
			return err
		}
		e.HasSpeed = true
		e.Speed = uint16(uint32(b) * 128 / 10)
	case 0x0F: // passenger capacity (PROP_AIRCRAFT_PASSENGER_CAPACITY)
		w, err := r.ReadWord()
		if err != nil {
			return err
		}
		e.HasCapacity = true
		e.Capacity = w
	case 0x1A: // long intro date -- always wins over the short-format 0x00
		d, err := r.ReadDWord()
		if err != nil {
			return err
		}
		e.HasIntroDate = true
		e.IntroDate = int32(d)
	case 0x1B: // alter purchase list sort order -- extended byte
		if _, err := r.ReadExtendedByte(); err != nil {
			return err
		}
	case 0x1D, 0x1E: // CTT refit include/exclude list: count byte + count bytes
		count, err := r.ReadByte()
		if err != nil {
			return err
		}
		if err := r.Skip(int(count)); err != nil {
			return err
		}
	case 0x24: // badge list: word count + count words
		count, err := r.ReadWord()
		if err != nil {
			return err
		}
		if err := r.Skip(int(count) * 2); err != nil {
			return err
		}
	default:
		size, ok := aircraftPropSizes[prop]
		if !ok || size < 0 {
			return errUnknownProperty(prop)
		}
		if err := r.Skip(size); err != nil {
			return err
		}
	}
	return nil
}
