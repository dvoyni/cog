//go:build !js

package internal

import (
	"errors"
	"io/fs"
	"math"
	"os"
	"testing"
	"testing/fstest"
	"time"

	"github.com/dvoyni/cog/extensions/otosound"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
	"github.com/dvoyni/cog/slots/sound"
	"github.com/dvoyni/cog/slots/sound/soundplugin"
	"github.com/dvoyni/cog/slots/storage"
	"github.com/dvoyni/cog/slots/storage/storageplugin"
)

// step is the harness's fixed timestep, an exact binary fraction so that a
// playhead accumulated a tick at a time lands where the arithmetic says.
const step = 1.0 / 64

func TestRegisterRefusesAConfigThatIsNotOne(t *testing.T) {
	p := &plugin{hardware: &fakeAudio{}}
	err := p.Register(&kernel.Registrar{}, "48000")

	var refused otosound.ErrInvalidConfig
	if !errors.As(err, &refused) {
		t.Fatalf("Register accepted a %T: err = %v", "48000", err)
	}
}

func TestRegisterRefusesANegativeRateOrBuffer(t *testing.T) {
	var rate otosound.ErrInvalidSampleRate
	if err := (&plugin{}).Register(&kernel.Registrar{}, otosound.Config{SampleRate: -1}); !errors.As(err, &rate) {
		t.Fatalf("Register accepted a negative SampleRate: err = %v", err)
	}
	var buffer otosound.ErrInvalidBufferSize
	if err := (&plugin{}).Register(&kernel.Registrar{}, otosound.Config{BufferSize: -1}); !errors.As(err, &buffer) {
		t.Fatalf("Register accepted a negative BufferSize: err = %v", err)
	}
}

// The whole machine, end to end: a game plays a real Ogg, storage reads it,
// the prepare goroutine decodes it and converts it to the device rate, Install
// mints an id, the flush Emits a start, the ring carries it across, and the
// Mixer produces samples a device would have been handed.
//
// What this cannot assert is that those samples sound right, which needs ears.
// What it can assert is that they are there, correctly shaped, not silent and
// not clipping - and that is what makes the by-ear check a check rather than a
// hunt for a build that produces nothing at all.
func TestAGamePlayingARealOggProducesSamplesForTheDevice(t *testing.T) {
	h := newHarness(t)

	h.play(sound.ClipWithResource("music/pianoroll.ogg"))
	h.tickUntilPlaying(t)

	// A second of it, which is most of the fixture: the clip opens quietly, and
	// four blocks of a piano roll's first 40ms would be a measurement of the
	// fade-in rather than of the Mixer.
	out := render(t, h.plugin.backend.mixer, 100)

	var peak float32
	var leftEnergy, rightEnergy float64
	for i, sample := range out {
		if sample > 1 || sample < -1 {
			t.Fatalf("sample %d is %v, which is outside what the device accepts", i, sample)
		}
		if abs := float32(math.Abs(float64(sample))); abs > peak {
			peak = abs
		}
		if i%outChannels == 0 {
			leftEnergy += float64(sample) * float64(sample)
		} else {
			rightEnergy += float64(sample) * float64(sample)
		}
	}
	if peak < 0.01 {
		t.Fatalf("a second of a real Ogg peaked at %v, which is silence", peak)
	}
	if leftEnergy == 0 || rightEnergy == 0 {
		t.Fatalf("one output channel is empty: left %v, right %v", leftEnergy, rightEnergy)
	}
}

// A Device that could never be opened is reported once, and the game runs on in
// silence rather than failing to start. Registration never fails on a Device,
// which is gfx's shape one Slot over.
func TestADeviceThatCanNotBeOpenedIsReportedOnceAndTheGameRunsOn(t *testing.T) {
	reported := make(chan error, 8)
	h := newHarnessWith(t, &fakeAudio{openErr: errors.New("no audio endpoint")}, func(err error) error {
		reported <- err
		return nil
	})

	voice := h.play(sound.ClipWithResource("music/pianoroll.ogg"))
	for range 8 {
		h.tick()
	}

	if len(reported) != 1 {
		t.Fatalf("a Device that could never be opened was reported %d times", len(reported))
	}
	var unavailable otosound.ErrDeviceUnavailable
	if err := <-reported; !errors.As(err, &unavailable) {
		t.Fatalf("what was reported is %v", err)
	}
	if got := h.probe(voice); !got.Found {
		t.Fatal("the Voice did not survive a Device that never opened")
	}
	if h.probe(voice).Device.Ready {
		t.Fatal("sound reports a ready Device with nothing open")
	}
}

// probeCmd reads the live view and the Device.
type probeCmd kernel.Command[probeRequest, probeResponse]
type probeRequest struct{ Voice sound.Voice }
type probeResponse struct {
	Info   sound.VoiceInfo
	Found  bool
	Device sound.Device
}

func probeCmdImpl() (kernel.Lock, kernel.Execute[probeRequest, probeResponse]) {
	var voices kernel.Read[*sound.Voices]
	var device kernel.Read[*sound.Device]
	return func(access kernel.ResourceAccess) {
			voices = access.GetRead[*sound.Voices]()
			device = access.GetRead[*sound.Device]()
		}, func(_ kernel.Kernel, request probeRequest) probeResponse {
			live := voices.Get()
			response := probeResponse{Device: *device.Get()}
			response.Info, response.Found = live.Info(request.Voice)
			return response
		}
}

// recordCmd records into the queue under its write lock, one tick's worth of
// what a game's Systems would say.
type recordCmd kernel.Command[recordRequest, recordResponse]
type recordRequest struct{ Record func(*sound.Queue) }
type recordResponse struct{}

func recordCmdImpl() (kernel.Lock, kernel.Execute[recordRequest, recordResponse]) {
	var queue kernel.Write[*sound.Queue]
	return func(access kernel.ResourceAccess) {
			queue = access.GetWrite[*sound.Queue]()
		}, func(_ kernel.Kernel, request recordRequest) recordResponse {
			request.Record(queue.Get())
			return recordResponse{}
		}
}

// probePlugin is the test's own plugin, which is what a game already has: its
// Systems lock sound's resources, so reading the view inside one is the game
// reading its own engine.
type probePlugin struct{}

func (probePlugin) Name() kernel.PluginName           { return "otosound-probe-test" }
func (probePlugin) Dependencies() []kernel.PluginName { return []kernel.PluginName{sound.Name} }

func (probePlugin) Register(registrar *kernel.Registrar, _ any) error {
	registrar.HandleCommand[recordCmd](recordCmdImpl)
	registrar.HandleCommand[probeCmd](probeCmdImpl)
	return nil
}

// permanentAdapter provides storage's PermanentFS Adapter: an empty filesystem
// that reads nothing and refuses writes, since nothing here persists anything.
type permanentAdapter struct{}

func (permanentAdapter) Name() kernel.PluginName           { return "test-permanent-fs" }
func (permanentAdapter) Dependencies() []kernel.PluginName { return nil }
func (permanentAdapter) Register(registrar *kernel.Registrar, _ any) error {
	registrar.ProvideAdapter[testPermanentFS](storage.PermanentFS(emptyPermanentFS{}))
	return nil
}

// testPermanentFS is the Adapter this fixture fills storage's permanent
// filesystem Port as.
type testPermanentFS kernel.Adapter[storage.PermanentFSPort]

type emptyPermanentFS struct{ fstest.MapFS }

func (emptyPermanentFS) WriteFile(string, []byte, fs.FileMode) error { return errors.ErrUnsupported }
func (emptyPermanentFS) MkdirAll(string, fs.FileMode) error          { return errors.ErrUnsupported }
func (emptyPermanentFS) Remove(string) error                         { return errors.ErrUnsupported }
func (emptyPermanentFS) Rename(string, string) error                 { return errors.ErrUnsupported }

// harness is one composed engine: sound over otosound, with storage behind it
// holding the fixture clip and a device that opens nothing. app is not composed
// - sound subscribes to app.UpdateEvent and depends on nobody for it - so the
// test owns the clock.
type harness struct {
	t      *testing.T
	kernel kernel.Executioner
	plugin *plugin
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	return newHarnessWith(t, &fakeAudio{}, func(err error) error {
		t.Errorf("unexpected kernel error: %v", err)
		return err
	})
}

func newHarnessWith(t *testing.T, hardware *fakeAudio, handler kernel.ErrorHandler) *harness {
	t.Helper()
	clip, err := os.ReadFile("testdata/pianoroll.ogg")
	if err != nil {
		t.Fatalf("reading the fixture clip: %v", err)
	}
	files := fstest.MapFS{"music/pianoroll.ogg": {Data: clip}}
	sink := &plugin{hardware: hardware}
	engine := kernel.New(map[kernel.PluginName]any{
		storage.Name: storage.Config{}.WithReadFS("test", 10, files),
	}).Handler(handler).WithPlugins(
		storageplugin.New(), permanentAdapter{},
		soundplugin.New(), sink,
		probePlugin{},
	)
	go engine.Run()
	t.Cleanup(engine.Quit)
	<-engine.Ready()
	return &harness{t: t, kernel: engine.Executioner(), plugin: sink}
}

func (h *harness) record(record func(*sound.Queue)) {
	h.t.Helper()
	h.kernel.ExecuteCommand[recordCmd](recordRequest{Record: record})
}

func (h *harness) play(clip sound.ClipRef) sound.Voice {
	h.t.Helper()
	var voice sound.Voice
	h.record(func(queue *sound.Queue) { voice = queue.Play(clip, 0, sound.Params{}) })
	return voice
}

func (h *harness) tick() {
	h.t.Helper()
	h.kernel.PublishEvent(app.UpdateEvent{Dt: step, Last: true}).Wait()
}

func (h *harness) probe(voice sound.Voice) probeResponse {
	h.t.Helper()
	return h.kernel.ExecuteCommand[probeCmd](probeRequest{Voice: voice})
}

// tickUntilPlaying drives ticks until the prepare goroutine has finished and
// the flush has bound the Clip, which is a play before its Clip is ready doing
// exactly what the spec says: the Voice exists, addressable and silent, and
// starts when the Clip installs.
func (h *harness) tickUntilPlaying(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		h.tick()
		if len(h.plugin.backend.clips) > 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("the Clip never became resident")
}
