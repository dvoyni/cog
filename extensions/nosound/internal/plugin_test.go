package internal

import (
	"errors"
	"io/fs"
	"os"
	"testing"
	"testing/fstest"
	"time"

	"github.com/dvoyni/cog/extensions/nosound"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
	"github.com/dvoyni/cog/slots/sound"
	"github.com/dvoyni/cog/slots/sound/soundplugin"
	"github.com/dvoyni/cog/slots/storage"
	"github.com/dvoyni/cog/slots/storage/storageplugin"
)

// This is the testing story in one file: a test composes nosound, drives ticks
// under a fixed step, and asserts against sound's own tick-computed state and
// its events - never against samples, and never below the seam.
//
// The determinism line is the seam. Above it everything here is deterministic:
// which Voices exist, their playheads, which endings fire and on which tick. It
// stays that way because a playhead advances whether or not anyone can hear it,
// so the timeline is world time and never the Device's. Under nosound
// Device.Ready and Device.Latency are constants by construction, so a test may
// assert them here and nowhere else.

// step is the test's fixed timestep, an exact binary fraction so that a
// playhead accumulated a tick at a time lands where the arithmetic says.
const step = 1.0 / 64

// endsOnTick is the tick the fixture clip's one-shot ends on: its duration
// divided by the step, rounded up. 1.104399 s at 1/64 s a tick is 70.68 ticks,
// so the Voice is still playing after 70 and gone after 71.
const endsOnTick = 71

func TestAOneShotUnderNosoundEndsOnTheTickItsDurationSaysItShould(t *testing.T) {
	h := newHarness(t)

	voice := h.play(sound.ClipWithResource("music/pianoroll.ogg"))

	for tick := 1; tick < endsOnTick; tick++ {
		h.tick()
		got := h.probe(voice)
		if !got.Found {
			t.Fatalf("%v was gone after tick %d, and the clip is %v long", voice, tick, clipDuration)
		}
		if want := float32(float64(tick) * step); got.Info.Playhead != want {
			t.Fatalf("after tick %d the playhead is %v, want %v", tick, got.Info.Playhead, want)
		}
		if got.Info.Duration != clipDuration {
			t.Fatalf("the view says duration %v, want the header's %v", got.Info.Duration, clipDuration)
		}
	}

	h.tick()
	if got := h.probe(voice); got.Found || got.Live != 0 {
		t.Fatalf("after tick %d the view still holds %v", endsOnTick, voice)
	}
	if ended := h.waitEnded(); ended.Voice != voice || ended.Reason != sound.ReasonFinished {
		t.Fatalf("ended as %v/%v, want %v/finished", ended.Voice, ended.Reason, voice)
	}
}

// A Stop ends a Voice on the tick it was recorded, and the ending says who
// ended it - so a subscriber never has to tell "it ended" from "I ended it".
func TestAStoppedVoiceEndsAsStopped(t *testing.T) {
	h := newHarness(t)

	voice := h.play(sound.ClipWithResource("music/pianoroll.ogg"))
	h.tick()
	h.record(func(queue *sound.Queue) { queue.Stop(voice) })
	h.tick()

	if got := h.probe(voice); got.Found {
		t.Fatalf("%v outlived its Stop", voice)
	}
	if ended := h.waitEnded(); ended.Voice != voice || ended.Reason != sound.ReasonStopped {
		t.Fatalf("ended as %v/%v, want %v/stopped", ended.Voice, ended.Reason, voice)
	}
}

// A path that is not there, or bytes that are not Ogg Vorbis, ends the Voices
// recorded against it with ReasonFailed - a tick or more later, never in the
// tick that recorded the play. It is the one failure path a game can exercise
// deliberately, and nosound is what keeps it reachable.
func TestAClipThatIsNotThereEndsItsVoicesAsFailed(t *testing.T) {
	h := newHarnessReporting(t, func(error) {})

	voice := h.play(sound.ClipWithResource("music/missing.ogg"))
	h.tick()
	if got := h.probe(voice); !got.Found {
		t.Fatal("the Voice was gone in the tick that recorded its play")
	}

	h.tick()
	if got := h.probe(voice); got.Found {
		t.Fatalf("%v outlived the tick after its Clip failed", voice)
	}
	if ended := h.waitEnded(); ended.Reason != sound.ReasonFailed {
		t.Fatalf("ended as %v, want failed", ended.Reason)
	}
}

// Under nosound the Device is a constant, so a test may assert it here: Ready
// is true because it is a working Device that plays nothing, and Latency is
// zero because nothing is buffered.
func TestTheSlotReportsNosoundsDevice(t *testing.T) {
	h := newHarness(t)
	h.tick()

	device := h.probe(sound.NoVoice).Device
	if !device.Ready || device.Name != string(nosound.Name) {
		t.Fatalf("sound reports %+v, want a ready nosound", device)
	}
	if device.Latency != 0 {
		t.Fatalf("sound reports a latency of %v under nosound", device.Latency)
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

// probeCmd reads the live view and the Device.
type probeCmd kernel.Command[probeRequest, probeResponse]
type probeRequest struct{ Voice sound.Voice }
type probeResponse struct {
	Live   int
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
			response := probeResponse{Live: live.Len(), Device: *device.Get()}
			response.Info, response.Found = live.Info(request.Voice)
			return response
		}
}

// endedHandler collects every VoiceEndedEvent the flush publishes.
type endedHandler kernel.Subscription[sound.VoiceEndedEvent]

// probePlugin is the test's own plugin, which is what a game already has: its
// Systems lock sound's resources, so reading the view inside one is the game
// reading its own engine.
type probePlugin struct{ ended chan sound.VoiceEndedEvent }

func (probePlugin) Name() kernel.PluginName           { return "nosound-probe-test" }
func (probePlugin) Dependencies() []kernel.PluginName { return []kernel.PluginName{sound.Name} }

func (p probePlugin) Register(registrar *kernel.Registrar, _ any) error {
	registrar.HandleCommand[recordCmd](recordCmdImpl)
	registrar.HandleCommand[probeCmd](probeCmdImpl)
	registrar.Subscribe[endedHandler](p.collectEnded)
	return nil
}

func (p probePlugin) collectEnded() (kernel.Lock, kernel.Observe[sound.VoiceEndedEvent]) {
	return nil, func(_ kernel.Kernel, event sound.VoiceEndedEvent) {
		p.ended <- event
	}
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

// harness is one composed engine: sound over nosound, with storage behind it
// holding the fixture clip. app is not composed - sound subscribes to
// app.UpdateEvent and depends on nobody for it - so the test owns the clock,
// which is what a fixed step means here.
type harness struct {
	t      *testing.T
	kernel kernel.Executioner
	ended  chan sound.VoiceEndedEvent
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	return newHarnessWithHandler(t, func(err error) error {
		t.Errorf("unexpected kernel error: %v", err)
		return err
	})
}

// newHarnessReporting keeps the engine running through a reported error, for
// the paths that report one on purpose.
func newHarnessReporting(t *testing.T, report func(error)) *harness {
	t.Helper()
	return newHarnessWithHandler(t, func(err error) error {
		report(err)
		return nil
	})
}

func newHarnessWithHandler(t *testing.T, handler kernel.ErrorHandler) *harness {
	t.Helper()
	clip, err := os.ReadFile("testdata/pianoroll.ogg")
	if err != nil {
		t.Fatalf("reading the fixture clip: %v", err)
	}
	files := fstest.MapFS{"music/pianoroll.ogg": {Data: clip}}
	ended := make(chan sound.VoiceEndedEvent, 64)
	engine := kernel.New(map[kernel.PluginName]any{
		storage.Name: storage.Config{}.WithReadFS("test", 10, files),
	}).Handler(handler).WithPlugins(
		storageplugin.New(), permanentAdapter{},
		soundplugin.New(), New(),
		probePlugin{ended: ended},
	)
	go engine.Run()
	t.Cleanup(engine.Quit)
	<-engine.Ready()
	return &harness{t: t, kernel: engine.Executioner(), ended: ended}
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

func (h *harness) waitEnded() sound.VoiceEndedEvent {
	h.t.Helper()
	select {
	case event := <-h.ended:
		return event
	case <-time.After(5 * time.Second):
		h.t.Fatal("timed out waiting for a VoiceEndedEvent")
		return sound.VoiceEndedEvent{}
	}
}
