//go:build !race

package sfx_test

// raceEnabled is true when the test binary is built with -race. Allocation
// counts are not meaningful under the detector, whose runtime allocates on
// its own, so the allocation gate steps its beats for the detector to watch
// and checks no count; the counts are checked in a plain go test.
const raceEnabled = false
