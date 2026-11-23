//go:build !race

package rendertest

// raceSlowdown is how many times longer the harness waits when the test
// binary is built with -race, whose detector slows the kernel's jobs and the
// software adapter's rasterizing as much: 1 without it.
const raceSlowdown = 1
