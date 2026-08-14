package engine

// SubstituteEngineFor looks up what a train vehicle whose engine_type is
// engineType is CURRENTLY being displayed as in-game: when the GRF that
// really defines an engine slot is missing, OpenTTD falls back to the
// closest matching base-game default engine (EIDSEntry.SubstituteID) so
// the vehicle still renders as *something* rather than nothing -- this is
// the literal mechanism behind the "shows up as a generic default train"
// symptom this whole tool exists to fix. Returning that substitute's real
// stats lets the UI show a "currently displayed as" comparison against
// replacement candidates, even though the vehicle's true original engine
// (whatever the missing GRF actually defined) can't be recovered.
func SubstituteEngineFor(eids []EIDSEntry, engineType uint16) (DefaultTrainEngine, bool) {
	for _, e := range eids {
		if e.EngineID == int(engineType) {
			d, ok := DefaultTrainEngines[uint16(e.SubstituteID)]
			return d, ok
		}
	}
	return DefaultTrainEngine{}, false
}
