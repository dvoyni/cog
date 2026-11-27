//go:build !js

package otosound

// The format the mixer writes and the device plays: stereo, since panning is,
// of little-endian float32 samples.
const (
	outputChannels = 2
	bytesPerFrame  = outputChannels * 4
)

// mixer is the io.Reader the device's player pulls, one read a block, on the
// device's thread. It plays silence: every block it fills is zeros. It never
// blocks, since reads are sequential across a context's players, and a
// blocking read would hold up every other kernel's.
type mixer struct{}

// Read fills block with silence.
func (*mixer) Read(block []byte) (int, error) {
	clear(block)
	return len(block), nil
}
