//go:build !js && race

package otosound

// raceEnabled is true when the test binary is built with -race. Allocation
// counts are not meaningful under the detector, whose runtime allocates on
// its own, so a test that counts allocations skips its count under it; the
// counts are checked in a plain go test.
const raceEnabled = true
