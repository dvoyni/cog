package proto

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/kernel"

	"protoecs/ecs"
)

// cog#256 asks how a data-driven spawn names its lock set, on the premise that
// a spawn must name every Component it writes while a data row is not read
// until long after Lock ran. The premise does not survive contact with cog#240:
// Entities holds a reference to every Store, so Despawn empties all of them
// holding write{*Entities} alone, and every System touching any Store declares
// read{*Entities} unconditionally.
//
// If that is so, write{*Entities} already excludes every System in the frame,
// the per-Component writes a static Spawn declares are redundant *for locking*,
// and a spawn whose Components are chosen at runtime has precisely the lock set
// of one whose Components are spelled in Go. That is an argument read off the
// source. Requirement 1 of the map is that such claims are measured.

type Alpha struct{ N float64 }
type Beta struct{ N float64 }
type Gamma struct{ N float64 }

type AlphaWQ struct{ *Alpha }
type BetaWQ struct{ *Beta }
type GammaWQ struct{ *Gamma }

type AlphaBundle struct{ A Alpha }
type GammaBundle struct{ G Gamma }

// SeedBundle populates the world before the frame under measurement, so the
// benchmarked tick holds only the Systems being priced.
type SeedBundle struct {
	A Alpha
	B Beta
	G Gamma
}

// SeedEvent is deliberately not app.UpdateEvent: the seeding Spawn must not
// appear in the frame whose cost is the question.
type SeedEvent struct{ N int }

// occupancy records the greatest number of System bodies entered at once.
type occupancy struct {
	cur atomic.Int32
	max atomic.Int32
}

func (o *occupancy) hold() {
	n := o.cur.Add(1)
	for {
		m := o.max.Load()
		if n <= m || o.max.CompareAndSwap(m, n) {
			break
		}
	}
	// 2ms is roughly a thousand times the ~2.2us scheduling floor cog#241
	// measured, so a permitted overlap cannot be missed by timing luck.
	time.Sleep(2 * time.Millisecond)
	o.cur.Add(-1)
}

type barrierMode int

const (
	// The control. Two Systems writing different Components, neither spawning:
	// disjoint lock sets, so the scheduler may overlap them. If it does not,
	// the harness cannot observe concurrency and the subject proves nothing.
	barrierDisjointQueries barrierMode = iota
	// The subject. The same pair, except the Alpha System spawns rather than
	// queries. It still names only Alpha; only *Entities changes hands.
	barrierSpawnAndQuery

	// Frame-cost modes, no sleeping. All three run the same third System over
	// the same entities; they differ only in what it declares.
	barrierTwoWorkers
	barrierTwoPlusQuery
	barrierTwoPlusIdleSpawn
	barrierTwoPlusChurn
)

type (
	barrierSubA    kernel.Subscription[app.UpdateEvent]
	barrierSubB    kernel.Subscription[app.UpdateEvent]
	barrierSubC    kernel.Subscription[app.UpdateEvent]
	barrierSeedSub kernel.Subscription[SeedEvent]
)

type barrierPlug struct {
	mode barrierMode
	o    *occupancy
}

func (barrierPlug) Name() kernel.PluginName           { return "barrier" }
func (barrierPlug) Dependencies() []kernel.PluginName { return nil }

func (p barrierPlug) Register(r *kernel.Registrar, _ any) error {
	const ids = 8192
	en := ecs.NewEntities(ids)
	r.InitResource[*ecs.Entities](en)
	ecs.RegisterComponent[Alpha](r, en, ids)
	ecs.RegisterComponent[Beta](r, en, ids)
	ecs.RegisterComponent[Gamma](r, en, ids)
	o := p.o

	r.Subscribe[barrierSeedSub, SeedEvent](ecs.ToHandler[SeedEvent](en,
		func(sp *ecs.Spawn[SeedBundle], n *ecs.In[int]) {
			for range n.Get() {
				sp.New(SeedBundle{})
			}
		},
		ecs.Feed(func(e SeedEvent) int { return e.N })))

	switch p.mode {
	case barrierDisjointQueries, barrierSpawnAndQuery:
		// System B is identical in both: it writes Beta and nothing else.
		r.Subscribe[barrierSubB, app.UpdateEvent](ecs.ToHandler[app.UpdateEvent](en,
			func(q *ecs.Query[BetaWQ]) {
				o.hold()
				for _, it := range q.All() {
					it.Beta.N++
				}
			}))
		if p.mode == barrierDisjointQueries {
			r.Subscribe[barrierSubA, app.UpdateEvent](ecs.ToHandler[app.UpdateEvent](en,
				func(q *ecs.Query[AlphaWQ]) {
					o.hold()
					for _, it := range q.All() {
						it.Alpha.N++
					}
				}))
		} else {
			r.Subscribe[barrierSubA, app.UpdateEvent](ecs.ToHandler[app.UpdateEvent](en,
				func(sp *ecs.Spawn[AlphaBundle]) {
					o.hold()
					sp.New(AlphaBundle{})
				}))
		}

	case barrierTwoWorkers, barrierTwoPlusQuery, barrierTwoPlusIdleSpawn, barrierTwoPlusChurn:
		r.Subscribe[barrierSubA, app.UpdateEvent](ecs.ToHandler[app.UpdateEvent](en,
			func(q *ecs.Query[AlphaWQ]) {
				for _, it := range q.All() {
					it.Alpha.N += 1.5
				}
			}))
		r.Subscribe[barrierSubB, app.UpdateEvent](ecs.ToHandler[app.UpdateEvent](en,
			func(q *ecs.Query[BetaWQ]) {
				for _, it := range q.All() {
					it.Beta.N += 1.5
				}
			}))
		switch p.mode {
		case barrierTwoPlusQuery:
			// A third System that is concurrent: it writes Gamma, which nobody
			// else names, so it may run beside the other two. read{*Entities}.
			r.Subscribe[barrierSubC, app.UpdateEvent](ecs.ToHandler[app.UpdateEvent](en,
				func(q *ecs.Query[GammaWQ]) {
					for _, it := range q.All() {
						it.Gamma.N += 1.5
					}
				}))
		case barrierTwoPlusIdleSpawn:
			// Byte for byte the same work, plus a Spawn parameter it never
			// uses. That single parameter is the whole experiment: it upgrades
			// read{*Entities} to write{*Entities} at registration and changes
			// nothing else, so the delta against the case above is the barrier
			// with the spawning subtracted out.
			r.Subscribe[barrierSubC, app.UpdateEvent](ecs.ToHandler[app.UpdateEvent](en,
				func(q *ecs.Query[GammaWQ], _ *ecs.Spawn[GammaBundle]) {
					for _, it := range q.All() {
						it.Gamma.N += 1.5
					}
				}))
		case barrierTwoPlusChurn:
			// The same again, now actually spawning -- and despawning what it
			// spawned, so the population stays flat and the iteration above
			// keeps costing what it costs in the other two modes.
			r.Subscribe[barrierSubC, app.UpdateEvent](ecs.ToHandler[app.UpdateEvent](en,
				func(q *ecs.Query[GammaWQ], sp *ecs.Spawn[GammaBundle], w *ecs.WriteableEntities) {
					for _, it := range q.All() {
						it.Gamma.N += 1.5
					}
					w.Despawn(sp.New(GammaBundle{}))
				}))
		}
	}
	return nil
}

func runBarrier(tb testing.TB, mode barrierMode, seed int) (*kernel.Engine, *occupancy) {
	tb.Helper()
	o := &occupancy{}
	ctx, cancel := context.WithCancel(context.Background())
	tb.Cleanup(cancel)
	e := kernel.New(nil).
		Handler(func(err error) bool { tb.Errorf("kernel error: %v", err); return true }).
		WithPlugins(barrierPlug{mode: mode, o: o})
	go e.Run(ctx)
	<-e.Ready()
	if seed > 0 {
		e.Executioner().PublishEvent(SeedEvent{N: seed}).Wait()
	}
	return e, o
}

// TestASpawnExcludesASystemItSharesNoComponentWith is the whole of cog#256's
// locking question, measured. The two Systems name disjoint Components in both
// runs; only the *Entities access differs.
func TestASpawnExcludesASystemItSharesNoComponentWith(t *testing.T) {
	occupancyOver := func(mode barrierMode) int32 {
		e, o := runBarrier(t, mode, 0)
		for range 5 {
			e.Executioner().PublishEvent(tick).Wait()
		}
		return o.max.Load()
	}

	// Validate the harness before trusting it, as cog#241 did: observing no
	// overlap proves nothing unless an overlap would have been seen.
	if got := occupancyOver(barrierDisjointQueries); got != 2 {
		t.Fatalf("two Systems with disjoint lock sets reached occupancy %d, want 2; "+
			"the harness cannot observe concurrency, so the subject run proves nothing", got)
	}
	if got := occupancyOver(barrierSpawnAndQuery); got != 1 {
		t.Fatalf("a Spawn ran beside a System naming none of its Components: occupancy %d, want 1", got)
	}
	t.Logf("two Systems writing Alpha and Beta overlap; replacing the Alpha one with a " +
		"Spawn over Alpha serialises them, and the only thing that changed is write{*Entities}")
}

// BenchmarkSpawnBarrier prices what that exclusion costs a frame. All four
// modes run the same two workers; the third System, where present, iterates the
// same 2000 entities in every mode. Only its declared lock changes, so the
// delta between "a-query" and "an-idle-spawn" is the barrier with the spawning
// subtracted out, and "churn" adds back one real spawn and despawn.
func BenchmarkSpawnBarrier(b *testing.B) {
	const seed = 2000
	for _, c := range []struct {
		name string
		mode barrierMode
	}{
		{"two-workers", barrierTwoWorkers},
		{"two-workers-plus-a-query", barrierTwoPlusQuery},
		{"two-workers-plus-an-idle-spawn", barrierTwoPlusIdleSpawn},
		{"two-workers-plus-churn", barrierTwoPlusChurn},
	} {
		b.Run(c.name, func(b *testing.B) {
			e, _ := runBarrier(b, c.mode, seed)
			x := e.Executioner()
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				x.PublishEvent(tick).Wait()
			}
		})
	}
}
