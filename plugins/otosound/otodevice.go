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

// open takes the process's context once one exists, and creates it at
// sampleRate and bufferSize otherwise: stereo float32, named PluginName.
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
		panic("otosound: a player is started once, after the context is open")
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

// close stops the player with PauseAndStopReading, not Pause: Pause keeps
// reading the source so that playback can resume without a gap, and
// PauseAndStopReading returns only once a read in progress has finished.
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
