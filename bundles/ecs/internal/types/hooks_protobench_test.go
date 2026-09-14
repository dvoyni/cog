package types

// PROTOTYPE, throwaway: proto/ecs-hooks, for https://github.com/dvoyni/cog/issues/380.

import (
	"fmt"
	"testing"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

const hookPopulation = 10_000

func (w *hookBench) populate(n int) []Entity {
	ents := make([]Entity, n)
	for i := range ents {
		e := w.en.alloc()
		w.bodies.Set(e, body{X: float32(i)})
		w.vels.Set(e, velocity{X: 1})
		w.names.Set(e, named{Name: fmt.Sprint("entity ", i), X: float32(i)})
		ents[i] = e
	}
	return ents
}

type drainer interface {
	beginRun()
	endRun()
}

func reader[T any, K kindSet](en *Entities) drainer {
	h := &Hooks[T, K]{}
	h.bind(en)
	return h
}

// BenchmarkHookAddRemove prices one UpdateFor that adds and one Remove.From on a
// Store some Hooks[T, K] reads. Readers drain every 1024 pairs, off the clock.
func BenchmarkHookAddRemove(b *testing.B) {
	type arm struct {
		name    string
		readers func(en *Entities) []drainer
		names   bool
	}
	arms := []arm{
		{"none", func(*Entities) []drainer { return nil }, false},
		{"HookAddedRemoved", func(en *Entities) []drainer { return []drainer{reader[collider, HookAddedRemoved](en)} }, false},
		{"HookRemoved", func(en *Entities) []drainer { return []drainer{reader[collider, HookRemoved](en)} }, false},
		{"HookDespawned", func(en *Entities) []drainer { return []drainer{reader[collider, HookDespawned](en)} }, false},
		{"HookAll", func(en *Entities) []drainer { return []drainer{reader[collider, HookAll](en)} }, false},
		{"4-readers", func(en *Entities) []drainer {
			return []drainer{reader[collider, HookAddedRemoved](en), reader[collider, HookAll](en), reader[collider, HookRemoved](en), reader[collider, HookAdded](en)}
		}, false},
		{"string-HookAddedRemoved", func(en *Entities) []drainer { return []drainer{reader[named, HookAddedRemoved](en)} }, true},
		{"string-none", func(*Entities) []drainer { return nil }, true},
	}
	for _, a := range arms {
		b.Run(a.name, func(b *testing.B) {
			w := newHookBench(hookPopulation)
			ents := w.populate(hookPopulation)
			readers := a.readers(w.en)
			value := named{Name: "a collider's name"}
			for _, e := range ents {
				w.names.Remove(e)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				e := ents[i%len(ents)]
				if a.names {
					w.names.Set(e, value)
					w.names.Remove(e)
				} else {
					w.colliders.Set(e, collider{Radius: 1})
					w.colliders.Remove(e)
				}
				if i&1023 == 1023 {
					b.StopTimer()
					for _, r := range readers {
						r.beginRun()
						r.endRun()
					}
					b.StartTimer()
				}
			}
		})
	}
}

// BenchmarkHookSpawnDespawn prices a whole spawn and a whole despawn of an
// Entity carrying body and velocity.
func BenchmarkHookSpawnDespawn(b *testing.B) {
	arms := []struct {
		name    string
		readers func(en *Entities) []drainer
	}{
		{"none", func(*Entities) []drainer { return nil }},
		{"body-HookSpawnedDespawned", func(en *Entities) []drainer { return []drainer{reader[body, HookSpawnedDespawned](en)} }},
		{"body-HookAll", func(en *Entities) []drainer { return []drainer{reader[body, HookAll](en)} }},
		{"body+velocity-HookAll", func(en *Entities) []drainer {
			return []drainer{reader[body, HookAll](en), reader[velocity, HookAll](en)}
		}},
		{"other-Store-watched", func(en *Entities) []drainer { return []drainer{reader[collider, HookAll](en)} }},
	}
	for _, arm := range arms {
		b.Run(arm.name, func(b *testing.B) {
			w := newHookBench(hookPopulation)
			w.populate(hookPopulation)
			readers := arm.readers(w.en)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				w.en.despawn(w.spawn(body{X: 1}, velocity{X: 1}))
				if i&1023 == 1023 {
					b.StopTimer()
					for _, r := range readers {
						r.beginRun()
						r.endRun()
					}
					b.StartTimer()
				}
			}
		})
	}
}

// BenchmarkHookReader prices a reader's run over records already in its log:
// fixing the copy, iterating it, and the reset. ns/op is per record produced.
func BenchmarkHookReader(b *testing.B) {
	const records = 1024
	for _, shape := range []string{"add-remove-pairs", "additions", "changes", "changes-twice"} {
		for _, readers := range []int{1, 4} {
			b.Run(fmt.Sprintf("%s/%d-readers", shape, readers), func(b *testing.B) {
				w := newHookBench(hookPopulation)
				ents := w.populate(hookPopulation)
				hs := make([]*Hooks[body, HookAll], readers)
				w.en.hooks.preparing = 1
				for i := range hs {
					hs[i] = &Hooks[body, HookAll]{writer: 1}
					hs[i].bind(w.en)
				}
				w.en.hooks.preparing = 2
				writer := snapshotFor[body](w.en)
				var sum float32
				b.ResetTimer()
				for i := 0; i < b.N; i += records {
					b.StopTimer()
					for j := range records {
						e := ents[(i+j)%len(ents)]
						switch shape {
						case "add-remove-pairs":
							if j&1 == 0 {
								w.bodies.Remove(e)
							} else {
								w.bodies.Set(e, body{X: 1})
							}
						case "additions":
							w.bodies.Remove(e)
						case "changes", "changes-twice":
							row, _ := w.bodies.probe(e)
							writer.take(e, row)
							w.bodies.dense[row].X++
						}
					}
					writer.finish()
					if shape == "additions" {
						for _, h := range hs {
							h.beginRun()
							h.endRun()
						}
						for j := range records {
							w.bodies.Set(ents[(i+j)%len(ents)], body{X: 1})
						}
					}
					if shape == "changes-twice" {
						for j := range records {
							e := ents[(i+j)%len(ents)]
							row, _ := w.bodies.probe(e)
							writer.take(e, row)
							w.bodies.dense[row].X++
						}
						writer.finish()
					}
					b.StartTimer()
					for _, h := range hs {
						h.beginRun()
						for _, hook := range h.All() {
							sum += hook.Value.X
						}
						h.endRun()
					}
					if shape == "add-remove-pairs" {
						b.StopTimer()
						for j := range records {
							if j&1 == 0 {
								w.bodies.Set(ents[(i+j)%len(ents)], body{X: 1})
							}
						}
						for _, h := range hs {
							h.beginRun()
							h.endRun()
						}
						b.StartTimer()
					}
				}
				_ = sum
			})
		}
	}
}

type bigRow struct {
	Name  string
	X, Y  float32
	Z     [4]float64
	Flags uint64
}

// BenchmarkChangedWriter prices #268's content contract on a writer walking a
// watched Store: ns/op is per row walked, whole run (snapshot, writes, compare).
func BenchmarkChangedWriter(b *testing.B) {
	for _, n := range []int{1_000, 10_000} {
		for _, percent := range []int{0, 1, 10, 100} {
			for _, arm := range []string{"unwatched", "per-row", "whole-store", "explicit-call"} {
				b.Run(fmt.Sprintf("body8B/%d/%d%%/%s", n, percent, arm), func(b *testing.B) {
					benchmarkChangedWriter(b, n, percent, arm, func(s *Store[body], row int) { s.dense[row].X++ })
				})
				b.Run(fmt.Sprintf("bigRow64B/%d/%d%%/%s", n, percent, arm), func(b *testing.B) {
					benchmarkChangedWriter(b, n, percent, arm, func(s *Store[bigRow], row int) { s.dense[row].Z[3]++ })
				})
			}
		}
	}
}

func benchmarkChangedWriter[T any](b *testing.B, n, percent int, arm string, write func(*Store[T], int)) {
	en := newEntities(uint32(n))
	en.hookHub()
	store := benchStore[T](en, uint32(n))
	for range n {
		var zero T
		store.Set(en.alloc(), zero)
	}
	var snap *rowSnapshot
	if arm != "unwatched" {
		(&Hooks[T, HookAddedChanged]{}).bind(en)
		snap = snapshotFor[T](en)
	}
	every := n + 1
	if percent > 0 {
		every = 100 / percent
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i += n {
		if arm == "whole-store" {
			snap.takeAll()
		}
		for row := n - 1; row >= 0; row-- {
			if arm == "per-row" {
				snap.take(store.owners[row], uint32(row))
			}
			if row%every == 0 {
				write(store, row)
				if arm == "explicit-call" {
					snap.recordChangeAtCall(store.owners[row])
				}
			}
		}
		switch arm {
		case "per-row", "whole-store":
			snap.finish()
			snap.log.records = snap.log.records[:0]
		case "explicit-call":
			snap.run++
			snap.log.records = snap.log.records[:0]
		}
	}
}

// BenchmarkChangedRef prices Set.Ref on 1% of a watched Store's rows.
func BenchmarkChangedRef(b *testing.B) {
	const n = 10_000
	for _, arm := range []string{"unwatched", "per-row"} {
		b.Run(arm, func(b *testing.B) {
			en := newEntities(n)
			en.hookHub()
			store := benchStore[body](en, n)
			ents := make([]Entity, n)
			for i := range ents {
				ents[i] = en.alloc()
				store.Set(ents[i], body{})
			}
			var snap *rowSnapshot
			if arm != "unwatched" {
				(&Hooks[body, HookAddedChanged]{}).bind(en)
				snap = snapshotFor[body](en)
			}
			b.ReportAllocs()
			b.ResetTimer()
			const touched = n / 100
			for i := 0; i < b.N; i += touched {
				for j := range touched {
					e := ents[(j*97+i)%n]
					row, _ := store.probe(e)
					if snap != nil {
						snap.take(e, row)
					}
					store.dense[row].X++
				}
				if snap != nil {
					snap.finish()
					snap.log.records = snap.log.records[:0]
				}
			}
		})
	}
}

// --- Whole frames, through the kernel ---

type (
	hookMoveSystem   kernel.Subscription[app.UpdateEvent]
	hookChurnSystem  kernel.Subscription[app.UpdateEvent]
	hookReaderSystem kernel.Subscription[app.UpdateEvent]
)

type walkQ struct{ H *homing }

// frameArm is a whole-frame configuration: which logs to plan before any System
// is prepared (#382's ordering question, sidestepped), and what to subscribe.
type frameArm struct {
	plan      func(en *Entities)
	subscribe func(r *kernel.Registrar)
}

const churnPerTick = 100

// churn walks its own Component, so nothing but a lock Hooks add could
// serialise it against move, then adds or removes a collider on 100 Entities.
func churn(ents *[]Entity, tick *int) func(*Set[collider], *Remove[collider], *Query[walkQ]) {
	return func(set *Set[collider], remove *Remove[collider], q *Query[walkQ]) {
		for _, it := range q.All() {
			it.H.Target++
		}
		if len(*ents) == 0 {
			for e := range q.All() {
				*ents = append(*ents, e)
			}
		}
		*tick++
		step := len(*ents) / churnPerTick
		for i := range churnPerTick {
			e := (*ents)[i*step+(*tick/2)%step]
			if *tick&1 == 0 {
				set.UpdateFor(e, collider{Radius: 1})
			} else {
				remove.From(e)
			}
		}
	}
}

func spawner(sp *Spawn[spawnSet], we *WriteableEntities) {
	for range churnPerTick {
		we.Despawn(sp.New(spawnSet{Body: body{X: 1}, Velocity: velocity{X: 1}}))
	}
}

func hookFrameArms() map[string]frameArm {
	moveSub := func(r *kernel.Registrar) {
		r.Subscribe[hookMoveSystem](ToHandler[app.UpdateEvent](r, move))
	}
	churnSub := func(r *kernel.Registrar) {
		var ents []Entity
		var tick int
		r.Subscribe[hookChurnSystem](ToHandler[app.UpdateEvent](r, churn(&ents, &tick)))
	}
	spawnSub := func(r *kernel.Registrar) {
		r.Subscribe[hookChurnSystem](ToHandler[app.UpdateEvent](r, spawner))
	}
	read := func(system any) func(r *kernel.Registrar) {
		return func(r *kernel.Registrar) {
			r.Subscribe[hookReaderSystem](ToHandler[app.UpdateEvent](r, system))
		}
	}
	both := func(fs ...func(*kernel.Registrar)) func(*kernel.Registrar) {
		return func(r *kernel.Registrar) {
			for _, f := range fs {
				f(r)
			}
		}
	}
	noPlan := func(*Entities) {}
	plan := func(f func(en *Entities)) func(*Entities) { return func(en *Entities) { en.hookHub(); f(en) } }
	return map[string]frameArm{
		"move+empty":          {noPlan, both(moveSub, read(func() {}))},
		"move+changed-reader": {plan(func(en *Entities) { logFor[body](en).watch |= kindChanged }), both(moveSub, read(func(h *Hooks[body, HookAddedChanged]) {}))},
		"churn+empty":         {noPlan, both(churnSub, read(func() {}))},
		"churn+reader":        {plan(func(en *Entities) { logFor[collider](en) }), both(churnSub, read(func(h *Hooks[collider, HookAddedRemoved]) {}))},
		"churn+move+empty":    {noPlan, both(churnSub, moveSub, read(func() {}))},
		"churn+move+reader":   {plan(func(en *Entities) { logFor[collider](en) }), both(churnSub, moveSub, read(func(h *Hooks[collider, HookAddedRemoved]) {}))},
		"spawn+empty":         {noPlan, both(spawnSub, read(func() {}))},
		"spawn+reader":        {plan(func(en *Entities) { logFor[body](en) }), both(spawnSub, read(func(h *Hooks[body, HookSpawnedDespawned]) {}))},
	}
}

func (arm frameArm) world(tb testing.TB, n int) *kernel.Engine {
	entities, components, engine := newWorld(tb, uint32(n), func(r *kernel.Registrar) {
		arm.plan(r.Dependency[*Entities]())
		arm.subscribe(r)
	})
	populate(entities, components, n)
	for _, e := range components.bodies.owners {
		components.homings.Set(e, homing{})
	}
	return engine
}

var hookFrameNames = []string{
	"move+empty", "move+changed-reader", "churn+empty", "churn+reader",
	"churn+move+empty", "churn+move+reader", "spawn+empty", "spawn+reader",
}

func BenchmarkHookFrame(b *testing.B) {
	arms := hookFrameArms()
	for _, n := range []int{1_000, 10_000} {
		for _, name := range hookFrameNames {
			b.Run(fmt.Sprintf("%s/%d", name, n), func(b *testing.B) {
				engine := arms[name].world(b, n)
				executioner := engine.Executioner()
				for range 100 {
					frame(b, engine, 1)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if err := executioner.PublishEvent(app.UpdateEvent{Dt: 1}).Wait(); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

// TestHookFramesStayOnTheAllocationLine is requirement 6: a frame with a reader
// costs what the same frame with an empty System in its place costs.
func TestHookFramesStayOnTheAllocationLine(t *testing.T) {
	const frames = 5_000
	arms := hookFrameArms()
	measure := func(name string, n int) float64 {
		engine := arms[name].world(t, n)
		executioner := engine.Executioner()
		for range 300 {
			frame(t, engine, 1)
		}
		mallocs := allocationsDuring(func() {
			for range frames {
				if err := executioner.PublishEvent(app.UpdateEvent{Dt: 1}).Wait(); err != nil {
					t.Fatal(err)
				}
			}
		})
		return float64(mallocs) / frames
	}
	control := map[string]string{
		"move+changed-reader": "move+empty", "churn+reader": "churn+empty",
		"churn+move+reader": "churn+move+empty", "spawn+reader": "spawn+empty",
	}
	got := map[string][2]float64{}
	for _, name := range hookFrameNames {
		a, b := measure(name, 1_000), measure(name, 10_000)
		got[name] = [2]float64{a, b}
		t.Logf("%-22s objects a frame: 1k %.3f, 10k %.3f", name, a, b)
		if b > a+0.05 {
			t.Errorf("%s allocates per Entity: %.3f at 1k, %.3f at 10k", name, a, b)
		}
	}
	for name, base := range control {
		if got[name][1] > got[base][1]+0.05 {
			t.Errorf("%s costs %.3f objects a frame against %s's %.3f", name, got[name][1], base, got[base][1])
		}
	}
}

func TestHookFramesDeliver(t *testing.T) {
	counts := map[string]int{}
	run := func(arm frameArm) {
		engine := arm.world(t, 1_000)
		for range 11 {
			frame(t, engine, 1)
		}
	}
	moveSub := func(r *kernel.Registrar) { r.Subscribe[hookMoveSystem](ToHandler[app.UpdateEvent](r, move)) }
	churnSub := func(r *kernel.Registrar) {
		var ents []Entity
		var tick int
		r.Subscribe[hookChurnSystem](ToHandler[app.UpdateEvent](r, churn(&ents, &tick)))
	}
	run(frameArm{func(en *Entities) { en.hookHub(); logFor[body](en).watch |= kindChanged }, func(r *kernel.Registrar) {
		moveSub(r)
		r.Subscribe[hookReaderSystem](ToHandler[app.UpdateEvent](r, func(h *Hooks[body, HookAddedChanged]) {
			for range h.All() {
				counts["changed"]++
			}
		})).After[hookMoveSystem]()
	}})
	run(frameArm{func(en *Entities) { en.hookHub(); logFor[collider](en) }, func(r *kernel.Registrar) {
		churnSub(r)
		moveSub(r)
		r.Subscribe[hookReaderSystem](ToHandler[app.UpdateEvent](r, func(h *Hooks[collider, HookAddedRemoved]) {
			for range h.All() {
				counts["membership"]++
			}
		})).After[hookChurnSystem]()
	}})
	run(frameArm{func(en *Entities) { en.hookHub(); logFor[body](en) }, func(r *kernel.Registrar) {
		r.Subscribe[hookChurnSystem](ToHandler[app.UpdateEvent](r, spawner))
		r.Subscribe[hookReaderSystem](ToHandler[app.UpdateEvent](r, func(h *Hooks[body, HookSpawnedDespawned]) {
			for range h.All() {
				counts["spawn"]++
			}
		})).After[hookChurnSystem]()
	}})
	t.Logf("records over 11 frames at 1k: %v", counts)
	if counts["changed"] != 11_000 || counts["membership"] != 1_000 || counts["spawn"] != 2_200 {
		t.Fatalf("records over 11 frames: %v, want changed 11000, membership 1000, spawn 2200", counts)
	}
}
