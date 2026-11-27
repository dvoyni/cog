//go:build !js

package otosound

import (
	"sync"
	"time"

	"github.com/dvoyni/cog/plugins/sfx"
)

// deviceGoroutine is this kernel's device goroutine: it starts the player on
// the device Voices took, polls it for loss every Config.DeviceRetry, retries
// the take as often while there is none, and makes otosound's two device
// reports, through the Logger kept from Init. It runs outside the kernel, and
// shares nothing with the flush but the Device and the handoff ring's pulled
// flag, both atomics.
type deviceGoroutine struct {
	plugin *Plugin
	// stopping is closed by stop, which ends the goroutine at its next wait.
	stopping chan struct{}
	// running is the goroutine, which stop waits for.
	running sync.WaitGroup
	// everReady is whether a device has been ready in this kernel, after
	// which nothing the goroutine meets is reportable: a poll that errors and
	// a reopen that will not play are both the same device being gone, and a
	// game that has heard something must never be told the machine has no
	// sound card. The goroutine alone reads and writes it.
	everReady bool
}

// start runs the goroutine over what Voices' take answered: the settings
// in force, or why no device could be taken.
func (g *deviceGoroutine) start(settings deviceSettings, err error) {
	g.running.Go(func() { g.run(settings, err) })
}

// stop ends the goroutine and waits for it, so nothing logs after the kernel
// is gone. It may be called whether or not start was, once Init has made
// the goroutine's stopping channel, and panics before.
func (g *deviceGoroutine) stop() {
	if g.stopping == nil {
		panic("otosound: the device goroutine is stopped only after Init")
	}
	close(g.stopping)
	g.running.Wait()
}

// run is the goroutine's body, given what the take in start answered. A
// device it holds is started and watched; one it lost, or could not take or
// start, is retried every DeviceRetry, indefinitely, until the kernel is
// destroyed.
//
// What a retry can recover is what oto allows. An oto context's error only
// ever joins, and the context cannot be made again, so a retry after the
// context recorded an error takes the same context and fails to play on it
// for the rest of the process. The retry recovers a Windows machine that had
// no device at start, whose open the probe withheld, so no context exists
// until one appears; and oto rides out a device switch by itself, which
// otosound never sees as a loss.
func (g *deviceGoroutine) run(settings deviceSettings, err error) {
	for {
		if err == nil {
			g.reportIgnoredConfig(settings)
			// An Emit that read the ring pulled just before a loss detached
			// it may have published after the detach emptied the ring, so it
			// is emptied again before a player reads it, and no stale start
			// reaches the first block after an attach.
			g.plugin.mixer.ring.empty()
			err = g.plugin.device.play(&g.plugin.mixer)
		}
		if err == nil {
			g.attach(settings)
			if !g.waitForLoss() {
				return
			}
			g.detach()
		} else {
			g.reportUnavailable(err)
		}
		if !g.wait() {
			return
		}
		settings, err = g.take()
	}
}

// take asks whether the machine has an output device and, if it does, opens
// it. The probe runs before the open, and that order is the load-bearing
// part: oto's context is created once per process and never rebuilt, so one
// created while nothing exists is silent for the life of the process, and a
// device plugged in later would never be heard however often the take were
// retried. A machine with none and a refused open are one failure from here:
// no device could be taken.
func (g *deviceGoroutine) take() (deviceSettings, error) {
	if err := g.plugin.device.probe(); err != nil {
		return deviceSettings{}, err
	}
	return g.plugin.device.open(g.plugin.config.SampleRate, g.plugin.config.BufferSize)
}

// attach marks the handoff ring pulled, now that this kernel's player plays,
// and then sets the Device ready at settings. The order is contract: a flush
// that sees the Device ready restates every live voice, and Emit publishes
// nothing to a ring not pulled, so the ring is pulled before any flush can
// see the Device ready.
func (g *deviceGoroutine) attach(settings deviceSettings) {
	g.plugin.mixer.ring.pulled.Store(true)
	g.plugin.status.Store(&sfx.Device{
		Ready:      true,
		Name:       string(PluginName),
		SampleRate: settings.sampleRate,
		Channels:   outputChannels,
		Latency:    settings.bufferSize,
	})
	g.everReady = true
}

// detach gives up a lost device, in an order that is contract: the Device
// goes not ready first, so the flush sees the loss no later than it sees a
// release let go; then the player is stopped, which returns only once it
// has stopped reading the mixer; and only then is the handoff ring detached,
// which lets the flush side free a destroyed clip at once again and stops
// Emit publishing, and emptied, in the mixer's place, so nothing it holds
// keeps a clip alive through the loss or reaches the next player. Detaching
// before the player stopped would claim nothing is mid-copy while something
// still might be. Without the detach, the applied counter stops with the
// device, and every release after the loss is held for as long as it lasts.
func (g *deviceGoroutine) detach() {
	g.plugin.status.Store(&sfx.Device{Name: string(PluginName)})
	g.plugin.device.close()
	g.plugin.mixer.ring.pulled.Store(false)
	g.plugin.mixer.ring.empty()
}

// waitForLoss polls the playing device every DeviceRetry, since oto has
// nothing to push: a device that goes away shows only as an error, to
// whoever asks. It answers true once a poll errors, and false when stop
// ends the goroutine first.
func (g *deviceGoroutine) waitForLoss() bool {
	for g.wait() {
		if g.plugin.device.poll() != nil {
			return true
		}
	}
	return false
}

// wait waits one DeviceRetry, and answers false at once when stop ends the
// goroutine.
func (g *deviceGoroutine) wait() bool {
	timer := time.NewTimer(g.plugin.config.DeviceRetry)
	defer timer.Stop()
	select {
	case <-g.stopping:
		return false
	case <-timer.C:
		return true
	}
}

// reportUnavailable logs, once, as a warning, that no device could be taken
// or started, unless a device has been ready in this kernel: a failure after
// that is a loss, which is reported never.
func (g *deviceGoroutine) reportUnavailable(err error) {
	if g.everReady {
		return
	}
	g.plugin.logger.LogWarnOnce(deviceUnavailableLogKey, "otosound: the game plays in silence", "err", ErrDeviceUnavailable{Err: err})
}

// reportIgnoredConfig logs, once, as an error, that the settings in force
// are not those this kernel's Config asked for, which happens when an
// earlier kernel made the process's context.
func (g *deviceGoroutine) reportIgnoredConfig(settings deviceSettings) {
	config := g.plugin.config
	if settings.sampleRate == config.SampleRate && settings.bufferSize == config.BufferSize {
		return
	}
	g.plugin.logger.LogErrOnce(deviceConfigIgnoredLogKey, ErrDeviceConfigIgnored{
		AskedSampleRate: config.SampleRate,
		SampleRate:      settings.sampleRate,
		AskedBufferSize: config.BufferSize,
		BufferSize:      settings.bufferSize,
	})
}
