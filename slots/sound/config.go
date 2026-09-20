package sound

// DefaultMaxVoices is the voice cap when Config names none.
const DefaultMaxVoices = 64

// Config configures the sound Slot. It is supplied under Name, and its zero
// value is the default.
//
// There is one number, and the tempting shape is two - a total, and a smaller
// one for streamed Voices. That shape is foreclosed rather than rejected: the
// tier lives entirely inside the Adapter and sound never learns it, so sound
// cannot count what it cannot see. The spec states the worst case honestly
// instead: 64 streamed Voices is about 8.7 MB and 64 goroutines. The number a
// game tunes is the one it can reason about - how many sounds it wants at once
// - and the memory follows from what it plays.
type Config struct {
	// MaxVoices is how many Voices may exist at once. Zero means
	// DefaultMaxVoices.
	MaxVoices int
}

// WithMaxVoices sets the voice cap.
func (c Config) WithMaxVoices(n int) Config {
	c.MaxVoices = n
	return c
}
