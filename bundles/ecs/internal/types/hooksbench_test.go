package types

import (
	"fmt"
	"testing"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// What a Hook costs, beside framebench_test.go and spawnbench_test.go. The
// budgets in hooks.md § What it costs are checked by hand against the parent of
// the first Hooks commit, with interleaved A/B runs; timings are published and
// never tested. What can be counted exactly is tested here: the allocation line
// a frame with a reader sits on.

type (
	hookMoveSystem   kernel.Subscription[app.UpdateEvent]
	hookChurnSystem  kernel.Subscription[app.UpdateEvent]
	hookReaderSystem kernel.Subscription[app.UpdateEvent]
)

// walkQuery is churn's own Component, so nothing but a lock a Hook added could
// serialise churn against move.
type walkQuery struct{ Homing *homing }

// churnPerTick is how many Entities churn adds a collider to, or takes one from,
// each tick.
const churnPerTick = 100

// churn walks its own Component, then adds a collider to 100 Entities on one
// tick and takes it away on the next. It writes collider and homing; move
// writes body.
func churn() func(set *Set[collider], remove *Remove[collider], q *Query[walkQuery]) {
	var population []Entity
	tick := 0
	return func(set *Set[collider], remove *Remove[collider], q *Query[walkQuery]) {
		if population == nil {
			for e := range q.All() {
				population = append(population, e)
			}
		}
		for e, it := range q.All() {
			it.Homing.Target = e
		}
		tick++
		step := len(population) / churnPerTick
		if step == 0 {
			return
		}
		for i := range churnPerTick {
			e := population[i*step+(tick/2)%step]
			if tick&1 == 1 {
				set.UpdateFor(e, collider{Radius: 1})
			} else {
				remove.From(e)
			}
		}
	}
}

func subscribeChurn(registrar *kernel.Registrar) {
	registrar.Subscribe[hookChurnSystem](ToHandler[app.UpdateEvent](registrar, churn()))
}

func subscribeChurnAndMove(registrar *kernel.Registrar) {
	subscribeChurn(registrar)
	registrar.Subscribe[hookMoveSystem](ToHandler[app.UpdateEvent](registrar, move))
}

// hookFrame is one whole-frame arm: the Systems, and what sits in the reader's
// place — a Hooks[collider, HookAddedRemoved] reader, or an empty System, which
// is the control, because the engine charges per subscription.
type hookFrame struct {
	name      string
	subscribe func(*kernel.Registrar)
	reader    bool
}

var hookFrames = []hookFrame{
	{"churn+empty", subscribeChurn, false},
	{"churn+reader", subscribeChurn, true},
	{"churn+move+empty", subscribeChurnAndMove, false},
	{"churn+move+reader", subscribeChurnAndMove, true},
}

// world composes the arm at n Entities. The count is how many records the
// reader has been given, so a test can see the arm is recording.
func (arm hookFrame) world(tb testing.TB, n int) (*kernel.Engine, *int) {
	tb.Helper()
	delivered := new(int)
	entities, components, engine := newWorld(tb, uint32(n), func(registrar *kernel.Registrar) {
		arm.subscribe(registrar)
		if arm.reader {
			registrar.Subscribe[hookReaderSystem](ToHandler[app.UpdateEvent](registrar,
				func(h *Hooks[collider, HookAddedRemoved]) {
					for range h.All() {
						*delivered++
					}
				}))
		} else {
			registrar.Subscribe[hookReaderSystem](ToHandler[app.UpdateEvent](registrar, func() {}))
		}
	})
	populate(entities, components, n)
	for _, e := range components.bodies.owners {
		components.homings.Set(e, homing{})
	}
	return engine, delivered
}

// TestAHookReaderStaysOnTheAllocationLine is hooks.md's allocation line: a frame
// with a reader allocates what the same frame with an empty System in its place
// does, and the same at 10k Entities as at 1k, so nothing a reader or a
// recording writer does allocates per record or per Entity.
func TestAHookReaderStaysOnTheAllocationLine(t *testing.T) {
	const frames = 5_000
	measure := func(arm hookFrame, n int) float64 {
		engine, delivered := arm.world(t, n)
		executioner := engine.Executioner()
		for range 100 {
			frame(t, engine, 1)
		}
		defer func() {
			if arm.reader && *delivered < churnPerTick*frames {
				t.Errorf("%s at %d delivered %d records over %d frames, want at least %d",
					arm.name, n, *delivered, frames+100, churnPerTick*frames)
			}
		}()
		mallocs := allocationsDuring(func() {
			for range frames {
				if err := executioner.PublishEvent(app.UpdateEvent{Dt: 1}).Wait(); err != nil {
					t.Fatalf("publishing the update: %v", err)
				}
			}
		})
		return float64(mallocs) / frames
	}

	got := map[string][2]float64{}
	for _, arm := range hookFrames {
		got[arm.name] = [2]float64{measure(arm, 1_000), measure(arm, 10_000)}
		t.Logf("%-18s objects a frame: 1k %.3f, 10k %.3f", arm.name, got[arm.name][0], got[arm.name][1])
	}
	for _, pair := range [][2]string{{"churn+reader", "churn+empty"}, {"churn+move+reader", "churn+move+empty"}} {
		reader, control := got[pair[0]], got[pair[1]]
		if reader[1] > reader[0]+0.05 {
			t.Errorf("%s allocates per Entity: %.3f a frame at 1k, %.3f at 10k", pair[0], reader[0], reader[1])
		}
		for i, n := range []string{"1k", "10k"} {
			if reader[i] > control[i]+0.05 {
				t.Errorf("%s costs %.3f objects a frame at %s against %s's %.3f", pair[0], reader[i], n, pair[1], control[i])
			}
		}
	}
}

// BenchmarkHookNothingWatching is an UpdateFor that adds plus a Remove.From on a
// Store no reader watches: the whole of what recording costs a writer that
// nobody reads. It is BenchmarkSetUpdateForInsertingAndRemoving under the name
// hooks.md publishes it by, and that one is its A/B partner against the parent
// of the first Hooks commit.
func BenchmarkHookNothingWatching(b *testing.B) {
	world, ids := accessorPopulation(b, accessorBatch)
	for _, e := range ids {
		world.colliderSet.UpdateFor(e, collider{Radius: 1})
		world.colliderRemove.From(e)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e := ids[i%accessorBatch]
		world.colliderSet.UpdateFor(e, collider{Radius: 1})
		world.colliderRemove.From(e)
	}
}

type hookHandles struct {
	set    *Set[collider]
	remove *Remove[collider]
}

// recordingWorld is a world whose collider Store is read by readers, each a
// Hooks parameter of one System, with the writer handles captured out of another
// so a benchmark can call them outside a frame. A frame runs every reader.
func recordingWorld(tb testing.TB, n int, reader any) (*hookHandles, []Entity, *kernel.Engine) {
	tb.Helper()
	handles := &hookHandles{}
	entities, _, engine := newWorld(tb, uint32(n), func(registrar *kernel.Registrar) {
		registrar.Subscribe[hookCaptureSystem](ToHandler[app.UpdateEvent](registrar, func(s *Set[collider], r *Remove[collider]) {
			handles.set, handles.remove = s, r
		}))
		registrar.Subscribe[hookReaderSystem](ToHandler[app.UpdateEvent](registrar, reader))
	})
	frame(tb, engine, 1)
	ids := make([]Entity, n)
	for i := range ids {
		ids[i] = entities.alloc()
	}
	return handles, ids, engine
}

// hookReaderArm is, for one kind set, a System with one reader of collider and
// one with four.
type hookReaderArm struct {
	kinds     string
	one, four any
}

func hookReaders[K KindSet](kinds string) hookReaderArm {
	return hookReaderArm{kinds, func(h *Hooks[collider, K]) {}, func(a, b, c, d *Hooks[collider, K]) {}}
}

// BenchmarkHookAddRemove prices an UpdateFor that adds plus a Remove.From on a
// Store read under each kind set, by one reader and by four. The readers run
// every 1024 pairs, off the clock, so the log stays at its steady size.
func BenchmarkHookAddRemove(b *testing.B) {
	arms := []hookReaderArm{
		hookReaders[HookSpawned]("HookSpawned"),
		hookReaders[HookDespawned]("HookDespawned"),
		hookReaders[HookSpawnedDespawned]("HookSpawnedDespawned"),
		hookReaders[HookAdded]("HookAdded"),
		hookReaders[HookRemoved]("HookRemoved"),
		hookReaders[HookAddedRemoved]("HookAddedRemoved"),
		hookReaders[HookAddedChanged]("HookAddedChanged"),
		hookReaders[HookAll]("HookAll"),
	}
	for _, a := range arms {
		for _, readers := range []struct {
			name   string
			system any
		}{{"1-reader", a.one}, {"4-readers", a.four}} {
			b.Run(a.kinds+"/"+readers.name, func(b *testing.B) {
				handles, ids, engine := recordingWorld(b, accessorBatch, readers.system)
				pair := func(i int) {
					e := ids[i%accessorBatch]
					handles.set.UpdateFor(e, collider{Radius: 1})
					handles.remove.From(e)
				}
				for i := range accessorBatch {
					pair(i)
				}
				frame(b, engine, 1)
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					pair(i)
					if i&1023 == 1023 {
						b.StopTimer()
						frame(b, engine, 1)
						b.StartTimer()
					}
				}
			})
		}
	}
}

// BenchmarkHookReader prices a reader's run per record it is given: taking,
// folding and filling its copy, iterating it, and the run end. The records are
// made off the clock, 1024 at a time: add-remove pairs, whose additions are
// filled from the removal, or additions alone, filled from the live Store.
func BenchmarkHookReader(b *testing.B) {
	const records = 1024
	for _, shape := range []string{"add-remove-pairs", "additions"} {
		for _, n := range []int{1, 4} {
			b.Run(fmt.Sprintf("%s/%d-readers", shape, n), func(b *testing.B) {
				var readers []*Hooks[collider, HookAddedRemoved]
				var reader any = func(h *Hooks[collider, HookAddedRemoved]) { readers = append(readers[:0], h) }
				if n == 4 {
					reader = func(h0, h1, h2, h3 *Hooks[collider, HookAddedRemoved]) {
						readers = append(readers[:0], h0, h1, h2, h3)
					}
				}
				handles, ids, engine := recordingWorld(b, 2*records, reader)
				// Additions alone need the Entities to lose collider between
				// batches, which the readers see as removals made off the clock.
				produce := func(batch int) {
					for j := range records {
						e := ids[(batch+j)%len(ids)]
						handles.set.UpdateFor(e, collider{Radius: float32(j)})
						if shape == "add-remove-pairs" {
							handles.remove.From(e)
						}
					}
				}
				drain := func() {
					for _, h := range readers {
						h.beginRun()
						h.endRun()
					}
				}
				unproduce := func(batch int) {
					for j := range records {
						handles.remove.From(ids[(batch+j)%len(ids)])
					}
				}
				produce(0)
				frame(b, engine, 1)
				if shape == "additions" {
					unproduce(0)
					drain()
				}
				perRecord := records
				if shape == "add-remove-pairs" {
					perRecord = 2 * records
				}
				var sum float32
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i += perRecord {
					b.StopTimer()
					produce(i)
					b.StartTimer()
					for _, h := range readers {
						h.beginRun()
						for _, hook := range h.All() {
							sum += hook.Value.Radius
						}
						h.endRun()
					}
					if shape == "additions" {
						b.StopTimer()
						unproduce(i)
						drain()
						b.StartTimer()
					}
				}
				_ = sum
			})
		}
	}
}

// BenchmarkHookFrame is the parallel frame with a reader, against the same frame
// with an empty System in the reader's place: churn + move + reader at 10k is
// budgeted at 1.10x its control.
func BenchmarkHookFrame(b *testing.B) {
	for _, n := range []int{1_000, 10_000} {
		for _, arm := range hookFrames {
			b.Run(fmt.Sprintf("%s/%d", arm.name, n), func(b *testing.B) {
				engine, _ := arm.world(b, n)
				executioner := engine.Executioner()
				for range 100 {
					frame(b, engine, 1)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if err := executioner.PublishEvent(app.UpdateEvent{Dt: 1}).Wait(); err != nil {
						b.Fatalf("publishing the update: %v", err)
					}
				}
			})
		}
	}
}
