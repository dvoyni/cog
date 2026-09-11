package sbench

import (
	"context"
	"runtime"
	"testing"

	"github.com/dvoyni/cog/kernel"
)

// cog#241 asks whether two Systems writing one Component over disjoint entity
// sets should be allowed to run concurrently. Every candidate answer -- filter
// -encoded disjointness, per-store locks, a handler conflict matrix -- costs
// kernel machinery. None is worth building unless the parallelism it unlocks is
// worth more than the scheduling it adds.
//
// So price the ceiling. K Systems do *identical* real work on their *own*
// private Store, and differ in one respect only: the lock each declares.
//
//   - Disjoint: System i declares write{*tok[i]}, a type nobody else names, so
//     the scheduler may run all K concurrently.
//   - Shared:   every System declares write{*sharedTok}, so the scheduler
//     serialises all K -- exactly the false serialisation #241 is about.
//
// Neither regime touches another System's data, so the work is the same and the
// only variable is admission. Shared-minus-disjoint is therefore the entire
// prize on offer: no mechanism in the ticket can buy more than this, and most
// would buy less.

type benchBody struct{ X, Y, VX, VY float64 }

// step is the work one System does: a straight walk of its own dense array,
// integrating. This is the shape #239 measured All() into, so the per-entity
// cost is representative rather than a spin loop.
func step(dense []benchBody, iter int) {
	for range iter {
		for i := range dense {
			b := &dense[i]
			b.X += b.VX
			b.Y += b.VY
		}
	}
}

// tok[N] gives each System a lock nobody else names. The phantom parameter is
// what makes the reflect.Type distinct, which is the only thing the scheduler
// keys on.
type tok[N any] struct{ _ int }

type sharedTok struct{ _ int }

type (
	ph0 struct{}
	ph1 struct{}
	ph2 struct{}
	ph3 struct{}
	ph4 struct{}
	ph5 struct{}
	ph6 struct{}
	ph7 struct{}
)

// tickEvent2 carries the per-System work size so one publication is one frame.
type tickEvent2 struct{ iter int }

type sysSub[N any] kernel.Subscription[tickEvent2]

// disjointSys declares a lock unique to N. Lock stays straight-line.
func disjointSys[N any](dense []benchBody) func() (kernel.Lock, kernel.Observe[tickEvent2]) {
	return func() (kernel.Lock, kernel.Observe[tickEvent2]) {
		var w kernel.Write[*tok[N]]
		return func(a kernel.ResourceAccess) {
				w = a.GetWrite[*tok[N]]()
			}, func(_ kernel.Kernel, e tickEvent2) error {
				_ = w
				step(dense, e.iter)
				return nil
			}
	}
}

// sharedSys does the identical work but names the one lock every other System
// names too.
func sharedSys[N any](dense []benchBody) func() (kernel.Lock, kernel.Observe[tickEvent2]) {
	return func() (kernel.Lock, kernel.Observe[tickEvent2]) {
		var w kernel.Write[*sharedTok]
		return func(a kernel.ResourceAccess) {
				w = a.GetWrite[*sharedTok]()
			}, func(_ kernel.Kernel, e tickEvent2) error {
				_ = w
				step(dense, e.iter)
				return nil
			}
	}
}

func makeDense(entities int) []benchBody {
	d := make([]benchBody, entities)
	for i := range d {
		d[i] = benchBody{X: float64(i), Y: float64(i), VX: 1, VY: 1}
	}
	return d
}

type parPlugin struct {
	entities int
	systems  int
	shared   bool
}

func (parPlugin) Name() kernel.PluginName           { return "parbench" }
func (parPlugin) Dependencies() []kernel.PluginName { return nil }

func (p parPlugin) Register(r *kernel.Registrar, _ any) error {
	r.InitResource[*sharedTok](&sharedTok{})
	r.InitResource[*tok[ph0]](&tok[ph0]{})
	r.InitResource[*tok[ph1]](&tok[ph1]{})
	r.InitResource[*tok[ph2]](&tok[ph2]{})
	r.InitResource[*tok[ph3]](&tok[ph3]{})
	r.InitResource[*tok[ph4]](&tok[ph4]{})
	r.InitResource[*tok[ph5]](&tok[ph5]{})
	r.InitResource[*tok[ph6]](&tok[ph6]{})
	r.InitResource[*tok[ph7]](&tok[ph7]{})

	// Each System gets its own dense array, so no System can see another's
	// memory and the regimes differ only in their declared locks.
	if p.shared {
		regShared(r, p)
	} else {
		regDisjoint(r, p)
	}
	return nil
}

func regDisjoint(r *kernel.Registrar, p parPlugin) {
	n := p.systems
	e := p.entities
	if n > 0 {
		r.Subscribe[sysSub[ph0], tickEvent2](disjointSys[ph0](makeDense(e)))
	}
	if n > 1 {
		r.Subscribe[sysSub[ph1], tickEvent2](disjointSys[ph1](makeDense(e)))
	}
	if n > 2 {
		r.Subscribe[sysSub[ph2], tickEvent2](disjointSys[ph2](makeDense(e)))
	}
	if n > 3 {
		r.Subscribe[sysSub[ph3], tickEvent2](disjointSys[ph3](makeDense(e)))
	}
	if n > 4 {
		r.Subscribe[sysSub[ph4], tickEvent2](disjointSys[ph4](makeDense(e)))
	}
	if n > 5 {
		r.Subscribe[sysSub[ph5], tickEvent2](disjointSys[ph5](makeDense(e)))
	}
	if n > 6 {
		r.Subscribe[sysSub[ph6], tickEvent2](disjointSys[ph6](makeDense(e)))
	}
	if n > 7 {
		r.Subscribe[sysSub[ph7], tickEvent2](disjointSys[ph7](makeDense(e)))
	}
}

func regShared(r *kernel.Registrar, p parPlugin) {
	n := p.systems
	e := p.entities
	if n > 0 {
		r.Subscribe[sysSub[ph0], tickEvent2](sharedSys[ph0](makeDense(e)))
	}
	if n > 1 {
		r.Subscribe[sysSub[ph1], tickEvent2](sharedSys[ph1](makeDense(e)))
	}
	if n > 2 {
		r.Subscribe[sysSub[ph2], tickEvent2](sharedSys[ph2](makeDense(e)))
	}
	if n > 3 {
		r.Subscribe[sysSub[ph3], tickEvent2](sharedSys[ph3](makeDense(e)))
	}
	if n > 4 {
		r.Subscribe[sysSub[ph4], tickEvent2](sharedSys[ph4](makeDense(e)))
	}
	if n > 5 {
		r.Subscribe[sysSub[ph5], tickEvent2](sharedSys[ph5](makeDense(e)))
	}
	if n > 6 {
		r.Subscribe[sysSub[ph6], tickEvent2](sharedSys[ph6](makeDense(e)))
	}
	if n > 7 {
		r.Subscribe[sysSub[ph7], tickEvent2](sharedSys[ph7](makeDense(e)))
	}
}

func startParEngine(b *testing.B, p parPlugin) *kernel.Engine {
	b.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	b.Cleanup(cancel)
	e := kernel.New(nil).
		Handler(func(err error) bool { b.Fatalf("kernel error: %v", err); return true }).
		WithPlugins(p)
	go e.Run(ctx)
	<-e.Ready()
	return e
}

func benchFrame(b *testing.B, entities, systems, iter int, shared bool) {
	b.Helper()
	e := startParEngine(b, parPlugin{entities: entities, systems: systems, shared: shared})
	ex := e.Executioner()
	ex.PublishEvent(tickEvent2{iter: iter}).Wait() // warm the publication plan
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		ex.PublishEvent(tickEvent2{iter: iter}).Wait()
	}
}

func TestReportCPUs(t *testing.T) {
	t.Logf("NumCPU=%d GOMAXPROCS=%d", runtime.NumCPU(), runtime.GOMAXPROCS(0))
}

// ---------------------------------------------------------------------------
// The ticket's own example: exactly two Systems, as in "a movement System and a
// UI-anchor System both writing Position".
// ---------------------------------------------------------------------------

func BenchmarkPar2_1000_Disjoint(b *testing.B) { benchFrame(b, 1000, 2, 1, false) }
func BenchmarkPar2_1000_Shared(b *testing.B)   { benchFrame(b, 1000, 2, 1, true) }

func BenchmarkPar2_5000_Disjoint(b *testing.B) { benchFrame(b, 5000, 2, 1, false) }
func BenchmarkPar2_5000_Shared(b *testing.B)   { benchFrame(b, 5000, 2, 1, true) }

// ---------------------------------------------------------------------------
// Eight Systems: the upper end of what one frame of nox would contend on one
// Component, and the regime where a serialising lock costs the most.
// ---------------------------------------------------------------------------

func BenchmarkPar8_100_Disjoint(b *testing.B) { benchFrame(b, 100, 8, 1, false) }
func BenchmarkPar8_100_Shared(b *testing.B)   { benchFrame(b, 100, 8, 1, true) }

func BenchmarkPar8_1000_Disjoint(b *testing.B) { benchFrame(b, 1000, 8, 1, false) }
func BenchmarkPar8_1000_Shared(b *testing.B)   { benchFrame(b, 1000, 8, 1, true) }

func BenchmarkPar8_5000_Disjoint(b *testing.B) { benchFrame(b, 5000, 8, 1, false) }
func BenchmarkPar8_5000_Shared(b *testing.B)   { benchFrame(b, 5000, 8, 1, true) }

// ---------------------------------------------------------------------------
// A deliberately heavy System (iter=20 over 5000 entities, ~100k updates), to
// find where parallelism does pay. bevy's maintainer claimed dispatch is too
// expensive "for all but the heaviest systems"; this is the heaviest case.
// ---------------------------------------------------------------------------

func BenchmarkPar8_Heavy_Disjoint(b *testing.B) { benchFrame(b, 5000, 8, 20, false) }
func BenchmarkPar8_Heavy_Shared(b *testing.B)   { benchFrame(b, 5000, 8, 20, true) }

// The ticket's literal example at heavy work: exactly two Systems, 5000
// entities each, 20 passes. If parallelism ever pays for two Systems, here.
func BenchmarkPar2_Heavy_Disjoint(b *testing.B) { benchFrame(b, 5000, 2, 20, false) }
func BenchmarkPar2_Heavy_Shared(b *testing.B)   { benchFrame(b, 5000, 2, 20, true) }
