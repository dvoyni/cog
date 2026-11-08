//go:build darwin

package internal

import "time"

// defaultBufferSize is how much audio the device buffers when Config names
// none, and so the declick ramp's length, because the ramp is one block.
//
// It is longer on macOS than elsewhere because oto's AudioQueue driver splits
// the buffer into four AudioQueue buffers, and at 10ms each is 120 frames,
// which the device plays as noise and the game as silence. Measured on an M2
// Pro's speakers in 2026-11 with a 440Hz sine: 10ms noise; 20ms, 40ms, 60ms and
// oto's own default clean. 40ms leaves headroom over the smallest clean size
// for a device thread that shares the machine with a game.
const defaultBufferSize = 40 * time.Millisecond
