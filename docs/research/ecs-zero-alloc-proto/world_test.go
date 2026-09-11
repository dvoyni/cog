package proto

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/kernel"

	"protoecs/ecs"
)

// cog#245 asks whether more than one World may exist at a time, and if so how a
// second one is named. The ticket's own instruction is to check the cheapest
// place first: two worlds in one `go test` process either works or it does not.
// This file runs that case, and the others the answer turns on -- whether two
// worlds fit in one engine, whether two worlds serialise against each other, and
// whether one world can drive another.

// Marked is this file's own Component, so it cannot collide with the ones the
// other test files register.
type Marked struct{ N float64 }

type MarkedQ struct{ M *Marked }

type markSub kernel.Subscription[app.UpdateEvent]

// soloWorld is one World's registration-time handle: the plain Go value cog#244
// found the app must thread through its plugin constructors.
type soloWorld struct {
	en    *ecs.Entities
	store *ecs.Store[Marked]
	seed  []ecs.Entity
}

// soloPlug is a whole World in one plugin: id authority, one Component, and
// optionally one System over it. Its name is a field so the same plugin can be
// instantiated twice, which is the "two instances of one plugin" option the
// ticket asks about.
type soloPlug struct {
	name kernel.PluginName
	w    *soloWorld
	n    int
	sys  any
}

func (p soloPlug) Name() kernel.PluginName         { return p.name }
func (soloPlug) Dependencies() []kernel.PluginName { return nil }

func (p soloPlug) Register(r *kernel.Registrar, _ any) error {
	ids := uint32(p.n + 16)
	en := ecs.NewEntities(ids)
	r.InitResource[*ecs.Entities](en)
	st := ecs.RegisterComponent[Marked](r, en, ids)
	p.w.en, p.w.store = en, st
	for range p.n {
		e := en.Alloc()
		st.Add(e, Marked{})
		p.w.seed = append(p.w.seed, e)
	}
	if p.sys != nil {
		r.Subscribe[markSub, app.UpdateEvent](ecs.ToHandler[app.UpdateEvent](en, p.sys))
	}
	return nil
}

// addN is a System that advances every Marked by a fixed amount, so which World
// ran is visible in the numbers afterwards.
func addN(d float64) any {
	return func(q *ecs.Query[MarkedQ], _ app.UpdateEvent) {
		for _, it := range q.All() {
			it.M.N += d
		}
	}
}

func startSolo(tb testing.TB, p soloPlug) *kernel.Engine {
	tb.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	tb.Cleanup(cancel)
	e := kernel.New(nil).
		Handler(func(err error) bool { tb.Errorf("kernel error: %v", err); return true }).
		WithPlugins(p)
	go e.Run(ctx)
	<-e.Ready()
	return e
}

func mustN(t *testing.T, w *soloWorld, want float64) {
	t.Helper()
	for _, e := range w.seed {
		m, ok := w.store.Get(e)
		if !ok {
			t.Fatalf("entity %v lost its Marked", e)
		}
		if m.N != want {
			t.Fatalf("Marked.N = %v, want %v", m.N, want)
		}
	}
}

// Two engines in one process are two Worlds: same Component Go type, different
// stores, different values. This is the whole answer, and it needs nothing added
// to the ECS, because a resource cell is keyed per engine registry.
func TestTwoEnginesAreTwoWorlds(t *testing.T) {
	wa, wb := &soloWorld{}, &soloWorld{}
	ea := startSolo(t, soloPlug{"world", wa, 4, addN(1)})
	eb := startSolo(t, soloPlug{"world", wb, 4, addN(100)})

	ea.Executioner().PublishEvent(tick).Wait()
	eb.Executioner().PublishEvent(tick).Wait()

	if wa.store == wb.store {
		t.Fatalf("both engines share one *ecs.Store[Marked]")
	}
	if wa.en == wb.en {
		t.Fatalf("both engines share one *ecs.Entities")
	}
	mustN(t, wa, 1)
	mustN(t, wb, 100)
	t.Logf("two worlds, same Component type, stores %p and %p", wa.store, wb.store)
}

// The counter-case: two Worlds in ONE engine. The second one's id authority
// collides with the first's, because *ecs.Entities is one resource cell per
// engine -- so this is not a Component-keying problem that a phantom tag on
// Store alone would solve. Naming the plugins differently does not help.
func TestTwoWorldsInOneEngineCollide(t *testing.T) {
	wa, wb := &soloWorld{}, &soloWorld{}
	_, errs := compose(t, soloPlug{"world-a", wa, 2, nil}, soloPlug{"world-b", wb, 2, nil})
	if len(errs) == 0 {
		t.Fatalf("two worlds composed into one engine; expected an id-authority collision")
	}
	joined := errors.Join(errs...)
	var duplicate kernel.ErrDuplicateRegistration
	if !errors.As(joined, &duplicate) {
		t.Fatalf("want ErrDuplicateRegistration, got %v", joined)
	}
	t.Logf("the error a user sees: %v", duplicate)
}

// The cost the ticket fears: two Worlds sharing one lock per Component type, so
// every shared type serialises them. Two engines do not, and this measures it --
// both Systems hold the write lock on *ecs.Store[Marked] at the same instant. If
// the lock were shared, the second never enters and this times out.
func TestTwoWorldsDoNotSerialiseAgainstEachOther(t *testing.T) {
	entered := make(chan string, 2)
	release := make(chan struct{})
	blocking := func(which string) any {
		return func(q *ecs.Query[MarkedQ], _ app.UpdateEvent) {
			for _, it := range q.All() {
				it.M.N++
			}
			entered <- which
			<-release
		}
	}

	wa, wb := &soloWorld{}, &soloWorld{}
	ea := startSolo(t, soloPlug{"world", wa, 2, blocking("a")})
	eb := startSolo(t, soloPlug{"world", wb, 2, blocking("b")})

	// Both publications are in flight: PublishEvent returns before the handler runs.
	pa := ea.Executioner().PublishEvent(tick)
	pb := eb.Executioner().PublishEvent(tick)

	seen := map[string]bool{}
	deadline := time.After(5 * time.Second)
	for range 2 {
		select {
		case which := <-entered:
			seen[which] = true
		case <-deadline:
			close(release)
			t.Fatalf("only %v entered: the two worlds serialised on one lock", seen)
		}
	}
	close(release)
	pa.Wait()
	pb.Wait()
	t.Logf("both worlds held the write lock on *ecs.Store[Marked] concurrently")
}

// A second World still has to be ticked. It does not need a second OS loop:
// app.UpdateEvent is a plain struct, so the engine that owns the frame can drive
// the other one through its Executioner. The nested Wait blocks a task in the
// driving engine, not its coordinator, so it completes.
func TestOneEngineCanDriveAnother(t *testing.T) {
	driven := &soloWorld{}
	eDriven := startSolo(t, soloPlug{"world", driven, 3, addN(7)})

	driver := &soloWorld{}
	pump := func(q *ecs.Query[MarkedQ], ev app.UpdateEvent) {
		for _, it := range q.All() {
			it.M.N++
		}
		eDriven.Executioner().PublishEvent(ev).Wait()
	}
	eDriver := startSolo(t, soloPlug{"world", driver, 3, pump})

	eDriver.Executioner().PublishEvent(tick).Wait()

	mustN(t, driver, 1)
	mustN(t, driven, 7)
	t.Logf("one frame in the driving engine advanced both worlds")
}

// What a second World costs, in the only currency it spends: goroutines. An
// engine is its Run loop plus one scheduler coordinator; there is no worker pool
// to duplicate.
func TestSecondEngineCostsAFixedHandfulOfGoroutines(t *testing.T) {
	runtime.GC()
	time.Sleep(20 * time.Millisecond)
	before := runtime.NumGoroutine()
	w := &soloWorld{}
	e := startSolo(t, soloPlug{"world", w, 2, addN(1)})
	e.Executioner().PublishEvent(tick).Wait()
	time.Sleep(20 * time.Millisecond)
	after := runtime.NumGoroutine()
	t.Logf("a second World costs %d goroutines (%d -> %d)", after-before, before, after)
	if after-before > 8 {
		t.Fatalf("a second engine cost %d goroutines; expected a small fixed number", after-before)
	}
}

// The alternative the ticket names: separate the worlds in the *type*. It works
// today with no ECS change at all -- a generic wrapper makes Component types
// distinct, so they get distinct cells. What it does not separate is the id
// authority, which is still one *ecs.Entities. So this buys tagged Components
// inside one World, not two Worlds.
type Tagged[C any, W any] struct{ C C }

type (
	serverW struct{}
	clientW struct{}
)

type taggedQ struct {
	S *Tagged[Marked, serverW]
	C Tagged[Marked, clientW]
}

type taggedPlug struct {
	en     *ecs.Entities
	server *ecs.Store[Tagged[Marked, serverW]]
	client *ecs.Store[Tagged[Marked, clientW]]
	seed   ecs.Entity
}

func (*taggedPlug) Name() kernel.PluginName           { return "tagged" }
func (*taggedPlug) Dependencies() []kernel.PluginName { return nil }
func (p *taggedPlug) Register(r *kernel.Registrar, _ any) error {
	p.en = ecs.NewEntities(16)
	r.InitResource[*ecs.Entities](p.en)
	p.server = ecs.RegisterComponent[Tagged[Marked, serverW]](r, p.en, 16)
	p.client = ecs.RegisterComponent[Tagged[Marked, clientW]](r, p.en, 16)
	p.seed = p.en.Alloc()
	p.server.Add(p.seed, Tagged[Marked, serverW]{Marked{N: 1}})
	p.client.Add(p.seed, Tagged[Marked, clientW]{Marked{N: 2}})
	r.Subscribe[markSub, app.UpdateEvent](ecs.ToHandler[app.UpdateEvent](p.en,
		func(q *ecs.Query[taggedQ], _ app.UpdateEvent) {
			for _, it := range q.All() {
				it.S.C.N += it.C.C.N
			}
		}))
	return nil
}

func TestATypeTagSeparatesStoresButNotTheIdAuthority(t *testing.T) {
	p := &taggedPlug{}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	e := kernel.New(nil).
		Handler(func(err error) bool { t.Errorf("kernel error: %v", err); return true }).
		WithPlugins(p)
	go e.Run(ctx)
	<-e.Ready()
	e.Executioner().PublishEvent(tick).Wait()

	if p.server == nil || p.client == nil {
		t.Fatalf("a tagged Component did not get its own store")
	}
	s, ok := p.server.Get(p.seed)
	if !ok || s.C.N != 3 {
		t.Fatalf("server-tagged Marked = %v ok=%v, want 3", s, ok)
	}
	// The point of the test: one entity id is valid in both, because both stores
	// answer to the same *ecs.Entities. Tagging Components does not fork the World.
	if _, ok := p.client.Get(p.seed); !ok {
		t.Fatalf("the same entity id did not address the client-tagged store")
	}
	t.Logf("two tagged Component types, two stores, one shared id authority")
}
