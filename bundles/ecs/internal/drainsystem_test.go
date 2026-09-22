package internal

import (
	"reflect"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// The two Last() Systems an app orders against the drainer: one that wants its
// own queue drained this Update, one that wants to see this Update's drain.
type (
	beforeDrainSystem kernel.Subscription[app.UpdateEvent]
	afterDrainSystem  kernel.Subscription[app.UpdateEvent]
)

// orderedFixture is an app with a Last() subscriber on each side of the
// drainer. It registers the After one first, so the order the test observes can
// only come from the edges: registration order would give the opposite.
type orderedFixture struct {
	ticket atomic.Int64
	before atomic.Int64
	after  atomic.Int64
}

func (*orderedFixture) Name() kernel.PluginName { return "ordered" }

func (*orderedFixture) Dependencies() []kernel.PluginName { return []kernel.PluginName{ecs.Name} }

func (f *orderedFixture) Register(registrar *kernel.Registrar, _ any) error {
	registrar.Subscribe[afterDrainSystem](ecs.ToHandler[app.UpdateEvent](registrar, func() {
		f.after.Store(f.ticket.Add(1))
	})).Last().After[ecs.DrainOnUpdate]()
	registrar.Subscribe[beforeDrainSystem](ecs.ToHandler[app.UpdateEvent](registrar, func() {
		f.before.Store(f.ticket.Add(1))
	})).Last().Before[ecs.DrainOnUpdate]()
	return nil
}

// drainerOf is the drainer's row in the engine's description.
func drainerOf(t *testing.T, engine *kernel.Engine) kernel.SubscriptionDescription {
	t.Helper()
	for _, subscription := range engine.Describe().Subscriptions {
		if subscription.Type == reflect.TypeFor[ecs.DrainOnUpdate]() {
			return subscription
		}
	}
	t.Fatalf("the architecture has no ecs.DrainOnUpdate")
	return kernel.SubscriptionDescription{}
}

// TestTheEcsPluginSubscribesTheDrainerLastOnUpdate is the ECS's first System of
// its own and its first production dependency on the app slot. The composition
// is the plugin alone with no configuration at all, because there is no opt-out:
// a queue that is never drained is not a configuration, and a reservation must
// settle within the Update it was made in.
//
// The lock set is write{*ecs.Entities} and nothing besides, which is what makes
// a drain sound for free: every System declares read{*ecs.Entities}, so the
// scheduler already refuses to run a drain beside a System queuing into a
// buffer.
func TestTheEcsPluginSubscribesTheDrainerLastOnUpdate(t *testing.T) {
	engine := runEngine(t, New())

	drainer := drainerOf(t, engine)
	if drainer.Owner != ecs.Name {
		t.Errorf("ecs.DrainOnUpdate is owned by %q, want %q", drainer.Owner, ecs.Name)
	}
	if drainer.Event != reflect.TypeFor[app.UpdateEvent]() {
		t.Errorf("ecs.DrainOnUpdate is subscribed to %s, want app.UpdateEvent", kernel.TypeName(drainer.Event))
	}
	if drainer.Phase != "last" {
		t.Errorf("ecs.DrainOnUpdate runs in the %s phase, want last", drainer.Phase)
	}
	entities := reflect.TypeFor[*ecs.Entities]()
	if !slices.Equal(drainer.Writes, []reflect.Type{entities}) ||
		len(drainer.Reads) != 0 || len(drainer.Uses) != 0 {
		t.Errorf("ecs.DrainOnUpdate writes %v, reads %v and uses %v; want write{*ecs.Entities} alone",
			drainer.Writes, drainer.Reads, drainer.Uses)
	}
}

// TestTheEcsPluginStaysZeroDependency holds the line the drainer crosses
// nothing of: it names app.UpdateEvent, which is an event type and not a
// resource, so the ECS depends on no plugin and an app that composes ecs alone
// still composes.
func TestTheEcsPluginStaysZeroDependency(t *testing.T) {
	if deps := New().Dependencies(); deps != nil {
		t.Fatalf("the ecs plugin declares dependencies %v, want none", deps)
	}
}

// TestALastSubscriberOrdersItselfBeforeTheDrain is one of the two directions a
// Last() subscriber has, and the reason the drainer has an exported identity at
// all: Last() subscribers have no order among themselves, so one that wants its
// own queue drained this Update says so by name.
func TestALastSubscriberOrdersItselfBeforeTheDrain(t *testing.T) {
	engine := runEngine(t, New(), &orderedFixture{})

	drainer := drainerOf(t, engine)
	if !slices.Contains(drainer.DependsOn, reflect.TypeFor[beforeDrainSystem]()) {
		t.Fatalf("ecs.DrainOnUpdate depends on %v, want beforeDrainSystem among them",
			drainer.DependsOn)
	}
}

// TestALastSubscriberOrdersItselfAfterTheDrain is the other direction: a
// renderer, an audio flush or a Last() Hooks reader that wants to see this
// Update's drain.
func TestALastSubscriberOrdersItselfAfterTheDrain(t *testing.T) {
	engine := runEngine(t, New(), &orderedFixture{})

	for _, subscription := range engine.Describe().Subscriptions {
		if subscription.Type != reflect.TypeFor[afterDrainSystem]() {
			continue
		}
		if !slices.Contains(subscription.DependsOn, reflect.TypeFor[ecs.DrainOnUpdate]()) {
			t.Fatalf("afterDrainSystem depends on %v, want ecs.DrainOnUpdate among them",
				subscription.DependsOn)
		}
		return
	}
	t.Fatalf("the architecture has no afterDrainSystem")
}

// TestTheDrainSeparatesTheTwoLastSubscribers runs the frame the two previous
// tests describe. Neither subscriber names the other, so the order below is the
// drainer's alone — and the After one is registered first, so registration
// order would give the opposite.
func TestTheDrainSeparatesTheTwoLastSubscribers(t *testing.T) {
	fixture := &orderedFixture{}
	engine := runEngine(t, New(), fixture)

	for range 3 {
		engine.Executioner().PublishEvent(app.UpdateEvent{Dt: 1}).Wait()
		before, after := fixture.before.Load(), fixture.after.Load()
		if before == 0 || after == 0 {
			t.Fatalf("a Last() subscriber did not run: before %d, after %d", before, after)
		}
		if before > after {
			t.Fatalf("the Before subscriber took ticket %d and the After one %d; want the drain between them",
				before, after)
		}
	}
}
