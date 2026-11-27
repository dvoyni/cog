package sfx

import "time"

// Device reports the sound device as it is now. It is read-only: a game cannot
// choose a backend, only see which one it got.
type Device struct {
	// Ready reports whether anything is audible. False means absent, not opened
	// yet, or lost; a game cannot tell which, and does not need to.
	Ready bool
	// Name is the backend's own name: "otosound", or "headless".
	Name string
	// SampleRate and Channels are the device's, not a clip's. Zero until Ready.
	SampleRate int
	Channels   int
	// Latency is the length of the output buffer in force, which is all oto
	// offers, not the figure a Config asked for. Zero until Ready. The contract
	// promises ordering and causality and never a latency figure, so this is
	// the only place one is reported.
	Latency time.Duration
}
