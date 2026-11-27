//go:build !js

package otosound

import (
	"io"
	"time"
)

// device is how otosound reaches the sound card, an interface so that
// everything above it is testable without one: a test needing a sound card
// could not run in CI, and a process has one oto context to spend.
type device interface {
	// probe answers errNoOutputDevice on a machine with no audio output
	// device at all, and nil on one that has one, on one it could not find
	// out about, and once the process's context exists: a device gone since
	// the context was made is a loss, which is not its question. It is asked
	// before every open, since a context opened while nothing exists is
	// silent for the life of the process.
	probe() error
	// open takes the process's context, or creates it at sampleRate and
	// bufferSize, and answers the settings in force, which differ from those
	// asked for when an earlier kernel created the context. It does not wait
	// for the device, so it is cheap enough to call from sfx's Start.
	open(sampleRate int, bufferSize time.Duration) (deviceSettings, error)
	// play waits for the context to be ready, then makes this kernel's player
	// over source and starts it. It is called after open succeeded, and again
	// after each loss, once close has stopped the player before it.
	play(source io.Reader) error
	// poll answers the error the context, or this kernel's player, has met
	// since play: a device that went away, which oto shows only to whoever
	// asks. It answers nil while the device plays.
	poll() error
	// close stops this kernel's player, if it has one, and returns only once
	// the player has stopped reading its source. The context is the
	// process's, and outlives every kernel in it.
	close()
}

// deviceSettings are the settings of the context in force, which the first
// kernel in the process to open it asked for.
type deviceSettings struct {
	sampleRate int
	bufferSize time.Duration
}
