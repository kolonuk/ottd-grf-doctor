package grf

import "fmt"

// ParsedGRF is everything this package extracted from one .grf file.
type ParsedGRF struct {
	Engines []ParsedEngine
	Objects []ParsedObject

	// Warnings accumulates non-fatal issues hit while parsing (e.g. an
	// Action0 property this package doesn't have a size for, which
	// forces it to stop reading properties for that block -- see
	// action0_trains.go). Parsing continues past these; only a
	// structurally malformed file (bad container header, truncated
	// pseudo-sprite) is a hard error.
	Warnings []string
}

type unknownPropertyError struct{ prop uint8 }

func (e unknownPropertyError) Error() string {
	return fmt.Sprintf("unknown property 0x%02X (unmapped size -- can't safely continue this Action0 block)", e.prop)
}

func errUnknownProperty(prop uint8) error { return unknownPropertyError{prop: prop} }

// ParseGRF reads a container-format-2 .grf file (see container.go's doc
// comment for what that means and its limits) and extracts every
// vehicle (train, road vehicle, ship, aircraft) and object it defines,
// with whatever properties this package knows how to read (see
// action0_trains.go / action0_road.go / action0_ships.go /
// action0_aircraft.go / action0_objects.go for exactly which --
// verified directly against OpenTTD's own property handlers, not
// guessed). ParsedEngine.Feature distinguishes which of the four
// vehicle types each entry describes; local IDs are only unique within
// one feature (a road vehicle #5 and a ship #5 are unrelated engines),
// so this keeps a separate map per feature while parsing.
func ParseGRF(path string) (*ParsedGRF, error) {
	data, err := loadFile(path)
	if err != nil {
		return nil, err
	}
	sprites, err := walkContainer2(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	trainEngines := map[uint16]*ParsedEngine{}
	roadEngines := map[uint16]*ParsedEngine{}
	shipEngines := map[uint16]*ParsedEngine{}
	aircraftEngines := map[uint16]*ParsedEngine{}
	objects := map[uint16]*ParsedObject{}
	names := newNameTable()
	result := &ParsedGRF{}

	for _, ps := range sprites {
		if len(ps.Data) == 0 {
			continue
		}
		action := ps.Data[0]
		body := ps.Data[1:]
		switch action {
		case 0x00:
			if err := parseAction0(body, trainEngines, roadEngines, shipEngines, aircraftEngines, objects); err != nil {
				result.Warnings = append(result.Warnings, err.Error())
			}
		case 0x04:
			if err := parseAction4(body, names); err != nil {
				result.Warnings = append(result.Warnings, "Action4: "+err.Error())
			}
		default:
			// Every other action (sprite layouts, callbacks, GRF info,
			// etc.) isn't needed for the properties this tool cares
			// about -- skipped by construction (we only look at Action0/
			// Action4 pseudo-sprites at all).
		}
	}

	resolveEngineNames(trainEngines, names.trainNames, result)
	resolveEngineNames(roadEngines, names.roadNames, result)
	resolveEngineNames(shipEngines, names.shipNames, result)
	resolveEngineNames(aircraftEngines, names.aircraftNames, result)

	for id, o := range objects {
		if name, ok := names.genericStrings[o.NameStringID]; ok {
			o.Name = name
		} else {
			o.Name = fmt.Sprintf("Object #%d", id)
		}
		result.Objects = append(result.Objects, *o)
	}

	return result, nil
}

func resolveEngineNames(engines map[uint16]*ParsedEngine, byID map[uint16]string, result *ParsedGRF) {
	for id, e := range engines {
		if name, ok := byID[id]; ok {
			e.Name = name
		} else {
			e.Name = fmt.Sprintf("Engine #%d", id)
		}
		result.Engines = append(result.Engines, *e)
	}
}

// parseAction0 decodes one Action0 pseudo-sprite (already past the
// leading 0x00 action byte) per FeatureChangeInfo's documented format:
//
//	<00> <feature> <num-props> <num-info> <id> (<property> <new-info>)...
func parseAction0(body []byte, trainEngines, roadEngines, shipEngines, aircraftEngines map[uint16]*ParsedEngine, objects map[uint16]*ParsedObject) error {
	r := newByteReader(body)
	feature, err := r.ReadByte()
	if err != nil {
		return err
	}
	numprops, err := r.ReadByte()
	if err != nil {
		return err
	}
	numinfo, err := r.ReadByte()
	if err != nil {
		return err
	}
	first, err := r.ReadExtendedByte()
	if err != nil {
		return err
	}

	for p := uint8(0); p < numprops; p++ {
		if !r.HasData(1) {
			return fmt.Errorf("Action0 feature 0x%02X: truncated before reading property %d/%d", feature, p+1, numprops)
		}
		prop, err := r.ReadByte()
		if err != nil {
			return err
		}
		switch feature {
		case gsfTrains:
			if err := parseTrainAction0(r, first, uint32(numinfo), prop, trainEngines); err != nil {
				return fmt.Errorf("Action0 trains, property 0x%02X: %w", prop, err)
			}
		case gsfRoadVehicles:
			if err := parseRoadAction0(r, first, uint32(numinfo), prop, roadEngines); err != nil {
				return fmt.Errorf("Action0 road vehicles, property 0x%02X: %w", prop, err)
			}
		case gsfShips:
			if err := parseShipAction0(r, first, uint32(numinfo), prop, shipEngines); err != nil {
				return fmt.Errorf("Action0 ships, property 0x%02X: %w", prop, err)
			}
		case gsfAircraft:
			if err := parseAircraftAction0(r, first, uint32(numinfo), prop, aircraftEngines); err != nil {
				return fmt.Errorf("Action0 aircraft, property 0x%02X: %w", prop, err)
			}
		case gsfObjects:
			if err := parseObjectAction0(r, first, uint32(numinfo), prop, objects); err != nil {
				return fmt.Errorf("Action0 objects, property 0x%02X: %w", prop, err)
			}
		default:
			// Not a feature this package extracts data for -- but we
			// still can't safely skip an unknown-sized property block
			// for an unmapped feature, so stop processing this
			// particular Action0 call cleanly (already-parsed features
			// from earlier blocks are unaffected).
			return nil
		}
	}
	return nil
}
