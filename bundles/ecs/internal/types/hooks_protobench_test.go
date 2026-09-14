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

type (
	q1 struct {
		C collider
		_ Entered
		_ Exited
	}
	q2 struct {
		B body
		C collider
		_ Entered
		_ Exited
	}
	q2n struct {
		N named
		C collider
		_ Entered
		_ Exited
	}
	q2b struct {
		V velocity
		C collider
		_ Entered
		_ Exited
	}
	q2c struct {
		B body
		V velocity
		C collider
		_ Entered
		_ Exited
	}
	q2d struct {
		B body
		C collider
		_ Without[disabled]
		_ Entered
		_ Exited
	}
	q2x struct {
		B body
		C collider
		_ Changed
	}
)

type drainer interface {
	beginRun()
	endRun()
}

func newReader[Q any](en *Entities) drainer {
	h := &Hooks[Q]{}
	h.bind(en)
	return h
}

// BenchmarkHookAddRemove prices one UpdateFor that adds and one Remove.From, on
// a Store some Hooks[Q] names. Readers drain every 1024 pairs, off the clock.
func BenchmarkHookAddRemove(b *testing.B) {
	arms := []struct {
		name    string
		readers func(en *Entities) []drainer
	}{
		{"none", func(*Entities) []drainer { return nil }},
		{"Q1-one-store", func(en *Entities) []drainer { return []drainer{newReader[q1](en)} }},
		{"Q2-two-stores", func(en *Entities) []drainer { return []drainer{newReader[q2](en)} }},
		{"Q2-string-values", func(en *Entities) []drainer { return []drainer{newReader[q2n](en)} }},
		{"Q2-changed-only", func(en *Entities) []drainer { return []drainer{newReader[q2x](en)} }},
		{"4-Qs", func(en *Entities) []drainer {
			return []drainer{newReader[q2](en), newReader[q2b](en), newReader[q2c](en), newReader[q2d](en)}
		}},
		{"Q2-4-readers", func(en *Entities) []drainer {
			return []drainer{newReader[q2](en), &Hooks[q2]{}, &Hooks[q2]{}, &Hooks[q2]{}}
		}},
	}
	for _, arm := range arms {
		b.Run(arm.name, func(b *testing.B) {
			w := newHookBench(hookPopulation)
			ents := w.populate(hookPopulation)
			readers := arm.readers(w.en)
			for _, r := range readers {
				if h, ok := r.(*Hooks[q2]); ok && h.log == nil {
					h.bind(w.en)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				e := ents[i%len(ents)]
				w.colliders.Set(e, collider{Radius: 1})
				w.colliders.Remove(e)
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
	// lazy is #379's fallback: record under the act's own lock only — Entity,
	// gained or lost, and the removed row — and resolve at the reader's start.
	b.Run("lazy-Q2", func(b *testing.B) {
		w := newHookBench(hookPopulation)
		ents := w.populate(hookPopulation)
		type lazyRecord struct {
			e      Entity
			seq    uint64
			gained bool
		}
		var hub hookHub
		var records []lazyRecord
		var rows []collider
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			e := ents[i%len(ents)]
			w.colliders.Set(e, collider{Radius: 1})
			records = append(records, lazyRecord{e, hub.seq.Add(1), true})
			row, _ := w.colliders.probe(e)
			rows = append(rows, w.colliders.dense[row])
			records = append(records, lazyRecord{e, hub.seq.Add(1), false})
			w.colliders.Remove(e)
			if i&1023 == 1023 {
				records, rows = records[:0], rows[:0]
			}
		}
	})
}

type (
	sq struct {
		B body
		V velocity
		_ Spawned
		_ Despawned
	}
	sqm struct {
		B body
		V velocity
		_ Entered
		_ Exited
		_ Changed
	}
	sqn struct {
		N named
		_ Spawned
		_ Despawned
	}
)

// BenchmarkHookSpawnDespawn prices a whole spawn and a whole despawn.
func BenchmarkHookSpawnDespawn(b *testing.B) {
	arms := []struct {
		name    string
		readers func(en *Entities) []drainer
	}{
		{"none", func(*Entities) []drainer { return nil }},
		{"Q-spawned-despawned", func(en *Entities) []drainer { return []drainer{newReader[sq](en)} }},
		{"Q-membership-changed", func(en *Entities) []drainer { return []drainer{newReader[sqm](en)} }},
		{"Q-unmatched", func(en *Entities) []drainer { return []drainer{newReader[sqn](en)} }},
		{"4-Qs", func(en *Entities) []drainer {
			return []drainer{newReader[sq](en), newReader[sqm](en), newReader[q2](en), newReader[sqn](en)}
		}},
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
// fixing the copy, iterating it, and the reset. ns/op is per record.
func BenchmarkHookReader(b *testing.B) {
	for _, records := range []int{64, 1024} {
		for _, shape := range []string{"entries", "enter-exit", "changes", "changes-folded"} {
			for _, readers := range []int{1, 4} {
				b.Run(fmt.Sprintf("%s/%d-records/%d-readers", shape, records, readers), func(b *testing.B) {
					w := newHookBench(hookPopulation)
					ents := w.populate(hookPopulation)
					hs := make([]*Hooks[q2x], readers)
					w.en.hookHub().preparing = 1
					for i := range hs {
						hs[i] = &Hooks[q2x]{}
						hs[i].bind(w.en)
					}
					w.en.hooks.preparing = 2
					writer := snapshotFor[body](w.en)
					for _, e := range ents {
						w.colliders.Set(e, collider{})
					}
					for _, h := range hs {
						h.beginRun()
						h.endRun()
					}
					var sum float32
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i += records {
						b.StopTimer()
						for j := range records {
							e := ents[(i+j)%len(ents)]
							switch shape {
							case "entries":
								w.colliders.Remove(e)
								w.colliders.Set(e, collider{})
							case "enter-exit":
								if j&1 == 0 {
									w.colliders.Remove(e)
								} else {
									w.colliders.Set(e, collider{})
								}
							case "changes", "changes-folded":
								row, _ := w.bodies.probe(e)
								writer.take(e, row)
								w.bodies.dense[row].X++
							}
						}
						if shape == "changes-folded" {
							// Every change again, a second writer run: folded away.
							for j := range records {
								e := ents[(i+j)%len(ents)]
								row, _ := w.bodies.probe(e)
								writer.finish()
								writer.take(e, row)
								w.bodies.dense[row].X++
							}
						}
						writer.finish()
						// Remove the "entries" setup's own exits from the price: re-run so
						// only entries remain in the window being measured.
						b.StartTimer()
						for _, h := range hs {
							h.beginRun()
							for _, hook := range h.All() {
								sum += hook.Values.B.X
							}
							h.endRun()
						}
						if shape == "enter-exit" {
							b.StopTimer()
							for j := range records {
								e := ents[(i+j)%len(ents)]
								if j&1 == 0 {
									w.colliders.Set(e, collider{})
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

type (
	watchBody struct {
		B body
		_ Changed
	}
	watchBig struct {
		R bigRow
		_ Changed
	}
)

func benchmarkChangedWriter[T any](b *testing.B, n, percent int, arm string, write func(*Store[T], int)) {
	en := newEntities(uint32(n))
	store := benchStore[T](en, uint32(n))
	for range n {
		var zero T
		store.Set(en.alloc(), zero)
	}
	var snap *rowSnapshot
	if arm != "unwatched" {
		switch any(store).(type) {
		case *Store[body]:
			(&Hooks[watchBody]{}).bind(en)
		case *Store[bigRow]:
			(&Hooks[watchBig]{}).bind(en)
		}
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
			snap.hooks.changes = snap.hooks.changes[:0]
		case "explicit-call":
			snap.run++
			snap.hooks.changes = snap.hooks.changes[:0]
		}
	}
}

// BenchmarkChangedRef prices Set.Ref on 1% of a watched Store's rows: the sparse
// writer, where snapshotting the whole Store would be the wrong shape.
func BenchmarkChangedRef(b *testing.B) {
	const n = 10_000
	for _, arm := range []string{"unwatched", "per-row", "whole-store"} {
		b.Run(arm, func(b *testing.B) {
			en := newEntities(n)
			store := benchStore[body](en, n)
			ents := make([]Entity, n)
			for i := range ents {
				ents[i] = en.alloc()
				store.Set(ents[i], body{})
			}
			var snap *rowSnapshot
			if arm != "unwatched" {
				(&Hooks[watchBody]{}).bind(en)
				snap = snapshotFor[body](en)
			}
			b.ReportAllocs()
			b.ResetTimer()
			const touched = n / 100
			for i := 0; i < b.N; i += touched {
				if arm == "whole-store" {
					snap.takeAll()
				}
				for j := range touched {
					e := ents[(j*97+i)%n]
					row, _ := store.probe(e)
					if arm == "per-row" {
						snap.take(e, row)
					}
					store.dense[row].X++
				}
				if snap != nil {
					snap.finish()
					snap.hooks.changes = snap.hooks.changes[:0]
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
	hookWalkSystem   kernel.Subscription[app.UpdateEvent]
)

type (
	frameColliderQ struct {
		C collider
		_ Entered
		_ Exited
	}
	frameBodyColQ struct {
		B body
		C collider
		_ Entered
		_ Exited
	}
	frameChangedQ struct {
		B body
		_ Changed
	}
	frameSpawnQ struct {
		B body
		V velocity
		_ Spawned
		_ Despawned
	}
	walkQ struct{ H *homing }
)

// frameArm is a whole-frame configuration: which logs to plan before any System
// is prepared (#382's ordering question, sidestepped), and what to subscribe.
type frameArm struct {
	plan      func(en *Entities)
	subscribe func(r *kernel.Registrar)
}

const churnPerTick = 100

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
	reader := func(read any) func(r *kernel.Registrar) {
		return func(r *kernel.Registrar) {
			r.Subscribe[hookReaderSystem](ToHandler[app.UpdateEvent](r, read))
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
	return map[string]frameArm{
		"move":                  {noPlan, moveSub},
		"move+changed-reader":   {func(en *Entities) { hookLogFor[frameChangedQ](en) }, both(moveSub, reader(func(h *Hooks[frameChangedQ]) {}))},
		"churn":                 {noPlan, churnSub},
		"churn+Q1-reader":       {func(en *Entities) { hookLogFor[frameColliderQ](en) }, both(churnSub, reader(func(h *Hooks[frameColliderQ]) {}))},
		"churn+Q2-reader":       {func(en *Entities) { hookLogFor[frameBodyColQ](en) }, both(churnSub, reader(func(h *Hooks[frameBodyColQ]) {}))},
		"churn+move":            {noPlan, both(churnSub, moveSub)},
		"churn+move+Q2-widened": {func(en *Entities) { hookLogFor[frameBodyColQ](en) }, both(churnSub, moveSub, reader(func(h *Hooks[frameBodyColQ]) {}))},
		"churn+move+Q2-unwidened": {func(en *Entities) {
			hookLogFor[frameBodyColQ](en)
			en.hooks.noWiden = true
		}, both(churnSub, moveSub, reader(func(h *Hooks[frameBodyColQ]) {}))},
		"move+empty":       {noPlan, both(moveSub, reader(func() {}))},
		"churn+empty":      {noPlan, both(churnSub, reader(func() {}))},
		"churn+move+empty": {noPlan, both(churnSub, moveSub, reader(func() {}))},
		"spawn+empty":      {noPlan, both(func(r *kernel.Registrar) { r.Subscribe[hookChurnSystem](ToHandler[app.UpdateEvent](r, spawner)) }, reader(func() {}))},
		"spawn":            {noPlan, func(r *kernel.Registrar) { r.Subscribe[hookChurnSystem](ToHandler[app.UpdateEvent](r, spawner)) }},
		"spawn+Q-reader":   {func(en *Entities) { hookLogFor[frameSpawnQ](en) }, both(func(r *kernel.Registrar) { r.Subscribe[hookChurnSystem](ToHandler[app.UpdateEvent](r, spawner)) }, reader(func(h *Hooks[frameSpawnQ]) {}))},
	}
}

func (arm frameArm) world(tb testing.TB, n int) *kernel.Engine {
	entities, components, engine := newWorld(tb, uint32(n), func(r *kernel.Registrar) {
		arm.plan(r.Dependency[*Entities]())
		arm.subscribe(r)
	})
	populate(entities, components, n)
	// churn walks its own Component, so only the widened read can serialise it
	// against move.
	for _, e := range components.bodies.owners {
		components.homings.Set(e, homing{})
	}
	return engine
}

var hookFrameNames = []string{
	"move+empty", "move+changed-reader", "churn+empty", "churn+Q1-reader", "churn+Q2-reader",
	"churn+move+empty", "churn+move+Q2-widened", "churn+move+Q2-unwidened", "spawn+empty", "spawn+Q-reader",
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

// TestHookFramesStayOnTheAllocationLine is requirement 6: steady-state frames
// with a reader cost what the same frame without one costs, at 1k and 10k.
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
	// Each arm with a reader is held to the same frame with an empty System in
	// the reader's place: the engine charges per subscription, not the ECS.
	control := map[string]string{
		"move+changed-reader": "move+empty", "churn+Q1-reader": "churn+empty", "churn+Q2-reader": "churn+empty",
		"churn+move+Q2-widened": "churn+move+empty", "churn+move+Q2-unwidened": "churn+move+empty", "spawn+Q-reader": "spawn+empty",
	}
	got := map[string][2]float64{}
	for _, name := range hookFrameNames {
		a, b := measure(name, 1_000), measure(name, 10_000)
		got[name] = [2]float64{a, b}
		t.Logf("%-26s objects a frame: 1k %.3f, 10k %.3f", name, a, b)
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
	count := func(name string) func(kinds string) { return func(string) { counts[name]++ } }
	run := func(name string, plan func(*Entities), sub func(*kernel.Registrar)) {
		engine := frameArm{plan, sub}.world(t, 1_000)
		for range 11 {
			frame(t, engine, 1)
		}
	}
	both := func(fs ...func(*kernel.Registrar)) func(*kernel.Registrar) {
		return func(r *kernel.Registrar) {
			for _, f := range fs {
				f(r)
			}
		}
	}
	moveSub := func(r *kernel.Registrar) { r.Subscribe[hookMoveSystem](ToHandler[app.UpdateEvent](r, move)) }
	churnSub := func(r *kernel.Registrar) {
		var ents []Entity
		var tick int
		r.Subscribe[hookChurnSystem](ToHandler[app.UpdateEvent](r, churn(&ents, &tick)))
	}
	c := count("changed")
	run("changed", func(en *Entities) { hookLogFor[frameChangedQ](en) }, both(moveSub, func(r *kernel.Registrar) {
		r.Subscribe[hookReaderSystem](ToHandler[app.UpdateEvent](r, func(h *Hooks[frameChangedQ]) {
			for range h.All() {
				c("")
			}
		})).After[hookMoveSystem]()
	}))
	m := count("membership")
	run("membership", func(en *Entities) { hookLogFor[frameBodyColQ](en) }, both(churnSub, func(r *kernel.Registrar) {
		r.Subscribe[hookReaderSystem](ToHandler[app.UpdateEvent](r, func(h *Hooks[frameBodyColQ]) {
			for range h.All() {
				m("")
			}
		})).After[hookChurnSystem]()
	}))
	s := count("spawn")
	run("spawn", func(en *Entities) { hookLogFor[frameSpawnQ](en) }, both(func(r *kernel.Registrar) {
		r.Subscribe[hookChurnSystem](ToHandler[app.UpdateEvent](r, spawner))
	}, func(r *kernel.Registrar) {
		r.Subscribe[hookReaderSystem](ToHandler[app.UpdateEvent](r, func(h *Hooks[frameSpawnQ]) {
			for range h.All() {
				s("")
			}
		})).After[hookChurnSystem]()
	}))
	t.Logf("records over 11 frames: %v", counts)
}
