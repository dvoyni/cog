package internal

import (
	"errors"
	"reflect"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecs/ecsplugin"
	"github.com/dvoyni/cog/extensions/nosound/nosoundplugin"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
	"github.com/dvoyni/cog/slots/sound"
	"github.com/dvoyni/cog/slots/sound/soundplugin"
	"github.com/dvoyni/cog/slots/storage"
	"github.com/dvoyni/cog/slots/storage/storageplugin"
)

// driftQuery moves a Transform, as a game System does before the binding
// records it.
type driftQuery struct {
	Place *m.Transform
}

// retuneQuery writes one of the binding's own Components.
type retuneQuery struct {
	Emitter *Emitter
}

type driftSystem kernel.Subscription[app.UpdateEvent]

// moverPlugin is a game that orders itself against the binding's identity. It
// writes one of the binding's Components, or, when it only places, the
// m.Transform the ecs plugin owns.
type moverPlugin struct {
	deps   []kernel.PluginName
	places bool
}

func (*moverPlugin) Name() kernel.PluginName             { return "mover" }
func (p *moverPlugin) Dependencies() []kernel.PluginName { return p.deps }

func (p *moverPlugin) Register(registrar *kernel.Registrar, _ any) error {
	system := ecs.ToHandler[app.UpdateEvent](registrar, func(q *ecs.Query[retuneQuery]) {
		for _, it := range q.All() {
			it.Emitter.Params.Volume = m.Some[float32](0.5)
		}
	})
	if p.places {
		system = ecs.ToHandler[app.UpdateEvent](registrar, func(q *ecs.Query[driftQuery]) {
			for _, it := range q.All() {
				it.Place.Position = it.Place.Position.Add(m.Vec3{X: 1})
			}
		})
	}
	registrar.Subscribe[driftSystem](system).Before[RecordOnUpdate]()
	return nil
}

// compose builds the binding's engine with the mover beside it and returns
// every error composition reported.
func compose(mover *moverPlugin) error {
	var failure error
	kernel.New(nil).Handler(func(err error) error { failure = errors.Join(failure, err); return nil }).
		WithPlugins(
			storageplugin.New(), permanentAdapter{}, readMountAdapter{storage.ReadMount{Id: "test", Priority: 10, FS: fstest.MapFS{}}},
			soundplugin.New(), nosoundplugin.New(),
			ecsplugin.New(), New(), mover,
		)
	return failure
}

// TestTheCouplingCheckStillHoldsOnTheBindingsComponents is the coupling rule
// across the split. The Components are declared in the root and registered by
// the internal plugin under ecsaudio.Name, so a game System that locks one of
// their Stores must still declare ecsaudio, and composes once it does.
func TestTheCouplingCheckStillHoldsOnTheBindingsComponents(t *testing.T) {
	err := compose(&moverPlugin{deps: []kernel.PluginName{ecs.Name}})
	var undeclared kernel.ErrUndeclaredDependency
	if !errors.As(err, &undeclared) {
		t.Fatalf("a System locking the binding's Stores without declaring ecsaudio composed with %v", err)
	}
	if undeclared.Plugin != "mover" || undeclared.Owner != Name ||
		undeclared.Resource != reflect.TypeFor[*ecs.Store[Emitter]]() {
		t.Errorf("the refusal is %+v, want mover locking *ecs.Store[ecsaudio.Emitter] owned by %q",
			undeclared, Name)
	}

	if err := compose(&moverPlugin{deps: []kernel.PluginName{ecs.Name, Name}}); err != nil {
		t.Fatalf("a System declaring ecsaudio did not compose: %v", err)
	}
}

// TestASystemPlacingEntitiesNeedsOnlyEcs is the documented cost of the ecs
// plugin owning the m.Transform Store: every plugin with Systems already
// depends on ecs, so a System writing where an Entity stands composes without
// declaring this binding, and the coupling check never sees it.
func TestASystemPlacingEntitiesNeedsOnlyEcs(t *testing.T) {
	if err := compose(&moverPlugin{deps: []kernel.PluginName{ecs.Name}, places: true}); err != nil {
		t.Fatalf("a System writing m.Transform with only an ecs dependency did not compose: %v", err)
	}
}

// The System writes no Component and names no WriteableEntities, so it takes no
// structural lock: a frame that spawns a hundred emitters would otherwise be a
// hundred structural changes for bookkeeping nobody outside the binding reads.
// And it reads no sound resource but the queue, because an entry outliving its
// Voice is the whole policy and reading the live view would widen the lock set
// for nothing.
//
// The lock set is read off the composed engine rather than off the signature,
// because the signature is what a reader would check and the description is
// what the scheduler acts on.
func TestTheRecordingSystemTakesNoStructuralLockAndReadsNoVoiceView(t *testing.T) {
	engine := kernel.New(nil).Handler(func(err error) error { t.Errorf("composing: %v", err); return nil }).
		WithPlugins(
			storageplugin.New(), permanentAdapter{}, readMountAdapter{storage.ReadMount{Id: "test", Priority: 10, FS: fstest.MapFS{}}},
			soundplugin.New(), nosoundplugin.New(),
			ecsplugin.New(), New(),
		)

	var found, writesQueue bool
	for _, subscription := range engine.Describe().Subscriptions {
		if subscription.Type != reflect.TypeFor[RecordOnUpdate]() {
			continue
		}
		found = true
		for _, written := range subscription.Writes {
			writesQueue = writesQueue || written == reflect.TypeFor[*sound.Queue]()
			if written == reflect.TypeFor[*ecs.Entities]() {
				t.Error("the recording System takes the structural lock")
			}
			if kernel.TypeName(written) != "*ecsaudio.table" && written != reflect.TypeFor[*sound.Queue]() {
				t.Errorf("the recording System writes %s, want only sound's queue and its own table",
					kernel.TypeName(written))
			}
		}
		for _, read := range subscription.Reads {
			switch read {
			case reflect.TypeFor[*sound.Voices]():
				t.Error("the recording System reads sound's live Voice view")
			case reflect.TypeFor[*sound.Listener](), reflect.TypeFor[*sound.Buses](),
				reflect.TypeFor[*sound.Clips](), reflect.TypeFor[*sound.Device]():
				t.Errorf("the recording System reads %s, which is a sound resource other than the queue",
					kernel.TypeName(read))
			}
		}
	}
	if !found {
		t.Fatal("the composed engine describes no ecsaudio.RecordOnUpdate subscription")
	}
	if !writesQueue {
		t.Fatal("the recording System does not write sound's queue, so this test asserted nothing")
	}
}
