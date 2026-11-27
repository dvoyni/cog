//go:build !js

package otosound

import (
	"cmp"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/sfx"
)

// PluginName is otosound's plugin name, which its Device also answers as its
// name, and the application name it gives oto.
const PluginName kernel.PluginName = "otosound"

// Config's defaults, each what its zero field means, and the two sentinels
// of DecodedClipLimit.
const (
	defaultSampleRate       = 48000
	defaultBufferSize       = 10 * time.Millisecond
	defaultDecodedClipLimit = 512 << 10
	defaultRingDepth        = 8
	defaultReadAhead        = 200 * time.Millisecond
	defaultRefillWait       = 5 * time.Millisecond
	defaultDeviceRetry      = time.Second

	decodedClipLimitNone         = -1
	decodedClipLimitAlwaysStream = -2
)

// Config is otosound's part of the kernel's config, under PluginName. Its
// zero value is the default: a zero field takes the default its comment
// names. A negative value in a field that takes none, or a DecodedClipLimit
// below -2, is a composition error, and Init panics.
type Config struct {
	// SampleRate is the rate the mixer produces and the device consumes. A
	// clip is converted to it once, in Prepare, so the device thread never
	// resamples a base rate. Zero means 48000. It is the oto context's, which
	// is per process, so only the first kernel's is in force.
	SampleRate int
	// BufferSize is how much audio the device buffers, and so how long a block
	// the mixer produces and how long the declick ramp is. Zero means 10 ms,
	// which v1's spike ran for 12 s on Windows with no underrun. It is the oto
	// context's, which is per process, so only the first kernel's is in force.
	BufferSize time.Duration
	// DecodedClipLimit is the largest decoded size, in bytes, a clip may have
	// and still be decoded once at load; a larger clip streams, with a decoder
	// and a read-ahead ring per voice.
	//   0  means 512 KiB, the default
	//  -1  means no limit: always decode
	//  -2  means always stream: never decode at load
	//   >0 is the limit in bytes
	DecodedClipLimit int
	// RingDepth is how many flushes' published changes the handoff ring
	// between the flush and the mixer holds. Zero means 8.
	RingDepth int
	// ReadAhead is how much audio a streamed voice's read-ahead ring holds.
	// Zero means 200 ms.
	ReadAhead time.Duration
	// RefillWait is how long a streamed voice's read-ahead sleeps when its
	// ring is full. Zero means 5 ms.
	RefillWait time.Duration
	// DeviceRetry paces both the retry of a device that could not be taken
	// and the poll of an open device for loss. Zero means 1 s.
	DeviceRetry time.Duration
}

// Plugin is otosound. It implements kernel.Plugin alone, and holds the state
// the backend its Init binds to sfx.BackendSocket works on.
type Plugin struct {
	// device is how otosound reaches the sound card: oto, as NewPlugin sets
	// it, or the fake device a test sets in its place.
	device device
	// config is the kernel's Config with every default filled in.
	config Config
	// outputSampleRate is the rate the mixer produces and every clip is
	// converted to: the rate in force once Voices has taken the device,
	// which a context an earlier kernel made settles, and Config's until
	// then, or when the take failed. It is the flush side's alone.
	outputSampleRate int
	// mixer is what the device's player pulls.
	mixer mixer
	// clips is the clip table, every clip Install minted an ID for, under
	// it. It is the flush side's alone: a start's ClipID is resolved through
	// it when Emit stages the start, so the device thread never reads it.
	clips map[sfx.ClipID]*preparedClip
	// lastClipID is the ID Install minted last; the next is one more, so an
	// ID is never zero and never repeats.
	lastClipID sfx.ClipID
	// readAheads is each voice slot's read-ahead, nil for a slot playing no
	// streamed voice: Emit opens one on a start on a streamed clip, and
	// halts it on the slot's stop or on the start that replaces it. It is
	// the flush side's alone, and Destroy's, made at Voices.
	readAheads []*readAhead
	// readAheadGoroutines are the read-aheads still filling, halted or not,
	// which Destroy waits for.
	readAheadGoroutines sync.WaitGroup
	// releases are the destroyed clips held until the mixer has passed the
	// changes that destroyed them.
	releases []clipRelease
	// status is the Device as it is now: the device goroutine writes it, and
	// the flush reads it with no notice when it changes.
	status atomic.Pointer[sfx.Device]
	// deviceGoroutine is the device goroutine, which Destroy waits for
	// before it closes the player.
	deviceGoroutine sync.WaitGroup
	// finishedPreparesMutex guards finishedPrepares, the prepares finished
	// since the last TakePrepared, which the prepare goroutines append to.
	// It is the backend's only lock, and nothing the mixer runs can reach it.
	finishedPreparesMutex sync.Mutex
	finishedPrepares      []sfx.Prepared
	// prepareGoroutines are the prepares in flight, which Destroy waits for.
	prepareGoroutines sync.WaitGroup
}

// NewPlugin returns otosound, reaching the sound card through oto.
func NewPlugin() *Plugin { return &Plugin{device: &otoDevice{}} }

// Name returns PluginName.
func (*Plugin) Name() kernel.PluginName { return PluginName }

// Dependencies names none: binding sfx's socket is no dependency, since
// bindings resolve after every Init, and Init reads no other plugin's
// resource.
func (*Plugin) Dependencies() []kernel.PluginName { return nil }

// Init reads the config, fills in its defaults, makes the clip table, sets
// the Device not ready and named "otosound", and binds sfx.BackendSocket to
// the backend. It opens nothing, so it never fails on a device: sfx's Start
// opens it, through the backend's Voices. A config of another type than
// *Config is a composition error, and panics, as does a field out of its
// range: a negative SampleRate, BufferSize, RingDepth, ReadAhead, RefillWait
// or DeviceRetry, or a DecodedClipLimit below -2, which names no tier.
func (p *Plugin) Init(configurator *kernel.Configurator, config any) error {
	givenConfig := kernel.PluginConfigOrDefault[Config](config)
	checkNotNegative("SampleRate", givenConfig.SampleRate)
	checkNotNegative("BufferSize", givenConfig.BufferSize)
	checkNotNegative("RingDepth", givenConfig.RingDepth)
	checkNotNegative("ReadAhead", givenConfig.ReadAhead)
	checkNotNegative("RefillWait", givenConfig.RefillWait)
	checkNotNegative("DeviceRetry", givenConfig.DeviceRetry)
	if givenConfig.DecodedClipLimit < decodedClipLimitAlwaysStream {
		panic(fmt.Sprintf("otosound: Config.DecodedClipLimit is %d, and it may not be below -2, which names no tier", givenConfig.DecodedClipLimit))
	}
	p.config = Config{
		SampleRate:       cmp.Or(givenConfig.SampleRate, defaultSampleRate),
		BufferSize:       cmp.Or(givenConfig.BufferSize, defaultBufferSize),
		DecodedClipLimit: cmp.Or(givenConfig.DecodedClipLimit, defaultDecodedClipLimit),
		RingDepth:        cmp.Or(givenConfig.RingDepth, defaultRingDepth),
		ReadAhead:        cmp.Or(givenConfig.ReadAhead, defaultReadAhead),
		RefillWait:       cmp.Or(givenConfig.RefillWait, defaultRefillWait),
		DeviceRetry:      cmp.Or(givenConfig.DeviceRetry, defaultDeviceRetry),
	}
	p.outputSampleRate = p.config.SampleRate
	p.clips = map[sfx.ClipID]*preparedClip{}
	p.status.Store(&sfx.Device{Name: string(PluginName)})
	configurator.BindSocket[sfx.BackendSocket](backend{plugin: p})
	return nil
}

// Start does nothing: the device opens at the backend's Voices, which sfx
// calls from its own Start.
func (*Plugin) Start(*kernel.Kernel) error { return nil }

// Stop does nothing: a flush may still emit until the kernel has finished
// its jobs, so the player stays open until Destroy.
func (*Plugin) Stop(*kernel.Kernel) error { return nil }

// Destroy waits for the prepares in flight, halts every read-ahead and waits
// for them, since a read-ahead ends with its voice and a kernel being
// destroyed ends every voice whether or not sfx emitted the stops, waits for
// the device goroutine, and then closes this kernel's player, which stops reading the mixer before it
// returns, so a destroyed kernel leaves nothing of otosound's running. The
// oto context stays open, for the kernels after.
func (p *Plugin) Destroy() error {
	p.prepareGoroutines.Wait()
	for _, slotReadAhead := range p.readAheads {
		if slotReadAhead != nil {
			slotReadAhead.halt()
		}
	}
	p.readAheadGoroutines.Wait()
	p.deviceGoroutine.Wait()
	p.device.close()
	return nil
}

// checkNotNegative panics naming Config's field when its value is negative.
func checkNotNegative[V int | time.Duration](field string, value V) {
	if value < 0 {
		panic(fmt.Sprintf("otosound: Config.%s is %v, and it may not be negative", field, value))
	}
}
