//go:build !race

package m

// raceEnabled is true when the test binary is built with -race. Allocation
// counts are not meaningful under the detector (sync.Pool drops items at
// random, and the race runtime allocates on its own), so every test that
// fails on an allocation count skips its count assertion when it is set.
// The allocation assertions still run in a plain go test.
const raceEnabled = false
