package internal

import (
	"os"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecs/ecsplugin"
	"github.com/dvoyni/cog/extensions/nosound/nosoundplugin"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/assets"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
	"github.com/dvoyni/cog/slots/sound"
	"github.com/dvoyni/cog/slots/sound/soundplugin"
	"github.com/dvoyni/cog/slots/storage"
	"github.com/dvoyni/cog/slots/storage/storageplugin"
)

// Everything here runs against a real kernel.Engine with the real ecs and the
// real sound Slot composed beside the binding, over nosound. The binding is
// judged by what reaches sound - the live Voice view, the Listener and
// VoiceEndedEvent - and never by what its System looks like, because the
// assertions a game would write are the same assertions whichever face recorded
// the operation.
//
// What is asserted here rather than in sound's own tests is the correspondence,
// which is this package's whole job.

// step is the test's fixed timestep, an exact binary fraction so that a
// playhead accumulated a tick at a time lands where the arithmetic says.
const step = 1.0 / 64

// clip is the fixture Clip's path, and endsOnTick the tick its one-shot ends
// on: 1.104399 s at a sixty-fourth of a second a tick is 70.68 ticks, so the
// Voice is still playing after 70 and gone after 71.
const (
	clip        = "music/pianoroll.ogg"
	endsOnTick  = 71
	otherClip   = "music/other.ogg"
	missingClip = "music/missing.ogg"
)

// marker is a Tag the game owns, so an Entity can be spawned carrying nothing
// of the binding's and given the binding's Components one at a time - which is
// the difference the reconcile has to see, since absent is not zero.
type marker struct{}

type marked struct{ Marker marker }

// spawnCmd creates one Entity carrying whichever of the binding's Components
// the request names. It is a System registered as a command, which is how a
// test reaches a structural change from outside a tick.
type spawnCmd kernel.Command[spawnRequest, spawnResponse]

type spawnRequest struct {
	Emitter  *Emitter
	Place    *m.Transform
	Listener bool
}

type spawnResponse struct{ Entity ecs.Entity }

func spawnCmdImpl(registrar *kernel.Registrar) func() (kernel.Lock, kernel.Execute[spawnRequest, spawnResponse]) {
	return ecs.ToExecute[spawnRequest, spawnResponse](registrar, func(
		request spawnRequest,
		spawn *ecs.Spawn[marked],
		emitters *ecs.Set[Emitter],
		places *ecs.Set[m.Transform],
		listeners *ecs.Set[Listener],
		answer *ecs.Resp[spawnResponse],
	) {
		e := spawn.New(marked{})
		if request.Emitter != nil {
			emitters.UpdateFor(e, *request.Emitter)
		}
		if request.Place != nil {
			places.UpdateFor(e, *request.Place)
		}
		if request.Listener {
			listeners.UpdateFor(e, Listener{})
		}
		answer.Set(spawnResponse{Entity: e})
	})
}

// changeCmd is every non-structural edit a game makes to an Entity the binding
// watches: setting or removing an Emitter, and setting or removing a Transform.
type changeCmd kernel.Command[changeRequest, changeResponse]

type changeRequest struct {
	Entity ecs.Entity

	Emitter       *Emitter
	RemoveEmitter bool
	Place         *m.Transform
	RemovePlace   bool
}

type changeResponse struct{}

func changeCmdImpl(registrar *kernel.Registrar) func() (kernel.Lock, kernel.Execute[changeRequest, changeResponse]) {
	return ecs.ToExecute[changeRequest, changeResponse](registrar, func(
		request changeRequest,
		emitters *ecs.Set[Emitter],
		dropEmitters *ecs.Remove[Emitter],
		places *ecs.Set[m.Transform],
		dropPlaces *ecs.Remove[m.Transform],
	) {
		if request.Emitter != nil {
			emitters.UpdateFor(request.Entity, *request.Emitter)
		}
		if request.RemoveEmitter {
			dropEmitters.From(request.Entity)
		}
		if request.Place != nil {
			places.UpdateFor(request.Entity, *request.Place)
		}
		if request.RemovePlace {
			dropPlaces.From(request.Entity)
		}
	})
}

// despawnCmd retires one Entity, which is the case the whole despawn section is
// about: the Entity stops matching the query, and the Voice dies with it.
type despawnCmd kernel.Command[despawnRequest, despawnResponse]

type despawnRequest struct{ Entity ecs.Entity }

type despawnResponse struct{}

func despawnCmdImpl(registrar *kernel.Registrar) func() (kernel.Lock, kernel.Execute[despawnRequest, despawnResponse]) {
	return ecs.ToExecute[despawnRequest, despawnResponse](registrar, func(
		request despawnRequest, world *ecs.WriteableEntities,
	) {
		world.Despawn(request.Entity)
	})
}

// probeCmd reads back everything a test asserts on in one lock: sound's live
// Voice view, sound's Listener, and the binding's own table - which the test
// reads because "an entry outlives its Voice" is a claim about the table and
// not only about what is audible.
type probeCmd kernel.Command[probeRequest, probeResponse]

type probeRequest struct {
	Entity ecs.Entity
	Voice  sound.Voice
}

type probeResponse struct {
	// Live is how many Voices sound holds, which is what says whether anything
	// restarted.
	Live  int
	Info  sound.VoiceInfo
	Found bool

	// Entries is how many Entities the binding holds a correspondence for, and
	// Entry is the one the request named.
	Entries int
	Entry   entry
	Held    bool

	Listener sound.ListenerParams
}

func probeCmdImpl(registrar *kernel.Registrar) func() (kernel.Lock, kernel.Execute[probeRequest, probeResponse]) {
	return ecs.ToExecute[probeRequest, probeResponse](registrar, func(
		request probeRequest,
		voices *ecs.Read[*sound.Voices],
		listener *ecs.Read[*sound.Listener],
		correspondence *ecs.Read[*table],
		answer *ecs.Resp[probeResponse],
	) {
		live, held := voices.Get(), correspondence.Get()
		response := probeResponse{Live: live.Len(), Entries: len(held.entries)}
		response.Info, response.Found = live.Info(request.Voice)
		response.Entry, response.Held = held.of(request.Entity)
		response.Listener = sound.ListenerParams{
			Position:    m.Some(listener.Get().Position()),
			Orientation: m.Some(listener.Get().Orientation()),
		}
		answer.Set(response)
	})
}

// endedHandler collects every VoiceEndedEvent the flush publishes, which is the
// other half of sound's closure property: a Voice is in the live view, or its
// ending has fired.
type endedHandler kernel.Subscription[sound.VoiceEndedEvent]

// gamePlugin stands in for the game: it spawns the world's Entities and reads
// back what sound holds. It declares the binding because it names the binding's
// Components, and sound because it locks sound's resources.
type gamePlugin struct{ ended chan sound.VoiceEndedEvent }

func (*gamePlugin) Name() kernel.PluginName { return "game" }

func (*gamePlugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{ecs.Name, sound.Name, Name}
}

func (p *gamePlugin) Register(registrar *kernel.Registrar, _ any) error {
	ecs.RegisterComponent[marker](registrar, 16)
	registrar.HandleCommand[spawnCmd](spawnCmdImpl(registrar))
	registrar.HandleCommand[changeCmd](changeCmdImpl(registrar))
	registrar.HandleCommand[despawnCmd](despawnCmdImpl(registrar))
	registrar.HandleCommand[probeCmd](probeCmdImpl(registrar))
	registrar.Subscribe[endedHandler](p.collectEnded)
	return nil
}

func (p *gamePlugin) collectEnded() (kernel.Lock, kernel.Observe[sound.VoiceEndedEvent]) {
	return nil, func(_ kernel.Kernel, event sound.VoiceEndedEvent) { p.ended <- event }
}

// errorSink keeps every error the engine reported, so a test that expects one
// asserts on it and every other test asserts there was none.
type errorSink struct {
	mu   sync.Mutex
	errs []error
}

func (s *errorSink) add(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.errs = append(s.errs, err)
}

func (s *errorSink) snapshot() []error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]error(nil), s.errs...)
}

// harness is one composed engine: sound over nosound, ecs, the binding, and a
// game plugin standing in for the app. app is not composed - sound and the
// binding both subscribe to app.UpdateEvent and depend on nobody for it - so
// the test owns the clock, which is what a fixed step means here.
type harness struct {
	t      *testing.T
	kernel kernel.Executioner
	ended  chan sound.VoiceEndedEvent
	errs   *errorSink
	ogg    []byte
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	return newHarnessCapped(t, 0)
}

// newHarnessCapped is newHarness under a voice cap, which is the only way a
// test reaches a Voice that sound ended on its own with nothing having
// despawned.
func newHarnessCapped(t *testing.T, maxVoices int) *harness {
	t.Helper()
	bytes, err := os.ReadFile("testdata/pianoroll.ogg")
	if err != nil {
		t.Fatalf("reading the fixture clip: %v", err)
	}
	files := fstest.MapFS{
		clip:      {Data: bytes},
		otherClip: {Data: bytes},
	}
	sink := &errorSink{}
	ended := make(chan sound.VoiceEndedEvent, 64)
	engine := kernel.New(map[kernel.PluginName]any{
		sound.Name: sound.Config{MaxVoices: maxVoices},
	}).Handler(func(err error) error { sink.add(err); return nil }).
		WithPlugins(
			storageplugin.New(), permanentAdapter{}, readMountAdapter{storage.ReadMount{Id: "test", Priority: 10, FS: files}},
			soundplugin.New(), nosoundplugin.New(),
			ecsplugin.New(), New(), &gamePlugin{ended: ended},
		)
	stopped := make(chan struct{})
	t.Cleanup(func() {
		engine.Quit()
		<-stopped
	})
	go func() {
		defer close(stopped)
		engine.Run()
	}()
	<-engine.Ready()
	return &harness{t: t, kernel: engine.Executioner(), ended: ended, errs: sink, ogg: bytes}
}

// oggBytes is the fixture Clip as bytes a caller already holds, which is the
// other kind of sound.ClipRef and the one that is not comparable. It is built
// once per harness, because a fresh Blob every call names an asset nothing can
// ask for twice.
func (h *harness) oggBytes() assets.Blob {
	h.t.Helper()
	return assets.NewBlob(h.ogg)
}

// tick publishes one real app.UpdateEvent and waits for it: every System runs,
// then sound's flush drains what they recorded.
func (h *harness) tick() {
	h.t.Helper()
	h.kernel.PublishEvent(app.UpdateEvent{Dt: step, Last: true}).Wait()
}

func (h *harness) ticks(n int) {
	h.t.Helper()
	for range n {
		h.tick()
	}
}

func (h *harness) spawn(request spawnRequest) ecs.Entity {
	h.t.Helper()
	return h.kernel.ExecuteCommand[spawnCmd](request).Entity
}

// emit spawns an Entity that sounds like clip and stands nowhere, which is the
// commonest Emitter there is.
func (h *harness) emit(ref sound.ClipRef, params sound.Params) ecs.Entity {
	h.t.Helper()
	return h.spawn(spawnRequest{Emitter: &Emitter{Clip: ref, Params: params}})
}

func (h *harness) change(request changeRequest) {
	h.t.Helper()
	h.kernel.ExecuteCommand[changeCmd](request)
}

func (h *harness) despawn(e ecs.Entity) {
	h.t.Helper()
	h.kernel.ExecuteCommand[despawnCmd](despawnRequest{Entity: e})
}

func (h *harness) probe(e ecs.Entity) probeResponse {
	h.t.Helper()
	response := h.kernel.ExecuteCommand[probeCmd](probeRequest{Entity: e})
	response.Info, response.Found = h.info(response.Entry.voice)
	return response
}

func (h *harness) info(voice sound.Voice) (sound.VoiceInfo, bool) {
	h.t.Helper()
	response := h.kernel.ExecuteCommand[probeCmd](probeRequest{Voice: voice})
	return response.Info, response.Found
}

func (h *harness) listener() sound.ListenerParams {
	h.t.Helper()
	return h.kernel.ExecuteCommand[probeCmd](probeRequest{}).Listener
}

// voiceOf is the Voice the binding started for an Entity, and it fails the test
// when the binding holds no entry at all - every caller is asserting about a
// Voice it believes exists.
func (h *harness) voiceOf(e ecs.Entity) sound.Voice {
	h.t.Helper()
	got := h.probe(e)
	if !got.Held {
		h.t.Fatalf("the binding holds no entry for %v", e)
	}
	return got.Entry.voice
}

// waitEnded takes the next ending, and fails rather than hanging.
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

// noEnding asserts that nothing ended, which is most of what "and it did not
// restart" means: a Voice that restarted would have to end again.
func (h *harness) noEnding() {
	h.t.Helper()
	select {
	case event := <-h.ended:
		h.t.Fatalf("an unexpected %v ended as %v", event.Voice, event.Reason)
	default:
	}
}

func (h *harness) noErrors() {
	h.t.Helper()
	if errs := h.errs.snapshot(); len(errs) != 0 {
		h.t.Fatalf("the engine reported %d errors, the first being %v", len(errs), errs[0])
	}
}
