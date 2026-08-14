package grf

import "fmt"

// DebugDumpActionCounts is a temporary diagnostic helper (not meant for
// production use) that prints how many pseudo-sprites of each action
// type were found, plus the first few Action0 blocks' header fields, to
// help verify the container walker and Action0 dispatcher are correctly
// aligned against a real file.
func DebugDumpActionCounts(path string) error {
	data, err := loadFile(path)
	if err != nil {
		return err
	}
	sprites, err := walkContainer2(data)
	if err != nil {
		return fmt.Errorf("walkContainer2: %w (found %d pseudo-sprites before failing)", err, len(sprites))
	}
	counts := map[byte]int{}
	featureCounts := map[byte]int{}
	trainPropCounts := map[byte]int{}
	action0Total := 0
	action0TrainBlocks := 0
	for _, ps := range sprites {
		if len(ps.Data) == 0 {
			continue
		}
		counts[ps.Data[0]]++
		if ps.Data[0] != 0x00 {
			continue
		}
		action0Total++
		body := ps.Data[1:]
		r := newByteReader(body)
		feature, _ := r.ReadByte()
		featureCounts[feature]++
		numprops, _ := r.ReadByte()
		_, _ = r.ReadByte() // numinfo
		_, _ = r.ReadExtendedByte()
		if feature != gsfTrains {
			continue
		}
		action0TrainBlocks++
		for p := uint8(0); p < numprops; p++ {
			if !r.HasData(1) {
				break
			}
			prop, _ := r.ReadByte()
			trainPropCounts[prop]++
			size, ok := trainPropSizes[prop]
			if !ok {
				break // unknown -- can't safely continue reading this block's remaining properties
			}
			if size < 0 {
				break // variable-size property this quick scan doesn't special-case
			}
			if err := r.Skip(size); err != nil {
				break
			}
		}
	}
	fmt.Printf("[debug] total pseudo-sprites: %d\n", len(sprites))
	for action, n := range counts {
		fmt.Printf("[debug] action 0x%02X: %d blocks\n", action, n)
	}
	fmt.Printf("[debug] Action0 total: %d, feature breakdown:\n", action0Total)
	for f, n := range featureCounts {
		fmt.Printf("[debug]   feature 0x%02X: %d blocks\n", f, n)
	}
	fmt.Printf("[debug] Action0 feature=TRAINS blocks: %d, property number frequency:\n", action0TrainBlocks)
	for p, n := range trainPropCounts {
		fmt.Printf("[debug]   prop 0x%02X: %d times\n", p, n)
	}
	return nil
}
