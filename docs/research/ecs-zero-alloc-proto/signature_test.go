package proto

import (
	"context"
	"testing"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/kernel"

	"protoecs/ecs"
)

// A System that names the event is welded to it: it can only ever be
// subscribed to that one event type, so the same gameplay cannot also be driven
// by a fixed step, by a rollback re-simulation, or by a test harness with its
// own clock. The event belongs to the adapter, which is already generic over
// it; a System declares only what it consumes.
//
// This file measures whether that decoupling is free.

// FixedTick is a second driver, unrelated to app.UpdateEvent, standing in for a
// fixed-step simulation clock.
type FixedTick struct{ Step float64 }

type Advanced struct{ N float64 }

type AdvancedQ struct{ A *Advanced }

type (
	variableSub kernel.Subscription[app.UpdateEvent]
	fixedSub    kernel.Subscription[FixedTick]
)

// advance names no event. It is the System both drivers run, unchanged and
// un-recompiled, and it is the whole point of the file.
func advance(q *ecs.Query[AdvancedQ], dt *ecs.In[float64]) {
	// Read it once, outside the loop. In is a pointer to a cell the adapter
	// writes, so a Get() inside the loop is a load the compiler cannot hoist
	// past the Component writes -- it has no way to prove they do not alias.
	// Measured: calling it per entity costs ~0.5ns an entity, which is a tenth
	// of the whole iteration. Hoisting erases it. See advanceNaively.
	d := dt.Get()
	for _, it := range q.All() {
		it.A.N += d
	}
}

// advanceNaively is the same System with the Get inside the loop, kept to
// measure what the hoist is worth.
func advanceNaively(q *ecs.Query[AdvancedQ], dt *ecs.In[float64]) {
	for _, it := range q.All() {
		it.A.N += dt.Get()
	}
}

type twoDriverWorld struct {
	en    *ecs.Entities
	store *ecs.Store[Advanced]
	seed  []ecs.Entity
}

type twoDriverPlug struct {
	w *twoDriverWorld
	n int
}

func (twoDriverPlug) Name() kernel.PluginName           { return "two-drivers" }
func (twoDriverPlug) Dependencies() []kernel.PluginName { return nil }

func (p twoDriverPlug) Register(r *kernel.Registrar, _ any) error {
	ids := uint32(p.n + 16)
	en := ecs.NewEntities(ids)
	r.InitResource[*ecs.Entities](en)
	store := ecs.RegisterComponent[Advanced](r, en, ids)
	p.w.en, p.w.store = en, store
	for range p.n {
		e := en.Alloc()
		store.Add(e, Advanced{})
		p.w.seed = append(p.w.seed, e)
	}
	// One System, two subscriptions, two unrelated event types. Only the Feed
	// differs, and it is the adapter's business rather than the System's.
	r.Subscribe[variableSub, app.UpdateEvent](ecs.ToHandler[app.UpdateEvent](en, advance,
		ecs.Feed(func(e app.UpdateEvent) float64 { return e.Dt })))
	r.Subscribe[fixedSub, FixedTick](ecs.ToHandler[FixedTick](en, advance,
		ecs.Feed(func(e FixedTick) float64 { return e.Step })))
	return nil
}

func startTwoDrivers(tb testing.TB, n int) (*kernel.Engine, *twoDriverWorld) {
	tb.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	tb.Cleanup(cancel)
	w := &twoDriverWorld{}
	e := kernel.New(nil).
		Handler(func(err error) bool { tb.Errorf("kernel error: %v", err); return true }).
		WithPlugins(twoDriverPlug{w, n})
	go e.Run(ctx)
	<-e.Ready()
	return e, w
}

// The same System func, run by two unrelated events, each supplying its own
// per-tick value. Naming the event in the signature would have made this
// impossible without writing the System twice.
func TestOneSystemRunsUnderTwoUnrelatedEvents(t *testing.T) {
	const n = 8
	e, w := startTwoDrivers(t, n)
	ex := e.Executioner()

	ex.PublishEvent(app.UpdateEvent{Dt: 0.25, Last: true}).Wait()
	for _, seed := range w.seed {
		got, ok := w.store.Get(seed)
		if !ok || got.N != 0.25 {
			t.Fatalf("after the variable tick N = %v ok=%v, want 0.25", got, ok)
		}
	}

	ex.PublishEvent(FixedTick{Step: 2}).Wait()
	for _, seed := range w.seed {
		got, ok := w.store.Get(seed)
		if !ok || got.N != 2.25 {
			t.Fatalf("after the fixed tick N = %v ok=%v, want 2.25", got, ok)
		}
	}
	t.Logf("one System, two unrelated drivers, no event in its signature")
}

// eventNaming is the counterfactual: the same work, with the event named in the
// signature, which is what the decoupling has to be measured against.
func eventNaming(q *ecs.Query[AdvancedQ], ev app.UpdateEvent) {
	for _, it := range q.All() {
		it.A.N += ev.Dt
	}
}

type namedSub kernel.Subscription[app.UpdateEvent]

type oneDriverPlug struct {
	w     *twoDriverWorld
	n     int
	named bool
	naive bool
}

func (oneDriverPlug) Name() kernel.PluginName           { return "one-driver" }
func (oneDriverPlug) Dependencies() []kernel.PluginName { return nil }

func (p oneDriverPlug) Register(r *kernel.Registrar, _ any) error {
	ids := uint32(p.n + 16)
	en := ecs.NewEntities(ids)
	r.InitResource[*ecs.Entities](en)
	store := ecs.RegisterComponent[Advanced](r, en, ids)
	p.w.en, p.w.store = en, store
	for range p.n {
		e := en.Alloc()
		store.Add(e, Advanced{})
		p.w.seed = append(p.w.seed, e)
	}
	if p.named {
		r.Subscribe[namedSub, app.UpdateEvent](ecs.ToHandler[app.UpdateEvent](en, eventNaming))
		return nil
	}
	system := any(advance)
	if p.naive {
		system = advanceNaively
	}
	r.Subscribe[variableSub, app.UpdateEvent](ecs.ToHandler[app.UpdateEvent](en, system,
		ecs.Feed(func(e app.UpdateEvent) float64 { return e.Dt })))
	return nil
}

// What the decoupling costs: a Feed against naming the event directly.
func BenchmarkEventDecoupling(b *testing.B) {
	run := func(b *testing.B, n int, named, naive bool) {
		ctx, cancel := context.WithCancel(context.Background())
		b.Cleanup(cancel)
		e := kernel.New(nil).
			Handler(func(err error) bool { b.Errorf("kernel error: %v", err); return true }).
			WithPlugins(oneDriverPlug{&twoDriverWorld{}, n, named, naive})
		go e.Run(ctx)
		<-e.Ready()
		ex := e.Executioner()
		for range 4 {
			ex.PublishEvent(tick).Wait()
		}
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			ex.PublishEvent(tick).Wait()
		}
	}
	b.Run("named-event/1000", func(b *testing.B) { run(b, 1000, true, false) })
	b.Run("fed-In/1000", func(b *testing.B) { run(b, 1000, false, false) })
	b.Run("fed-In-unhoisted/1000", func(b *testing.B) { run(b, 1000, false, true) })
	b.Run("named-event/10000", func(b *testing.B) { run(b, 10000, true, false) })
	b.Run("fed-In/10000", func(b *testing.B) { run(b, 10000, false, false) })
	b.Run("fed-In-unhoisted/10000", func(b *testing.B) { run(b, 10000, false, true) })
}
