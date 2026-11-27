//go:build !js

package otosound

// The format the mixer writes and the device plays: stereo, since panning is,
// of little-endian float32 samples.
const (
	outputChannels = 2
	bytesPerFrame  = outputChannels * 4
)

// mixer is the io.Reader the device's player pulls, one read a block, on the
// device's thread. At each block start it drains the handoff ring, applying
// every flush's published changes in order; it plays silence: every block it
// fills is zeros. It never blocks, since reads are sequential across a
// context's players, and a blocking read would hold up every other kernel's.
type mixer struct {
	// ring is the handoff ring, made at Voices: the flush side publishes to
	// it and the mixer drains it, the mixer through its atomics alone.
	ring *handoffRing
}

// Read drains the ring and fills block with silence.
func (m *mixer) Read(block []byte) (int, error) {
	m.ring.drain(m.apply)
	clear(block)
	return len(block), nil
}

// apply applies one drained operation to what the mixer plays: nothing yet,
// since it plays silence.
func (*mixer) apply(*operation) {}
