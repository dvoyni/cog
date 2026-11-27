//go:build !js

package otosound

import (
	"errors"
	"fmt"
	"time"
)

// The keys otosound's two device reports are logged once under, each of its
// own, so each is said once for the kernel's life.
const (
	deviceUnavailableLogKey   = "otosound: device unavailable"
	deviceConfigIgnoredLogKey = "otosound: device config ignored"
)

// ErrDeviceUnavailable reports a device that could not be opened at all: the
// open was refused, or the machine has no output device. It is logged once,
// as a warning, since it is a fact about the machine the game runs on rather
// than a mistake of the game's, and otosound then behaves as the headless
// backend does: it accepts everything, plays nothing, and the game runs,
// while the open is retried every Config.DeviceRetry.
//
// A device that was ready and is then lost is reported never: Device going
// not ready is the whole of that notice, since loss is ordinary.
type ErrDeviceUnavailable struct {
	// Err is what the open failed with.
	Err error
}

func (e ErrDeviceUnavailable) Error() string {
	return fmt.Sprintf("otosound: no audio device could be opened: %v", e.Err)
}

func (e ErrDeviceUnavailable) Unwrap() error { return e.Err }

// ErrDeviceConfigIgnored reports a kernel whose SampleRate or BufferSize,
// defaults filled in, differ from those of the process's oto context, which
// an earlier kernel made: oto's context is per process, so the first
// kernel's settings are in force for every kernel after it. The kernel is
// audible all the same, on a player of its own, and its Device answers the
// settings in force. It is logged once, as an error, since the composition
// asked for what it cannot have.
type ErrDeviceConfigIgnored struct {
	// AskedSampleRate and SampleRate are the rate this kernel asked for and
	// the one in force.
	AskedSampleRate, SampleRate int
	// AskedBufferSize and BufferSize are the buffer this kernel asked for and
	// the one in force.
	AskedBufferSize, BufferSize time.Duration
}

func (e ErrDeviceConfigIgnored) Error() string {
	return fmt.Sprintf("otosound: the audio context is already open at %d Hz and %v, so this kernel's %d Hz and %v are ignored",
		e.SampleRate, e.BufferSize, e.AskedSampleRate, e.AskedBufferSize)
}

// errNoOutputDevice is a machine with no audio output device at all, which
// the probe answers before any open. It reaches a game wrapped in
// ErrDeviceUnavailable.
var errNoOutputDevice = errors.New("the machine reports no audio output device at all")

// errNotOurClip is Install handed a prepared clip otosound did not make. sfx
// installs only what otosound's own Prepare produced, so it guards against a
// future caller rather than a failure any composition can have.
var errNotOurClip = errors.New("otosound: Install was handed a prepared clip otosound did not make")
