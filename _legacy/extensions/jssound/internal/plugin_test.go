//go:build js

package internal

import (
	"errors"
	"io/fs"
	"os"
	"testing"
	"testing/fstest"
	"time"

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
	fakeAudio(t)
	err := (&plugin{}).Register(&kernel.Registrar{}, "interactive")

	var refused ErrInvalidConfig
	if !errors.As(err, &refused) {
		t.Fatalf("Register accepted a %T: err = %v", "interactive", err)
	}
}

func TestRegisterRefusesANegativeLatencyHint(t *testing.T) {
	fakeAudio(t)
	var refused ErrInvalidLatencyHint
	if err := (&plugin{}).Register(&kernel.Registrar{}, Config{LatencyHint: -1}); !errors.As(err, &refused) {
		t.Fatalf("Register accepted a negative LatencyHint: err = %v", err)
	}
}

// The whole machine, end to end: a game plays a real Ogg, storage reads it, the
// Adapter parses its headers and hands the bytes to the browser, the decode
// callback lands, the flush drains it, Install mints an id and the next flush
// Emits a start that becomes an AudioBufferSourceNode on the destination.
//
// What this cannot assert is that the result sounds right, which needs ears and
// a browser. What it can assert is that the graph is there, wired the way the
// spec describes, and carrying the matrix sound computed - which is what makes a
// by-ear check a check rather than a hunt for a build that produces nothing.
func TestAGamePlayingARealOggBuildsTheNodeGraph(t *testing.T) {
	h := newHarness(t)

	voice := h.play(sound.ClipWithResource("music/pianoroll.ogg"))
	h.tickUntilPlaying(t)

	sources := h.audio.sources()
	if len(sources) != 1 {
		t.Fatalf("the play made %d source nodes, want 1", len(sources))
	}
	if !sources[0].Get("started").Truthy() {
		t.Fatal("the source node was never started")
	}
	if h.audio.panners() != 0 {
		t.Fatalf("%d PannerNodes were made, and the arithmetic is sound's", h.audio.panners())
	}
	// A non-positional stereo Voice at full volume is the identity: left to
	// left, right to right. What matters here is that it came from sound rather
	// than from a default, so the diagonal is asserted and the shape of the
	// pan is left to sound's own W3C tests.
	if got := h.audio.matrix(); got[0][0] <= 0 || got[1][1] <= 0 {
		t.Fatalf("the graph carries %v, want sound's own matrix on the diagonal", got)
	}
	if info := h.probe(voice); !info.Found {
		t.Fatal("the Voice is not in sound's live view")
	}
}

// Device.Ready reaches the game through sound's own resource, false until the
// player's gesture resumes the context and true on the tick after it - which is
// what a web game's click-to-enable prompt is gated on.
func TestTheGameSeesTheDeviceBecomeReadyOnTheGesture(t *testing.T) {
	h := newHarness(t)
	h.tick()

	if h.device().Ready {
		t.Fatal("sound reports a ready Device before any gesture")
	}
	h.audio.gesture("pointerdown")
	h.tick()

	device := h.device()
	if !device.Ready {
		t.Fatal("sound still reports an unready Device after the gesture resumed the context")
	}
	if device.Name != string(Name) {
		t.Fatalf("the Device names %q, want %q", device.Name, Name)
	}
}

// A page with no Web Audio at all is reported once, through the Adapter's own
// subscription, and the game runs on in silence rather than failing to start.
// Registration never fails on a Device, which is gfx's shape one Slot over.
func TestAPageWithNoWebAudioIsReportedOnceAndTheGameRunsOn(t *testing.T) {
	noWebAudio(t)
	reported := make(chan error, 8)
	h := compose(t, nil, fixtureBytes(t), func(err error) error {
		reported <- err
		return nil
	})

	voice := h.play(sound.ClipWithResource("music/pianoroll.ogg"))
	for range 8 {
		h.tick()
	}

	if len(reported) != 1 {
		t.Fatalf("a page with no Web Audio was reported %d times", len(reported))
	}
	var unavailable ErrDeviceUnavailable
	if err := <-reported; !errors.As(err, &unavailable) {
		t.Fatalf("what was reported is %v", err)
	}
	if !h.probe(voice).Found {
		t.Fatal("the Voice did not survive a page with no Web Audio")
	}
	if h.device().Ready {
		t.Fatal("sound reports a ready Device with no Web Audio at all")
	}
}

// A Clip whose loop tags are broken is said out loud once, through the same
// subscription, so a game's suite learns its music file is mistagged - and the
// Clip still plays, looping whole.
func TestABrokenLoopRegionIsReportedOnceThroughTheSubscription(t *testing.T) {
	reported := make(chan error, 8)
	h := newHarnessWith(t, mistaggedFixture(t), func(err error) error {
		reported <- err
		return nil
	})

	h.play(sound.ClipWithResource("music/pianoroll.ogg"))
	h.tickUntilPlaying(t)
	for range 4 {
		h.tick()
	}

	if len(reported) != 1 {
		t.Fatalf("a Clip with a broken Loop Region was reported %d times, want once", len(reported))
	}
	var ignored ErrLoopRegionIgnored
	if err := <-reported; !errors.As(err, &ignored) {
		t.Fatalf("what was reported is %v", err)
	}
}

// mistaggedFixture is the fixture with a loop region that is not inside it.
func mistaggedFixture(t *testing.T) []byte {
	t.Helper()
	return retagged(t, fixture(t), "LOOPSTART=11025", "LOOPEND=48705").Data()
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

func (probePlugin) Name() kernel.PluginName           { return "jssound-probe-test" }
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

// readMountAdapter contributes one read mount through storage's Port, standing
// in for the game plugin that would contribute its assets.
type readMountAdapter struct{ mount storage.ReadMount }

func (readMountAdapter) Name() kernel.PluginName           { return "test-read-mount" }
func (readMountAdapter) Dependencies() []kernel.PluginName { return nil }
func (a readMountAdapter) Register(registrar *kernel.Registrar, _ any) error {
	registrar.ProvideAdapter[testReadMount](a.mount)
	return nil
}

// testReadMount is the Adapter readMountAdapter contributes its mount as.
type testReadMount kernel.Adapter[storage.ReadMountPort]

type emptyPermanentFS struct{ fstest.MapFS }

func (emptyPermanentFS) WriteFile(string, []byte, fs.FileMode) error { return errors.ErrUnsupported }
func (emptyPermanentFS) MkdirAll(string, fs.FileMode) error          { return errors.ErrUnsupported }
func (emptyPermanentFS) Remove(string) error                         { return errors.ErrUnsupported }
func (emptyPermanentFS) Rename(string, string) error                 { return errors.ErrUnsupported }

// harness is one composed engine: sound over jssound, with storage behind it
// holding the fixture clip and a fake AudioContext on the global object. app is
// not composed - sound subscribes to app.UpdateEvent and depends on nobody for
// it - so the test owns the clock.
type harness struct {
	t      *testing.T
	kernel kernel.Executioner
	plugin *plugin
	audio  *audioFake
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	return compose(t, fakeAudio(t), fixtureBytes(t), func(err error) error {
		t.Errorf("unexpected kernel error: %v", err)
		return err
	})
}

// newHarnessWith composes the engine over whatever bytes the fixture path should
// hold, with the fake AudioContext installed.
func newHarnessWith(t *testing.T, clip []byte, handler kernel.ErrorHandler) *harness {
	t.Helper()
	if clip == nil {
		clip = fixtureBytes(t)
	}
	return compose(t, fakeAudio(t), clip, handler)
}

func fixtureBytes(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/pianoroll.ogg")
	if err != nil {
		t.Fatalf("reading the fixture clip: %v", err)
	}
	return data
}

// compose is the engine itself. audio is nil where the test has installed no
// AudioContext at all, which is the page jssound has to behave as nosound on.
func compose(t *testing.T, audio *audioFake, clip []byte, handler kernel.ErrorHandler) *harness {
	t.Helper()
	files := fstest.MapFS{"music/pianoroll.ogg": {Data: clip}}
	sink := &plugin{}
	engine := kernel.New(nil).Handler(handler).WithPlugins(
		storageplugin.New(), permanentAdapter{}, readMountAdapter{storage.ReadMount{Id: "test", Priority: 10, FS: files}},
		soundplugin.New(), sink,
		probePlugin{},
	)
	go engine.Run()
	t.Cleanup(engine.Quit)
	<-engine.Ready()
	return &harness{t: t, kernel: engine.Executioner(), plugin: sink, audio: audio}
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

func (h *harness) device() sound.Device {
	h.t.Helper()
	return h.probe(0).Device
}

// tickUntilPlaying drives ticks, settling whatever decode the browser has been
// asked for between them, until the Clip is resident and its start has been
// emitted. That is a play before its Clip is ready doing exactly what the spec
// says: the Voice exists, addressable and silent, and starts when the Clip
// installs.
//
// The gesture goes in first, because a suspended context is the ordinary first
// state of a page and a test that never clicked would be testing that rather
// than the graph.
func (h *harness) tickUntilPlaying(t *testing.T) {
	t.Helper()
	h.audio.gesture("pointerdown")
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		h.audio.settle(true)
		h.tick()
		if len(h.plugin.backend.clips) > 0 && len(h.audio.sources()) > 0 {
			return
		}
		h.audio.advance(step)
	}
	t.Fatal("the Clip never became resident")
}
