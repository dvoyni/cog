package internal

import (
	"errors"
	"reflect"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecs/ecsplugin"
	"github.com/dvoyni/cog/bundles/ecsaudio"
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
	Place *ecsaudio.Transform
}

type driftSystem kernel.Subscription[app.UpdateEvent]

// moverPlugin is a game that names the binding's Components through the root
// alone and orders itself against the binding's identity.
type moverPlugin struct{ deps []kernel.PluginName }

func (*moverPlugin) Name() kernel.PluginName             { return "mover" }
func (p *moverPlugin) Dependencies() []kernel.PluginName { return p.deps }

func (*moverPlugin) Register(registrar *kernel.Registrar, _ any) error {
	registrar.Subscribe[driftSystem](ecs.ToHandler[app.UpdateEvent](registrar, func(q *ecs.Query[driftQuery]) {
		for _, it := range q.All() {
			it.Place.Position = it.Place.Position.Add(m.Vec3{X: 1})
		}
	})).Before[ecsaudio.RecordOnUpdate]()
	return nil
}

// compose builds the binding's engine with the mover beside it and returns
// every error composition reported.
func compose(mover *moverPlugin) error {
	var failure error
	kernel.New(map[kernel.PluginName]any{
		storage.Name: storage.Config{}.WithReadFS("test", 10, fstest.MapFS{}),
	}).Handler(func(err error) error { failure = errors.Join(failure, err); return nil }).
		WithPlugins(
			storageplugin.New(), permanentAdapter{},
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
	if undeclared.Plugin != "mover" || undeclared.Owner != ecsaudio.Name ||
		undeclared.Resource != reflect.TypeFor[*ecs.Store[ecsaudio.Transform]]() {
		t.Errorf("the refusal is %+v, want mover locking *ecs.Store[ecsaudio.Transform] owned by %q",
			undeclared, ecsaudio.Name)
	}

	if err := compose(&moverPlugin{deps: []kernel.PluginName{ecs.Name, ecsaudio.Name}}); err != nil {
		t.Fatalf("a System declaring ecsaudio did not compose: %v", err)
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
	engine := kernel.New(map[kernel.PluginName]any{
		storage.Name: storage.Config{}.WithReadFS("test", 10, fstest.MapFS{}),
	}).Handler(func(err error) error { t.Errorf("composing: %v", err); return nil }).
		WithPlugins(
			storageplugin.New(), permanentAdapter{},
			soundplugin.New(), nosoundplugin.New(),
			ecsplugin.New(), New(),
		)

	var found, writesQueue bool
	for _, subscription := range engine.Describe().Subscriptions {
		if subscription.Type != reflect.TypeFor[ecsaudio.RecordOnUpdate]() {
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
