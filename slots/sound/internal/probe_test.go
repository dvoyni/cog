package internal

import (
	"testing"
	"testing/fstest"
	"time"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
	"github.com/dvoyni/cog/slots/sound"
	"github.com/dvoyni/cog/slots/storage"
	"github.com/dvoyni/cog/slots/storage/storageplugin"
)

// Reading the view costs a fixture plugin. A test driving the Engine from
// outside has no Systems of its own, so it writes a probe declaring the locks
// it wants - one command that records into the Queue under a write lock, one
// that reads the Voices and the Device under read locks, and a subscriber that
// collects the endings.
//
// For a game's test this is not ceremony at all: a game already composes its
// own plugin with its own Systems, so reading kernel.Read[*sound.Voices] inside
// one is the game reading its own engine.

// recordCmd records into the queue under its write lock. It carries a callback
// rather than a request per verb, because what a test wants to say is "these
// operations, in this order, in one tick" and a verb-shaped request would make
// each of them a tick of its own.
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

// probeCmd answers what the live view says now.
type probeCmd kernel.Command[probeRequest, probeResponse]
type probeRequest struct {
	Voice sound.Voice
	Bus   sound.Bus
	// Clip is what ClipInfoOf is asked about. It is on the probe rather than a
	// command of its own because a question asked of a resource is exactly what
	// a game's own System asks, holding a read lock beside the others.
	Clip sound.ClipRef
}
type probeResponse struct {
	Live      int
	Info      sound.VoiceInfo
	Found     bool
	All       []sound.VoiceInfo
	Device    sound.Device
	BusVolume float32
	// ListenerAt and ListenerFacing are read from the Listener resource, which
	// is a resource of its own for the reason the Buses are: a System asking
	// where the Listener is never contends with the Systems recording
	// operations.
	ListenerAt     m.Vec3
	ListenerFacing m.Quat
	// ClipInfo and ClipState are what ClipInfoOf answered about request.Clip.
	ClipInfo  sound.ClipInfo
	ClipState sound.State
}

func probeCmdImpl() (kernel.Lock, kernel.Execute[probeRequest, probeResponse]) {
	var voices kernel.Read[*sound.Voices]
	var buses kernel.Read[*sound.Buses]
	var listener kernel.Read[*sound.Listener]
	var device kernel.Read[*sound.Device]
	var clips kernel.Read[*sound.Clips]
	return func(access kernel.ResourceAccess) {
			voices = access.GetRead[*sound.Voices]()
			buses = access.GetRead[*sound.Buses]()
			listener = access.GetRead[*sound.Listener]()
			device = access.GetRead[*sound.Device]()
			clips = access.GetRead[*sound.Clips]()
		}, func(_ kernel.Kernel, request probeRequest) probeResponse {
			live := voices.Get()
			heardFrom := listener.Get()
			response := probeResponse{
				Live:           live.Len(),
				Device:         *device.Get(),
				BusVolume:      buses.Get().Volume(request.Bus),
				ListenerAt:     heardFrom.Position(),
				ListenerFacing: heardFrom.Orientation(),
			}
			response.ClipInfo, response.ClipState = sound.ClipInfoOf(clips, request.Clip)
			response.Info, response.Found = live.Info(request.Voice)
			for info := range live.All() {
				response.All = append(response.All, info)
			}
			return response
		}
}

// endedHandler collects every VoiceEndedEvent the flush publishes.
type endedHandler kernel.Subscription[sound.VoiceEndedEvent]

// probePlugin is the test's own plugin: its Systems are the two commands and
// the ending subscriber.
type probePlugin struct{ ended chan sound.VoiceEndedEvent }

func (probePlugin) Name() kernel.PluginName { return "sound-probe-test" }

// Dependencies names sound, whose resources the probe locks.
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

// soundBackendAdapter fills sound's Backend Port the way an Extension does.
type soundBackendAdapter struct{ backend sound.Backend }

func (soundBackendAdapter) Name() kernel.PluginName           { return "soundbackendtest" }
func (soundBackendAdapter) Dependencies() []kernel.PluginName { return nil }

func (a soundBackendAdapter) Register(registrar *kernel.Registrar, _ any) error {
	registrar.ProvideAdapter[testSoundBackend](a.backend)
	return nil
}

// testSoundBackend is the Adapter this fixture fills sound's Backend Port as.
type testSoundBackend kernel.Adapter[sound.BackendPort]

// step is the test's fixed timestep. It is an exact binary fraction, so a
// playhead accumulated a tick at a time lands on the tick the arithmetic says
// it should rather than a bit either side of it.
const step = 1.0 / 64

// harness is one composed engine plus what a test says to it.
type harness struct {
	t       *testing.T
	kernel  kernel.Executioner
	backend *fakeBackend
	ended   chan sound.VoiceEndedEvent
}

// newHarness composes sound over a fixture Backend, with storage behind it
// because that is where a Clip's bytes come from. app is not composed: sound
// subscribes to app.UpdateEvent and depends on nobody for it, so the test
// publishes the tick itself, which is what "a fixed TimeStep" means when the
// test owns the clock.
func newHarness(t *testing.T, backend *fakeBackend, config sound.Config, files fstest.MapFS) *harness {
	t.Helper()
	return newHarnessWithHandler(t, backend, config, files, func(err error) error {
		t.Errorf("unexpected kernel error: %v", err)
		return err
	})
}

// newHarnessReporting keeps the engine running through a reported error and
// hands it to report instead, for the paths that report one on purpose.
func newHarnessReporting(
	t *testing.T, backend *fakeBackend, files fstest.MapFS, report func(error),
) *harness {
	t.Helper()
	return newHarnessWithHandler(t, backend, sound.Config{}, files, func(err error) error {
		report(err)
		return nil
	})
}

func newHarnessWithHandler(
	t *testing.T, backend *fakeBackend, config sound.Config, files fstest.MapFS, handler kernel.ErrorHandler,
) *harness {
	t.Helper()
	ended := make(chan sound.VoiceEndedEvent, 64)
	engine := kernel.New(map[kernel.PluginName]any{
		storage.Name: storage.Config{}.WithReadFS("test", 10, files),
		sound.Name:   config,
	}).Handler(handler).WithPlugins(
		storageplugin.New(), permanentAdapter{},
		New(), soundBackendAdapter{backend},
		probePlugin{ended: ended},
	)
	go engine.Run()
	t.Cleanup(engine.Quit)
	<-engine.Ready()
	return &harness{t: t, kernel: engine.Executioner(), backend: backend, ended: ended}
}

// record runs one recording under the queue's write lock, which is one tick's
// worth of a game's Systems saying what they want.
func (h *harness) record(record func(*sound.Queue)) {
	h.t.Helper()
	h.kernel.ExecuteCommand[recordCmd](recordRequest{Record: record})
}

// play records one play and hands back its handle, which the queue minted
// before this call returned.
func (h *harness) play(clip sound.ClipRef, offset float32, params sound.Params) sound.Voice {
	h.t.Helper()
	var voice sound.Voice
	h.record(func(queue *sound.Queue) { voice = queue.Play(clip, offset, params) })
	return voice
}

// tick publishes one fixed step and waits for every subscriber, so the flush
// has run and released its locks by the time it returns.
func (h *harness) tick() {
	h.t.Helper()
	h.kernel.PublishEvent(app.UpdateEvent{Dt: step, Last: true}).Wait()
}

// pause publishes one engine Pause change and waits for every subscriber, so
// sound has suspended or resumed by the time it returns. It is published by
// hand for the reason the tick is: sound subscribes to app and depends on
// nobody for it, so the test owns the pause as it owns the clock.
func (h *harness) pause(paused bool) {
	h.t.Helper()
	h.kernel.PublishEvent(app.PauseChangeEvent{Paused: paused}).Wait()
}

// probe reads the live view.
func (h *harness) probe(voice sound.Voice) probeResponse {
	h.t.Helper()
	return h.kernel.ExecuteCommand[probeCmd](probeRequest{Voice: voice})
}

// askClip reads what sound knows about a Clip, under the read lock a game's own
// System would hold. It starts nothing, which is half of what it is here to
// prove.
func (h *harness) askClip(clip sound.ClipRef) (sound.ClipInfo, sound.State) {
	h.t.Helper()
	response := h.kernel.ExecuteCommand[probeCmd](probeRequest{Clip: clip})
	return response.ClipInfo, response.ClipState
}

// waitEnded takes the next ending, or fails. VoiceEndedEvent is published from
// inside the flush and outlives it, so a test waits for it rather than reading
// it off the tick that caused it - and asserts the tick itself against the live
// view, which is synchronous.
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

// noEnding fails when an ending is already waiting. It reads what has arrived
// rather than waiting for what has not, which is the only honest thing to
// assert about an event that should never come.
func (h *harness) noEnding() {
	h.t.Helper()
	select {
	case event := <-h.ended:
		h.t.Fatalf("%v ended as %v, and nothing should have ended", event.Voice, event.Reason)
	default:
	}
}
