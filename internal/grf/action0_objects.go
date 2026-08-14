package grf

// ParsedObject is what this package can dynamically extract about one
// scenery/object a GRF defines (feature GSF_OBJECTS), straight from its
// Action0 property blocks -- property table verified directly against
// OpenTTD's own handler (src/newgrf/newgrf_act0_objects.cpp
// ObjectChangeInfo) during this package's development.
type ParsedObject struct {
	LocalID uint16
	ClassID uint32 // raw 4-byte class label (prop 0x08), byteswapped as OpenTTD does
	Name    string // best-effort, see resolveNames

	HasIntroDate bool
	IntroDate    int32 // absolute day count, same epoch as ParsedEngine.IntroDate

	HasEndOfLifeDate bool
	EndOfLifeDate    int32 // 0 means "never" (OpenTTD default when unset)

	NameStringID uint16 // raw GRFStringID from prop 0x0A, for name resolution against Action4 generic strings
}

// objectPropSizes mirrors newgrf_act0_objects.cpp's IgnoreObjectProperty
// switch (the sizes for properties this package skips) plus the ones it
// actively reads.
var objectPropSizes = map[uint8]int{
	0x0B: 1, 0x0C: 1, 0x0D: 1, 0x12: 1, 0x14: 1, 0x16: 1, 0x17: 1, 0x18: 1,
	0x09: 2, 0x0A: 2, 0x10: 2, 0x11: 2, 0x13: 2, 0x15: 2,
	0x08: 4, 0x0E: 4, 0x0F: 4,
	0x19: -1, // badge list
}

func parseObjectAction0(r *byteReader, first, numinfo uint32, prop uint8, objects map[uint16]*ParsedObject) error {
	for i := uint32(0); i < numinfo; i++ {
		id := uint16(first + i)
		o, ok := objects[id]
		if !ok {
			o = &ParsedObject{LocalID: id}
			objects[id] = o
		}
		if err := applyObjectProperty(r, prop, o); err != nil {
			return err
		}
	}
	return nil
}

func applyObjectProperty(r *byteReader, prop uint8, o *ParsedObject) error {
	switch prop {
	case 0x08: // class ID (byteswapped, per OpenTTD's own std::byteswap(classid))
		d, err := r.ReadDWord()
		if err != nil {
			return err
		}
		o.ClassID = byteswap32(d)
	case 0x0A: // object name -> GRFStringID for later Action4 resolution
		w, err := r.ReadWord()
		if err != nil {
			return err
		}
		o.NameStringID = w
	case 0x0E: // introduction date
		d, err := r.ReadDWord()
		if err != nil {
			return err
		}
		o.HasIntroDate = true
		o.IntroDate = int32(d)
	case 0x0F: // end of life date
		d, err := r.ReadDWord()
		if err != nil {
			return err
		}
		o.HasEndOfLifeDate = true
		o.EndOfLifeDate = int32(d)
	case 0x19: // badge list: word count + count words
		count, err := r.ReadWord()
		if err != nil {
			return err
		}
		if err := r.Skip(int(count) * 2); err != nil {
			return err
		}
	default:
		size, ok := objectPropSizes[prop]
		if !ok || size < 0 {
			return errUnknownProperty(prop)
		}
		if err := r.Skip(size); err != nil {
			return err
		}
	}
	return nil
}

func byteswap32(v uint32) uint32 {
	return (v>>24)&0xFF | (v>>8)&0xFF00 | (v<<8)&0xFF0000 | (v << 24)
}
