package grf

// nameTable accumulates Action4 ("FeatureNewName") results. Vehicles
// (trains, in this package's scope) are named directly by local ID,
// matching how OpenTTD itself resolves them (GetNewEngine(..., id) in
// FeatureNewName -- id IS the local engine ID, no indirection). Objects
// go through an extra indirection: Action0 prop 0x0A gives a GRFStringID
// that a *generic* (language bit 7 set) Action4 entry must match -- see
// action0_objects.go's ParsedObject.NameStringID.
type nameTable struct {
	trainNames     map[uint16]string // local engine ID -> name
	genericStrings map[uint16]string // raw string ID -> name (for object name resolution)
}

func newNameTable() *nameTable {
	return &nameTable{trainNames: map[uint16]string{}, genericStrings: map[uint16]string{}}
}

const (
	gsfTrains          = 0x00
	gsfObjects         = 0x0F
	gsfEnd             = 0x15 // GSF_END per newgrf.h's enum order
	gsfOriginalStrings = 0x48
)

// parseAction4 decodes one Action4 pseudo-sprite (already past the
// leading 0x04 action byte) per FeatureNewName's documented format (see
// this package's development notes / OpenTTD's newgrf_act4.cpp):
//
//	<04> <veh-type> <language-id> <num-veh> <offset> <data...>
func parseAction4(data []byte, nt *nameTable) error {
	r := newByteReader(data)
	feature, err := r.ReadByte()
	if err != nil {
		return err
	}
	if feature >= gsfEnd && feature != gsfOriginalStrings {
		return nil // unsupported feature, same as OpenTTD's own skip
	}
	lang, err := r.ReadByte()
	if err != nil {
		return err
	}
	num, err := r.ReadByte()
	if err != nil {
		return err
	}
	generic := lang&0x80 != 0

	var id uint32
	if generic {
		w, err := r.ReadWord()
		if err != nil {
			return err
		}
		id = uint32(w)
	} else if feature <= 0x03 || feature == 0x15 /* GSF_BADGES */ {
		id, err = r.ReadExtendedByte()
		if err != nil {
			return err
		}
	} else {
		b, err := r.ReadByte()
		if err != nil {
			return err
		}
		id = uint32(b)
	}

	for i := uint32(0); i < uint32(num); i++ {
		if !r.HasData(1) {
			break
		}
		name, err := r.ReadString0()
		if err != nil {
			return err
		}
		curID := uint16(id + i)
		if generic {
			nt.genericStrings[curID] = name
		} else if feature == gsfTrains {
			nt.trainNames[curID] = name
		}
		// Other non-generic features (road/ship/aircraft/badges) aren't
		// this package's concern yet.
	}
	return nil
}
