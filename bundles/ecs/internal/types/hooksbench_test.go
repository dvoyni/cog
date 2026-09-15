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
	hookMoveSystem    kernel.Subscription[app.UpdateEvent]
	hookChurnSystem   kernel.Subscription[app.UpdateEvent]
	hookReaderSystem  kernel.Subscription[app.UpdateEvent]
	hookSpawnerSystem kernel.Subscription[app.UpdateEvent]
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

// spawnsPerTick is how many Entities spawner spawns each tick, despawning the
// ones it spawned the tick before.
const spawnsPerTick = 100

// spawner despawns the Entities it spawned last tick and spawns as many again,
// each carrying body and velocity.
func spawner() func(sp *Spawn[spawnSet], we *WriteableEntities) {
	live := make([]Entity, 0, spawnsPerTick)
	return func(sp *Spawn[spawnSet], we *WriteableEntities) {
		for _, e := range live {
			we.Despawn(e)
		}
		live = live[:0]
		for range spawnsPerTick {
			live = append(live, sp.New(spawnSet{Body: body{X: 1}, Velocity: velocity{X: 1}}))
		}
	}
}

func subscribeSpawner(registrar *kernel.Registrar) {
	registrar.Subscribe[hookSpawnerSystem](ToHandler[app.UpdateEvent](registrar, spawner()))
}

// hookFrame is one whole-frame arm: the Systems, and what sits in the reader's
// place — a Hooks reader counting what it is given, or, where reader is nil, an
// empty System, which is the control, because the engine charges per
// subscription.
type hookFrame struct {
	name      string
	subscribe func(*kernel.Registrar)
	reader    func(delivered *int) any
}

// colliderReader is the membership reader churn feeds.
func colliderReader(delivered *int) any {
	return func(h *Hooks[collider, HookAddedRemoved]) {
		for range h.All() {
			*delivered++
		}
	}
}

// lifetimeReader is the reader spawner feeds.
func lifetimeReader(delivered *int) any {
	return func(h *Hooks[body, HookSpawnedDespawned]) {
		for range h.All() {
			*delivered++
		}
	}
}

var hookFrames = []hookFrame{
	{"churn+empty", subscribeChurn, nil},
	{"churn+reader", subscribeChurn, colliderReader},
	{"churn+move+empty", subscribeChurnAndMove, nil},
	{"churn+move+reader", subscribeChurnAndMove, colliderReader},
	{"spawn+empty", subscribeSpawner, nil},
	{"spawn+reader", subscribeSpawner, lifetimeReader},
}

// world composes the arm at n Entities. The count is how many records the
// reader has been given, so a test can see the arm is recording.
func (arm hookFrame) world(tb testing.TB, n int) (*kernel.Engine, *int) {
	tb.Helper()
	delivered := new(int)
	entities, components, engine := newWorld(tb, uint32(n), func(registrar *kernel.Registrar) {
		arm.subscribe(registrar)
		if arm.reader != nil {
			registrar.Subscribe[hookReaderSystem](ToHandler[app.UpdateEvent](registrar, arm.reader(delivered)))
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
			if arm.reader != nil && *delivered < churnPerTick*frames {
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
	for _, pair := range [][2]string{
		{"churn+reader", "churn+empty"}, {"churn+move+reader", "churn+move+empty"}, {"spawn+reader", "spawn+empty"},
	} {
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

// BenchmarkHookNothingWatchingSpawnTwoFields is a Spawn carrying two
// Components on a world no reader watches: what recording costs a Spawn nobody
// reads. It is BenchmarkSpawnTwoFields under the name hooks.md publishes it by,
// and that one is its A/B partner against the parent of the first Hooks commit.
func BenchmarkHookNothingWatchingSpawnTwoFields(b *testing.B) { BenchmarkSpawnTwoFields(b) }

// BenchmarkHookNothingWatchingDespawnSixStores is a Despawn on a world no reader
// watches, where the components plugin has enrolled six Stores: what recording
// costs a Despawn nobody reads. It is BenchmarkDespawnOnly under the name
// hooks.md publishes it by, and that one is its A/B partner.
func BenchmarkHookNothingWatchingDespawnSixStores(b *testing.B) { BenchmarkDespawnOnly(b) }

// BenchmarkHookSpawnDespawn prices a Spawn carrying body and velocity plus its
// Despawn, with readers watching Stores the Component set carries and one it
// does not, against the same pair with nothing watching. The readers run every
// 1024 pairs, off the clock, so the logs stay at their steady size.
func BenchmarkHookSpawnDespawn(b *testing.B) {
	arms := []struct {
		name   string
		reader any
	}{
		{"none", func() {}},
		{"carried/body-HookSpawnedDespawned", func(h *Hooks[body, HookSpawnedDespawned]) {}},
		{"carried/body-HookAll", func(h *Hooks[body, HookAll]) {}},
		{"carried/body+velocity-HookAll", func(h *Hooks[body, HookAll], v *Hooks[velocity, HookAll]) {}},
		{"not-carried/collider-HookAll", func(h *Hooks[collider, HookAll]) {}},
	}
	for _, arm := range arms {
		b.Run(arm.name, func(b *testing.B) {
			var spawn *Spawn[spawnSet]
			var writeable *WriteableEntities
			_, _, engine := newWorld(b, spawnBatch, func(registrar *kernel.Registrar) {
				registrar.Subscribe[hookCaptureSystem](ToHandler[app.UpdateEvent](registrar,
					func(sp *Spawn[spawnSet], we *WriteableEntities) { spawn, writeable = sp, we }))
				registrar.Subscribe[hookReaderSystem](ToHandler[app.UpdateEvent](registrar, arm.reader))
			})
			frame(b, engine, 1)
			values := spawnSet{Body: body{X: 1}, Velocity: velocity{X: 2}}
			pair := func() { writeable.Despawn(spawn.New(values)) }
			for range 1024 {
				pair()
			}
			frame(b, engine, 1)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				pair()
				if i&1023 == 1023 {
					b.StopTimer()
					frame(b, engine, 1)
					b.StartTimer()
				}
			}
		})
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

// BenchmarkHookFrame is each frame with a reader, against the same frame with an
// empty System in the reader's place: churn + move + reader at 10k is budgeted
// at 1.10x its control, and spawn + reader is the 100 spawns plus despawns a
// tick with a HookSpawnedDespawned reader.
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
