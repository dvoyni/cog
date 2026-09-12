package ecs

import (
	"strings"
	"testing"
	"time"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/kernel"
)

// fixedTick is the second, unrelated event: a fixed-step simulation tick that
// shares nothing with app.UpdateEvent but the fact that a step can be projected
// out of it.
type fixedTick struct{ Step float64 }

type advanceOnUpdate kernel.Subscription[app.UpdateEvent]

type advanceOnFixedTick kernel.Subscription[fixedTick]

// advance names no event. That is the whole point of In: the same gameplay is
// drivable by the app's update, by a fixed-step tick, by a rollback
// re-simulation or by a test harness publishing its own frames, without being
// written twice.
func advance(q *Query[moveQuery], dt *In[float64]) {
	step := float32(dt.Get()) // once, outside the loop
	for _, it := range q.All() {
		it.Body.X += it.Velocity.X * step
	}
}

// TestOneSystemRunsUnderTwoUnrelatedEvents is the criterion the whole of In and
// Feed exists to meet: one System func, two unrelated event types, one engine,
// unchanged.
func TestOneSystemRunsUnderTwoUnrelatedEvents(t *testing.T) {
	entities, components, engine := newWorld(t, 64, func(registrar *kernel.Registrar, world *Entities) {
		registrar.Subscribe[advanceOnUpdate](ToHandler[app.UpdateEvent](world, advance,
			Feed(func(e app.UpdateEvent) float64 { return e.Dt })))
		registrar.Subscribe[advanceOnFixedTick](ToHandler[fixedTick](world, advance,
			Feed(func(e fixedTick) float64 { return e.Step })))
	})

	e := entities.alloc()
	components.bodies.Set(e, body{})
	components.velocities.Set(e, velocity{X: 10})

	frame(t, engine, 0.5)
	if err := engine.Executioner().PublishEvent(fixedTick{Step: 0.25}).Wait(); err != nil {
		t.Fatalf("publishing the fixed tick: %v", err)
	}

	if value, _ := components.bodies.Get(e); value.X != 7.5 {
		t.Fatalf("body is %v after an update of 0.5 and a fixed tick of 0.25 at velocity 10, want {7.5 0}", value)
	}
}

// TestAnInputNamedByNoFeedIsRejected says which Feed is missing rather than
// leaving the System to read a zero every tick.
func TestAnInputNamedByNoFeedIsRejected(t *testing.T) {
	message := composeAndFail(t, func(registrar *kernel.Registrar, world *Entities) {
		registrar.Subscribe[guardSystem](ToHandler[app.UpdateEvent](world,
			func(q *Query[moveQuery], dt *In[float64]) {}))
	})
	for _, want := range []string{"ecs.In[float64]", "Feed", "app.UpdateEvent"} {
		if !strings.Contains(message, want) {
			t.Fatalf("composition failure %q does not name %q", message, want)
		}
	}
}

// TestAFeedNothingConsumesIsRejected catches the typo the other way round: a
// projection computed every tick and thrown away, almost always a Feed whose
// type does not match the parameter it was written for.
func TestAFeedNothingConsumesIsRejected(t *testing.T) {
	message := composeAndFail(t, func(registrar *kernel.Registrar, world *Entities) {
		registrar.Subscribe[guardSystem](ToHandler[app.UpdateEvent](world,
			func(q *Query[moveQuery], dt *In[float64]) {},
			Feed(func(e app.UpdateEvent) float64 { return e.Dt }),
			Feed(func(e app.UpdateEvent) float32 { return float32(e.Dt) })))
	})
	if !strings.Contains(message, "ecs.In[float32]") {
		t.Fatalf("composition failure %q does not name the Feed nothing consumes", message)
	}
}

// TestAFeedMayNotBeSharedBetweenSystems is the hazard of hoisting a Feed into a
// variable: the Feeder carries the In instance, so two Systems given the same
// Feeder would write one cell from two publications that may run concurrently.
func TestAFeedMayNotBeSharedBetweenSystems(t *testing.T) {
	entities := NewEntities(8)
	shared := Feed(func(e app.UpdateEvent) float64 { return e.Dt })
	ToHandler[app.UpdateEvent](entities, func(dt *In[float64]) {}, shared)

	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatalf("one Feed was bound to two Systems")
		}
		if message, _ := recovered.(string); !strings.Contains(message, "ecs.Feed") {
			t.Fatalf("panic %v does not say to call ecs.Feed at each registration site", recovered)
		}
	}()
	ToHandler[app.UpdateEvent](entities, func(dt *In[float64]) {}, shared)
}

// notice is what a System publishes when it has something to say. Publishing is
// the one thing a System needs the kernel value for, and it is why kernel.Kernel
// is in the classification table at all.
type notice struct{ Count int }

type noticeSystem kernel.Subscription[app.UpdateEvent]

type noticeWatcher kernel.Subscription[notice]

// TestASystemMayPublishAnEvent covers the kernel value in a signature. It is the
// one parameter that declares nothing: publishing is fire-and-forget and takes
// no lock of its own.
func TestASystemMayPublishAnEvent(t *testing.T) {
	heard := make(chan int, 8)
	entities, components, engine := newWorld(t, 64, func(registrar *kernel.Registrar, world *Entities) {
		registrar.Subscribe[noticeSystem](ToHandler[app.UpdateEvent](world,
			func(k kernel.Kernel, q *Query[moveQuery]) {
				count := 0
				for range q.All() {
					count++
				}
				k.PublishEvent(notice{Count: count})
			}))
		registrar.Subscribe[noticeWatcher](ToHandler[notice](world,
			func(seen notice) {
				select {
				case heard <- seen.Count:
				default:
				}
			}))
	})

	for range 3 {
		e := entities.alloc()
		components.bodies.Set(e, body{})
		components.velocities.Set(e, velocity{X: 1})
	}

	frame(t, engine, 1)
	// The publication is fire-and-forget, so it is waited for on its own arrival
	// rather than through the frame that raised it.
	select {
	case count := <-heard:
		if count != 3 {
			t.Fatalf("the watcher heard %d entities, want 3", count)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("the event the System published never reached its watcher")
	}
}
