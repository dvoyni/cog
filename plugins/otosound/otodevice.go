//go:build !js

package otosound

import (
	"io"
	"sync"
	"time"

	"github.com/ebitengine/oto/v3"
)

// processContext is the process's one sharedContext, nil until the first
// open creates it, under contextMutex. It is package state because oto's
// context is: creating a second is an error, so the first kernel's settings
// are in force for every kernel in the process, after that kernel is
// destroyed too. oto has no way to close a context, so it is never closed.
var (
	contextMutex   sync.Mutex
	processContext *sharedContext
)

// otoDevice is the real sound card: the process's oto context, and this
// kernel's player on it.
type otoDevice struct {
	// sharedContext is the process's context, once open has taken it.
	sharedContext *sharedContext
	// player is this kernel's, kept reachable while it plays, since oto
	// stops a player it can no longer reach.
	player *oto.Player
}

// probe asks the machine whether it has an output device, until the
// process's context exists, and answers nothing after: that context was
// created on a machine that had a device, and a device gone since is a
// loss, which otosound promises to stay silent on.
func (d *otoDevice) probe() error {
	contextMutex.Lock()
	created := processContext != nil
	contextMutex.Unlock()
	if created {
		return nil
	}
	return probeOutputDevice()
}

// open takes the process's context once one exists, and creates it at
// sampleRate and bufferSize otherwise: stereo float32, named PluginName. The
// first kernel's settings win, answered in force to every kernel after,
// whose device goroutine logs the ones it asked for as ignored. That holds by
// construction, here, since no test reaches oto's real context: a test would
// open a real device, and spend the process's one context.
func (d *otoDevice) open(sampleRate int, bufferSize time.Duration) (deviceSettings, error) {
	contextMutex.Lock()
	defer contextMutex.Unlock()
	if processContext == nil {
		created, ready, err := oto.NewContext(&oto.NewContextOptions{
			SampleRate:      sampleRate,
			ChannelCount:    outputChannels,
			Format:          oto.FormatFloat32LE,
			BufferSize:      bufferSize,
			ApplicationName: string(PluginName),
		})
		if err != nil {
			return deviceSettings{}, err
		}
		processContext = &sharedContext{
			context:  created,
			ready:    ready,
			settings: deviceSettings{sampleRate: sampleRate, bufferSize: bufferSize},
		}
	}
	d.sharedContext = processContext
	return d.sharedContext.settings, nil
}

// play waits for the context to be ready, answers its error if it failed,
// and otherwise makes this kernel's player over source and starts it. oto
// takes the context's buffer as a duration and the player's in bytes, and
// both are said: the context sizes the device's buffer, and the player the
// queue the mixer is pulled into, the buffer size at the rate in force.
func (d *otoDevice) play(source io.Reader) error {
	if d.sharedContext == nil || d.player != nil {
		panic("otosound: a player is started after the context is open, while none plays")
	}
	<-d.sharedContext.ready
	if err := d.sharedContext.context.Err(); err != nil {
		return err
	}
	settings := d.sharedContext.settings
	player := d.sharedContext.context.NewPlayer(source)
	player.SetBufferSize(int(settings.bufferSize.Seconds()*float64(settings.sampleRate)) * bytesPerFrame)
	player.Play()
	d.player = player
	return nil
}

// poll answers the context's error, which only ever joins, and then this
// kernel's player's. It is called only while the player plays, and panics
// otherwise.
func (d *otoDevice) poll() error {
	if d.player == nil {
		panic("otosound: a device is polled only while its player plays")
	}
	if err := d.sharedContext.context.Err(); err != nil {
		return err
	}
	return d.player.Err()
}

// close stops the player with PauseAndStopReading, not Pause: Pause keeps
// reading the source so that playback can resume without a gap, and
// PauseAndStopReading returns only once a read in progress has finished,
// which is the barrier detaching the handoff ring after a loss needs.
func (d *otoDevice) close() {
	if d.player == nil {
		return
	}
	d.player.PauseAndStopReading()
	d.player = nil
}

// sharedContext is the process's oto context, the channel it closes once it
// is ready, and the settings it was made with.
type sharedContext struct {
	context  *oto.Context
	ready    chan struct{}
	settings deviceSettings
}
