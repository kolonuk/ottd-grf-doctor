package grf

// shipPropSizes mirrors newgrf_act0_ships.cpp's ShipVehicleChangeInfo
// switch (feature GSF_SHIPS, 0x02), verified directly against that
// source -- not guessed. -1 means "variable size, handled specially"
// (see applyShipProperty). Common vehicle properties are included
// directly (0x00,0x02,0x03,0x04,0x06,0x07), same as trainPropSizes.
// Ships have no track/infrastructure-type property (they just need
// water) and no power property (only speed/cost/capacity).
var shipPropSizes = map[uint8]int{
	0x00: 2, 0x02: 1, 0x03: 1, 0x04: 1, 0x06: 1, 0x07: 1,
	0x08: 1, 0x09: 1, 0x0A: 1, 0x0B: 1, 0x0C: 1, 0x0D: 2, 0x0F: 1, 0x10: 1,
	0x11: 4, 0x12: 1, 0x13: 1, 0x14: 1, 0x15: 1, 0x16: 1, 0x17: 1,
	0x18: 2, 0x19: 2, 0x1A: 4,
	0x1B: -1, // extended byte (alter purchase list sort order)
	0x1C: 1, 0x1D: 2,
	0x1E: -1, 0x1F: -1, // CTT refit include/exclude lists
	0x20: 2, 0x21: 4, 0x22: 1, 0x23: 2, 0x24: 1, 0x25: 2,
	0x26: -1, // badge list
}

func parseShipAction0(r *byteReader, first, numinfo uint32, prop uint8, engines map[uint16]*ParsedEngine) error {
	for i := uint32(0); i < numinfo; i++ {
		id := uint16(first + i)
		e, ok := engines[id]
		if !ok {
			e = &ParsedEngine{LocalID: id, Feature: gsfShips}
			engines[id] = e
		}
		if err := applyShipProperty(r, prop, e); err != nil {
			return err
		}
	}
	return nil
}

func applyShipProperty(r *byteReader, prop uint8, e *ParsedEngine) error {
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
	case 0x0B: // speed (1 unit = 0.5 km-ish/h) -- superseded by 0x23 if also present
		b, err := r.ReadByte()
		if err != nil {
			return err
		}
		if !e.HasSpeed {
			e.HasSpeed = true
			e.Speed = uint16(b)
		}
	case 0x0C: // cargo type
		b, err := r.ReadByte()
		if err != nil {
			return err
		}
		e.HasCargoType = true
		e.CargoType = b
	case 0x0D: // cargo capacity (PROP_SHIP_CARGO_CAPACITY)
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
	case 0x1E, 0x1F: // CTT refit include/exclude list: count byte + count bytes
		count, err := r.ReadByte()
		if err != nil {
			return err
		}
		if err := r.Skip(int(count)); err != nil {
			return err
		}
	case 0x23: // speed, wider format (1 unit = 0.5 km-ish/h) -- always wins over 0x0B
		w, err := r.ReadWord()
		if err != nil {
			return err
		}
		e.HasSpeed = true
		e.Speed = w
	case 0x26: // badge list: word count + count words
		count, err := r.ReadWord()
		if err != nil {
			return err
		}
		if err := r.Skip(int(count) * 2); err != nil {
			return err
		}
	default:
		size, ok := shipPropSizes[prop]
		if !ok || size < 0 {
			return errUnknownProperty(prop)
		}
		if err := r.Skip(size); err != nil {
			return err
		}
	}
	return nil
}
