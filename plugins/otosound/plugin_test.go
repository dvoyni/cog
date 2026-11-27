//go:build !js

package otosound

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/sfx"
	"github.com/dvoyni/cog/plugins/sfx/sfxtest"
)

// spawnOverFake answers a harness whose kernel composes sfx and otosound over
// device, otosound binding sfx's socket, and otosound's plugin in it.
func spawnOverFake(t testing.TB, device *fakeDevice) (*sfxtest.Harness, *Plugin) {
	t.Helper()
	plugin := NewPlugin()
	plugin.device = device
	h := sfxtest.NewHarness(t)
	h.UsePluginBackend()
	h.Spawn(nil, plugin)
	return h, plugin
}

// waitForReady answers plugin's Device once it is ready, and fails the test
// when it is not ready within sfxtest.Timeout.
func waitForReady(t testing.TB, plugin *Plugin) sfx.Device {
	t.Helper()
	deadline := time.Now().Add(sfxtest.Timeout)
	for {
		device := backend{plugin: plugin}.Device()
		if device.Ready {
			return device
		}
		if time.Now().After(deadline) {
			t.Fatalf("the device is %+v after %v, want it ready", device, sfxtest.Timeout)
		}
		time.Sleep(time.Millisecond)
	}
}

// wantSilence fails the test unless a block read from device's player is
// silence, every byte of it read.
func wantSilence(t testing.TB, device *fakeDevice) {
	t.Helper()
	source := device.playing()
	if source == nil {
		t.Fatal("no player plays")
	}
	block := slices.Repeat([]byte{0xff}, 480*bytesPerFrame)
	read, err := source.Read(block)
	if err != nil || read != len(block) {
		t.Fatalf("the player read %d bytes and %v, want %d and no error", read, err, len(block))
	}
	if index := slices.IndexFunc(block, func(b byte) bool { return b != 0 }); index >= 0 {
		t.Fatalf("byte %d of the block is %#x, want silence", index, block[index])
	}
}

// spawnWithConfig spawns, through a supervisor of its own, platform, sfx and
// plugin over a fake device, with config under PluginName, and answers once
// the spawn has returned.
func spawnWithConfig(t testing.TB, plugin *Plugin, config any) {
	t.Helper()
	plugin.device = &fakeDevice{}
	supervisor := kernel.NewSupervisor(nil)
	t.Cleanup(func() {
		supervisor.Quit()
		<-supervisor.Done()
	})
	if _, err := supervisor.Spawn(map[kernel.PluginName]any{PluginName: config}, 1, platform.NewPlugin(), sfx.NewPlugin(), plugin); err != nil {
		t.Fatal(err)
	}
}

// wantInitPanic fails the test unless spawning otosound with config panics
// naming want.
func wantInitPanic(t *testing.T, config any, want string) {
	t.Helper()
	defer func() {
		if recovered := recover(); recovered == nil || !strings.Contains(fmt.Sprint(recovered), want) {
			t.Fatalf("spawning with %#v panicked with %v, want a panic naming %q", config, recovered, want)
		}
	}()
	spawnWithConfig(t, NewPlugin(), config)
}

// sfx's Start opens the device through Voices; the player plays silence while
// nothing sounds, and the Device is ready, named otosound, stereo, at the
// rate and buffer size in force.
func TestAKernelComposingSfxAndOtosoundOpensAPlayerAndReportsAReadyDevice(t *testing.T) {
	device := &fakeDevice{}
	h, plugin := spawnOverFake(t, device)
	h.Step()

	got := waitForReady(t, plugin)
	want := sfx.Device{Ready: true, Name: "otosound", SampleRate: 48000, Channels: 2, Latency: 10 * time.Millisecond}
	if got != want {
		t.Errorf("the device is %+v, want %+v", got, want)
	}
	if opens, plays, closes := device.counts(); opens != 1 || plays != 1 || closes != 0 {
		t.Errorf("the device was opened %d times, played %d and closed %d, want 1, 1 and 0", opens, plays, closes)
	}
	wantSilence(t, device)
	h.Step()
}

// The Device is not ready, and named otosound, until the player plays: the
// game steps meanwhile, since the player starts on the device goroutine.
func TestTheDeviceIsNotReadyUntilThePlayerPlays(t *testing.T) {
	contextReady := make(chan struct{})
	device := &fakeDevice{contextReady: contextReady}
	h, plugin := spawnOverFake(t, device)
	h.Step()
	if got, want := (backend{plugin: plugin}).Device(), (sfx.Device{Name: "otosound"}); got != want {
		t.Errorf("the device before the context is ready is %+v, want %+v", got, want)
	}
	if device.playing() != nil {
		t.Error("a player plays before the context is ready")
	}

	close(contextReady)
	waitForReady(t, plugin)
	wantSilence(t, device)
}

// A second kernel takes its own player on the process's context, and both
// answer the settings the context's open answers in force, here those of a
// context made earlier in the process, not those they asked for. Destroying
// one closes its player alone: the other plays on.
func TestASecondKernelTakesItsOwnPlayerAndDestroyClosesOnlyIt(t *testing.T) {
	settingsInForce := deviceSettings{sampleRate: 44100, bufferSize: 20 * time.Millisecond}
	want := sfx.Device{Ready: true, Name: "otosound", SampleRate: settingsInForce.sampleRate, Channels: 2, Latency: settingsInForce.bufferSize}
	firstDevice := &fakeDevice{settled: settingsInForce}
	secondDevice := &fakeDevice{settled: settingsInForce}
	h, firstPlugin := spawnOverFake(t, firstDevice)
	if got := waitForReady(t, firstPlugin); got != want {
		t.Errorf("the first kernel's device is %+v, want %+v", got, want)
	}

	t.Run("the second kernel, destroyed as it ends", func(t *testing.T) {
		secondHarness, secondPlugin := spawnOverFake(t, secondDevice)
		secondHarness.Step()
		if got := waitForReady(t, secondPlugin); got != want {
			t.Errorf("the second kernel's device is %+v, want %+v", got, want)
		}
		wantSilence(t, secondDevice)
	})

	if _, plays, closes := secondDevice.counts(); plays != 1 || closes != 1 {
		t.Errorf("the destroyed kernel's player was played %d times and closed %d, want 1 and 1", plays, closes)
	}
	if _, plays, closes := firstDevice.counts(); plays != 1 || closes != 0 {
		t.Errorf("the live kernel's player was played %d times and closed %d, want 1 and 0", plays, closes)
	}
	h.Step()
	if got := waitForReady(t, firstPlugin); got != want {
		t.Errorf("the live kernel's device is %+v, want %+v", got, want)
	}
	wantSilence(t, firstDevice)
}

// Each zero field of Config takes the default its comment names, and a field
// given keeps its value, DecodedClipLimit's sentinels included.
func TestEachZeroConfigFieldTakesItsStatedDefault(t *testing.T) {
	defaultConfig := Config{
		SampleRate:       48000,
		BufferSize:       10 * time.Millisecond,
		DecodedClipLimit: 512 << 10,
		RingDepth:        8,
		ReadAhead:        200 * time.Millisecond,
		RefillWait:       5 * time.Millisecond,
		DeviceRetry:      time.Second,
	}
	everyFieldConfig := Config{SampleRate: 44100, BufferSize: time.Millisecond, DecodedClipLimit: 1, RingDepth: 2, ReadAhead: 3 * time.Millisecond, RefillWait: 4 * time.Millisecond, DeviceRetry: 5 * time.Millisecond}
	noLimitConfig := defaultConfig
	noLimitConfig.DecodedClipLimit = -1
	alwaysStreamConfig := defaultConfig
	alwaysStreamConfig.DecodedClipLimit = -2
	for name, test := range map[string]struct{ given, want Config }{
		"a zero config":         {Config{}, defaultConfig},
		"every field given":     {everyFieldConfig, everyFieldConfig},
		"no decoded clip limit": {Config{DecodedClipLimit: -1}, noLimitConfig},
		"always stream":         {Config{DecodedClipLimit: -2}, alwaysStreamConfig},
	} {
		t.Run(name, func(t *testing.T) {
			config := test.given
			plugin := NewPlugin()
			spawnWithConfig(t, plugin, &config)
			if plugin.config != test.want {
				t.Errorf("the config in force is %+v, want %+v", plugin.config, test.want)
			}
			if config != test.given {
				t.Errorf("Init wrote the caller's config: it is %+v, want %+v", config, test.given)
			}
		})
	}
	t.Run("no config", func(t *testing.T) {
		plugin := NewPlugin()
		spawnWithConfig(t, plugin, nil)
		if plugin.config != defaultConfig {
			t.Errorf("the config in force is %+v, want %+v", plugin.config, defaultConfig)
		}
	})
}

// A negative value in a field that takes none is a composition error, and
// Init panics naming the field.
func TestANegativeConfigFieldPanicsNamingIt(t *testing.T) {
	for field, config := range map[string]*Config{
		"SampleRate":  {SampleRate: -1},
		"BufferSize":  {BufferSize: -1},
		"RingDepth":   {RingDepth: -1},
		"ReadAhead":   {ReadAhead: -1},
		"RefillWait":  {RefillWait: -1},
		"DeviceRetry": {DeviceRetry: -1},
	} {
		t.Run(field, func(t *testing.T) { wantInitPanic(t, config, "Config."+field) })
	}
}

// A DecodedClipLimit below -2 names no tier, and Init panics naming it; -1
// and -2 are its sentinels, and do not.
func TestADecodedClipLimitBelowMinusTwoPanics(t *testing.T) {
	wantInitPanic(t, &Config{DecodedClipLimit: -3}, "Config.DecodedClipLimit")
}

// A config of another type than *Config, a Config by value included, panics
// in Init.
func TestAConfigThatIsNotAPointerToConfigPanics(t *testing.T) {
	wantInitPanic(t, Config{}, "*otosound.Config")
}
