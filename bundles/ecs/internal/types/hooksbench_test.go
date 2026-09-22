package types

import (
	"fmt"
	"strings"
	"testing"
	"time"

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
	hookReader2System kernel.Subscription[app.UpdateEvent]
	hookReader3System kernel.Subscription[app.UpdateEvent]
	hookReader4System kernel.Subscription[app.UpdateEvent]
	hookSpawnerSystem kernel.Subscription[app.UpdateEvent]
)

// subscribeReader subscribes the i-th System in a frame's reader places, each
// under its own subscription type.
func subscribeReader(registrar *kernel.Registrar, i int, system any) {
	switch i {
	case 0:
		registrar.Subscribe[hookReaderSystem](ToHandler[app.UpdateEvent](registrar, system))
	case 1:
		registrar.Subscribe[hookReader2System](ToHandler[app.UpdateEvent](registrar, system))
	case 2:
		registrar.Subscribe[hookReader3System](ToHandler[app.UpdateEvent](registrar, system))
	default:
		registrar.Subscribe[hookReader4System](ToHandler[app.UpdateEvent](registrar, system))
	}
}

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

// hookFrame is one whole-frame arm: the Systems, and what sits in each of the
// reader places — a Hooks reader counting what it is given, or, where reader is
// nil, an empty System, which is the control, because the engine charges per
// subscription. readers is how many places there are, 1 when it is 0; each
// reader counts into its own cell, because readers of one Store run together.
type hookFrame struct {
	name      string
	subscribe func(*kernel.Registrar)
	reader    func(delivered *int) any
	readers   int
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

// changeReader is the reader move feeds, whose every body changes every tick:
// the published worst case.
func changeReader(delivered *int) any {
	return func(h *Hooks[body, HookAddedChanged]) {
		for range h.All() {
			*delivered++
		}
	}
}

// mirrorReader reads every kind of act on body.
func mirrorReader(delivered *int) any {
	return func(h *Hooks[body, HookAll]) {
		for range h.All() {
			*delivered++
		}
	}
}

// churnMirrorReader watches collider for everything, Changed included, while
// churn adds and removes it.
func churnMirrorReader(delivered *int) any {
	return func(h *Hooks[collider, HookAll]) {
		for range h.All() {
			*delivered++
		}
	}
}

var hookFrames = []hookFrame{
	{"churn+empty", subscribeChurn, nil, 1},
	{"churn+reader", subscribeChurn, colliderReader, 1},
	{"churn+changed-reader", subscribeChurn, churnMirrorReader, 1},
	{"churn+move+empty", subscribeChurnAndMove, nil, 1},
	{"churn+move+reader", subscribeChurnAndMove, colliderReader, 1},
	{"spawn+empty", subscribeSpawner, nil, 1},
	{"spawn+reader", subscribeSpawner, lifetimeReader, 1},
	{"move+empty", subscribeMove, nil, 1},
	{"move+changed-reader", subscribeMove, changeReader, 1},
	{"move+4-empty", subscribeMove, nil, 4},
	{"move+4-readers-all", subscribeMove, mirrorReader, 4},
}

// world composes the arm at n Entities. The count is how many records the
// readers have been given, so a test can see the arm is recording.
func (arm hookFrame) world(tb testing.TB, n int) (*kernel.Engine, func() int) {
	tb.Helper()
	delivered := make([]int, max(arm.readers, 1))
	entities, components, engine := newWorld(tb, uint32(n), func(registrar *kernel.Registrar) {
		arm.subscribe(registrar)
		for i := range delivered {
			if arm.reader != nil {
				subscribeReader(registrar, i, arm.reader(&delivered[i]))
			} else {
				subscribeReader(registrar, i, func() {})
			}
		}
	})
	populate(entities, components, n)
	for _, e := range components.bodies.owners {
		components.homings.Set(e, homing{})
	}
	return engine, func() int {
		total := 0
		for _, count := range delivered {
			total += count
		}
		return total
	}
}

// TestAHookReaderStaysOnTheAllocationLine is hooks.md's allocation line: a frame
// with a reader allocates what the same frame with an empty System in its place
// does, and the same at 10k Entities as at 1k, so nothing a reader or a
// recording writer does allocates per record or per Entity.
func TestAHookReaderStaysOnTheAllocationLine(t *testing.T) {
	if raceEnabled {
		t.Skip("allocation counts are not meaningful under -race")
	}
	const frames = 5_000
	measure := func(arm hookFrame, n int) float64 {
		engine, delivered := arm.world(t, n)
		executioner := engine.Executioner()
		for range 100 {
			frame(t, engine, 1)
		}
		defer func() {
			if arm.reader != nil && delivered() < churnPerTick*frames {
				t.Errorf("%s at %d delivered %d records over %d frames, want at least %d",
					arm.name, n, delivered(), frames+100, churnPerTick*frames)
			}
		}()
		mallocs := allocationsDuring(func() {
			for range frames {
				executioner.PublishEvent(app.UpdateEvent{Dt: 1}).Wait()
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
		{"churn+reader", "churn+empty"}, {"churn+changed-reader", "churn+empty"},
		{"churn+move+reader", "churn+move+empty"}, {"spawn+reader", "spawn+empty"},
		{"move+changed-reader", "move+empty"}, {"move+4-readers-all", "move+4-empty"},
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
// Store read under each kind set, by one reader and by four, against the same
// pair on the same world with an empty System in the reader's place. The
// readers run every 1024 pairs, off the clock, so the log stays at its steady
// size.
func BenchmarkHookAddRemove(b *testing.B) {
	b.Run("nothing-watching", func(b *testing.B) { benchmarkHookAddRemove(b, func() {}) })
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
			b.Run(a.kinds+"/"+readers.name, func(b *testing.B) { benchmarkHookAddRemove(b, readers.system) })
		}
	}
}

func benchmarkHookAddRemove(b *testing.B, readers any) {
	handles, ids, engine := recordingWorld(b, accessorBatch, readers)
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
}

// BenchmarkHookAddRemoveString is BenchmarkHookAddRemove's pair on a Component
// holding a string, whose removal's retained copy is a non-trivial T, with one
// reader under HookAddedRemoved and HookAll against nothing watching.
func BenchmarkHookAddRemoveString(b *testing.B) {
	arms := []struct {
		name   string
		reader any
	}{
		{"nothing-watching", func() {}},
		{"HookAddedRemoved/1-reader", func(h *Hooks[namedComponent, HookAddedRemoved]) {}},
		{"HookAll/1-reader", func(h *Hooks[namedComponent, HookAll]) {}},
	}
	for _, arm := range arms {
		b.Run(arm.name, func(b *testing.B) {
			var set *Set[namedComponent]
			var remove *Remove[namedComponent]
			entities, _, engine := newWorldWith(b, accessorBatch, func(registrar *kernel.Registrar) {
				registrar.Subscribe[hookCaptureSystem](ToHandler[app.UpdateEvent](registrar,
					func(s *Set[namedComponent], r *Remove[namedComponent]) { set, remove = s, r }))
				registrar.Subscribe[hookReaderSystem](ToHandler[app.UpdateEvent](registrar, arm.reader))
			}, []kernel.PluginName{Name, "components", "hookowner"}, &hookOwnerPlugin{})
			frame(b, engine, 1)
			ids := make([]Entity, accessorBatch)
			for i := range ids {
				ids[i] = entities.alloc()
			}
			pair := func(i int) {
				e := ids[i%accessorBatch]
				set.UpdateFor(e, namedComponent{Name: "a voice's emitter"})
				remove.From(e)
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
					executioner.PublishEvent(app.UpdateEvent{Dt: 1}).Wait()
				}
			})
		}
	}
}

// BenchmarkHookShrink is ShrinkCmd's zero request after a spike on a watched
// Store: n Entities gain collider, every row is changed through Ref and then
// through a Query, all but one in a thousand are despawned, and two HookAll
// readers read all of it. Each op makes its spike first, so ns/op is the
// Command alone, timed around its execution through the kernel; the bytes each
// area released are reported per op. ShrinkCmd is opt-in, so it has no budget.
func BenchmarkHookShrink(b *testing.B) {
	for _, n := range []int{1_000, 10_000} {
		b.Run(fmt.Sprintf("%d", n), func(b *testing.B) {
			w := newHookSpikeWorld(b)
			var survivors []Entity
			var took time.Duration
			var released ShrinkResponse
			for i := 0; i < b.N; i++ {
				gone := survivors
				w.written(b, func(_ *Query[colliderQuery], _ *Set[collider], we *WriteableEntities) {
					for _, e := range gone {
						we.Despawn(e)
					}
				})
				survivors = w.spike(b, n)
				start := time.Now()
				op := w.shrink(b, ShrinkRequest{})
				took += time.Since(start)
				released.Hooks += op.Hooks
				released.Stores += op.Stores
				released.Entities += op.Entities
				released.Scratch += op.Scratch
			}
			b.ReportMetric(float64(took.Nanoseconds())/float64(b.N), "ns/op")
			b.ReportMetric(float64(released.Hooks)/float64(b.N), "hooks-B/op")
			b.ReportMetric(float64(released.Stores)/float64(b.N), "stores-B/op")
			b.ReportMetric(float64(released.Entities)/float64(b.N), "entities-B/op")
			b.ReportMetric(float64(released.Scratch)/float64(b.N), "scratch-B/op")
		})
	}
}

// hookWatchSystem is the System BenchmarkHookWatchCheck runs by hand.
type hookWatchSystem kernel.Subscription[app.UpdateEvent]

// watchWriteQuery is a Query writing two Stores, so its bind has two *T fields
// to check.
type watchWriteQuery struct {
	Body     *body
	Velocity *velocity
}

// BenchmarkHookWatchCheck is a writer's run start on Stores no reader watches:
// one run of a System, called by hand outside the kernel, whose body does
// nothing but bind what it holds. A handle is a *T Query field, a Set, a Remove
// or a field of a Spawn's Component set, and each pays one load and one bit
// test per run. The arm compiles unchanged against the parent of the first
// Hooks commit, which has no check, so the A/B difference divided by the arm's
// handles is the check's cost per handle per run, budgeted at 1 ns; 0-handles
// is what a System naming no writer handle pays.
func BenchmarkHookWatchCheck(b *testing.B) {
	arms := []struct {
		name   string
		system any
	}{
		{"0-handles", func() {}},
		{"2-handles/query-write-fields", func(q *Query[watchWriteQuery]) {
			for range q.All() {
			}
		}},
		{"2-handles/spawn-fields", func(sp *Spawn[spawnSet]) {}},
		{"12-handles/set-and-remove-on-six-stores", func(
			_ *Set[body], _ *Remove[body], _ *Set[velocity], _ *Remove[velocity],
			_ *Set[collider], _ *Remove[collider], _ *Set[homing], _ *Remove[homing],
			_ *Set[disabled], _ *Remove[disabled], _ *Set[solid], _ *Remove[solid],
		) {
		}},
	}
	for _, arm := range arms {
		b.Run(arm.name, func(b *testing.B) {
			var run kernel.Observe[app.UpdateEvent]
			_, _, engine := newWorld(b, 16, func(registrar *kernel.Registrar) {
				handler := ToHandler[app.UpdateEvent](registrar, arm.system)
				registrar.Subscribe[hookWatchSystem](func() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
					lock, observe := handler()
					run = observe
					return lock, observe
				})
			})
			frame(b, engine, 1)
			event := app.UpdateEvent{Dt: 1}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				run(kernel.Kernel{}, event)
			}
		})
	}
}

// BenchmarkHookPaceCounter is Validation mode's pace counter on one log: a
// run's start and end on the counter, around one append, against the append
// alone. The difference is its cost per counted run per log. A release build
// compiles the counter out through const validate, so the arm runs only under
// -tags ecs_validate.
func BenchmarkHookPaceCounter(b *testing.B) {
	if !validate {
		b.Skip("the pace counter runs only in a validating build")
	}
	for _, arm := range []string{"append-alone", "counted-run"} {
		b.Run(arm, func(b *testing.B) {
			var set *Set[collider]
			entities, _, engine := newWorld(b, 16, func(registrar *kernel.Registrar) {
				registrar.Subscribe[hookCaptureSystem](ToHandler[app.UpdateEvent](registrar, func(s *Set[collider]) { set = s }))
				registrar.Subscribe[hookReaderSystem](ToHandler[app.UpdateEvent](registrar, func(h *Hooks[collider, HookAll]) {}))
			})
			frame(b, engine, 1)
			store := set.changes.store
			log := store.hooks
			var pace systemPace
			pace.enrol(store)
			pace.begin(entities)
			pace.end()
			e := entities.alloc()
			counted := arm == "counted-run"
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if counted {
					pace.begin(entities)
				}
				log.changed(e, 1)
				if counted {
					pace.end()
				}
				if i&1023 == 1023 {
					log.base += uint64(len(log.records))
					log.records = log.records[:0]
				}
			}
		})
	}
}

// BenchmarkHookFirstWatchedRun is the allocation a writer's first run on a
// Store watched for Changed makes, and the none every later run makes, at 10k
// Entities of an 8 B Component: a *T Query field's whole-Store copy, Set.Ref on
// 1% of the rows, and Set.MarkChanged on 1%. The first arm gives the copy's
// buffers back before each op, off the clock, as ShrinkCmd does, so every op is
// a first watched run; B/op is its allocation.
func BenchmarkHookFirstWatchedRun(b *testing.B) {
	const n, touched = 10_000, 100
	for _, route := range []string{"query-field", "ref-1%", "mark-1%"} {
		for _, when := range []string{"first", "later"} {
			b.Run(route+"/"+when, func(b *testing.B) {
				w := changedWriterWorld[body](b, n, true)
				run := func(i int) {
					switch route {
					case "query-field":
						for _, it := range w.q.All() {
							it.Row.X++
						}
					case "ref-1%":
						for j := range touched {
							ref, _ := w.set.Ref(w.ids[(j*97+i)%n])
							ref.X++
						}
					default:
						for j := range touched {
							w.set.MarkChanged(w.ids[(j*97+i)%n])
						}
					}
					w.runEnd()
				}
				run(0)
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if when == "first" {
						b.StopTimer()
						w.set.changes.release()
						b.StartTimer()
					}
					run(i)
				}
			})
		}
	}
}

// row64 is a 64-byte Component with no padding, for the wide half of the
// Changed writer grid; body is the 8-byte half.
type row64 struct {
	A, B, C, D, E, F, G, H float64
}

// changedRowsPlugin owns row64.
type changedRowsPlugin struct{ ids uint32 }

func (changedRowsPlugin) Name() kernel.PluginName { return "changedrows" }

func (changedRowsPlugin) Dependencies() []kernel.PluginName { return []kernel.PluginName{Name} }

func (p changedRowsPlugin) Register(registrar *kernel.Registrar, _ any) error {
	RegisterComponent[row64](registrar, p.ids)
	return nil
}

// changedWriter is a writer of T captured out of its System, so a benchmark can
// run it outside a frame: a *T Query field and Set, which share one row copy,
// and the Store.
type changedWriter[T any] struct {
	q     *Query[fieldOf[T]]
	set   *Set[T]
	store *Store[T]
	ids   []Entity
}

// changedWriterWorld populates n Entities with T and captures a writer of T.
// When watched, a HookAddedChanged reader of T is registered; it runs once, in
// the frame that arms the writer's gates, and never again, so a benchmark
// empties the log itself.
func changedWriterWorld[T any](b *testing.B, n int, watched bool) *changedWriter[T] {
	b.Helper()
	w := &changedWriter[T]{}
	entities, _, engine := newWorldWith(b, uint32(n), func(registrar *kernel.Registrar) {
		registrar.Subscribe[hookCaptureSystem](ToHandler[app.UpdateEvent](registrar, func(q *Query[fieldOf[T]], set *Set[T]) {
			w.q, w.set = q, set
		}))
		if watched {
			registrar.Subscribe[hookReaderSystem](ToHandler[app.UpdateEvent](registrar, func(h *Hooks[T, HookAddedChanged]) {}))
		}
	}, []kernel.PluginName{Name, "components", "changedrows"}, changedRowsPlugin{ids: uint32(n)})
	w.ids = make([]Entity, n)
	var zero T
	var store *Store[T]
	for i := range w.ids {
		w.ids[i] = entities.alloc()
	}
	frame(b, engine, 1)
	store = w.set.store.Get()
	for _, e := range w.ids {
		store.Set(e, zero)
	}
	w.store = store
	return w
}

// runEnd is the writer's run end on its Store, off a frame: the compare, then
// the log emptied as a reader would leave it.
func (w *changedWriter[T]) runEnd() {
	w.set.changes.compare()
	if w.store.hooks != nil {
		w.store.hooks.records = w.store.hooks.records[:0]
	}
}

// BenchmarkHookChangedWriter is the Changed writer grid: a writer walking a
// 10k-Entity Store and writing 0, 1, 10 or 100% of its rows, 8 B and 64 B wide,
// through a *T Query field (the whole-Store copy) or through Ref on every row
// walked (the copy per row), each against the same walk with nothing watching.
// ns/op is per row walked, the run end's compare included.
func BenchmarkHookChangedWriter(b *testing.B) {
	const n = 10_000
	for _, width := range []string{"8B", "64B"} {
		for _, percent := range []int{0, 1, 10, 100} {
			for _, arm := range []string{"query-unwatched", "query-whole-store", "ref-unwatched", "ref-per-row"} {
				b.Run(fmt.Sprintf("%s/%d%%/%s", width, percent, arm), func(b *testing.B) {
					watched := arm == "query-whole-store" || arm == "ref-per-row"
					byQuery := strings.HasPrefix(arm, "query")
					if width == "8B" {
						benchmarkChangedWriter(b, n, percent, watched, byQuery, func(p *body) { p.X++ })
					} else {
						benchmarkChangedWriter(b, n, percent, watched, byQuery, func(p *row64) { p.H++ })
					}
				})
			}
		}
	}
}

func benchmarkChangedWriter[T any](b *testing.B, n, percent int, watched, byQuery bool, write func(*T)) {
	w := changedWriterWorld[T](b, n, watched)
	every := n + 1
	if percent > 0 {
		every = 100 / percent
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i += n {
		row := 0
		if byQuery {
			for _, it := range w.q.All() {
				if row%every == 0 {
					write(it.Row)
				}
				row++
			}
		} else {
			for _, e := range w.ids {
				ref, _ := w.set.Ref(e)
				if row%every == 0 {
					write(ref)
				}
				row++
			}
		}
		w.runEnd()
	}
}

// BenchmarkHookChangedRef is Set.Ref on 1% of a 10k-Entity Store, writing each
// row it reaches, watched for Changed and not. ns/op is per Ref, the run end's
// compare included.
func BenchmarkHookChangedRef(b *testing.B) {
	const n, touched = 10_000, 100
	for _, arm := range []string{"unwatched", "watched"} {
		b.Run(arm, func(b *testing.B) {
			w := changedWriterWorld[body](b, n, arm == "watched")
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i += touched {
				for j := range touched {
					ref, _ := w.set.Ref(w.ids[(j*97+i)%n])
					ref.X++
				}
				w.runEnd()
			}
		})
	}
}

// BenchmarkHookReaderChanges prices a reader's run per Changed record it takes:
// raw, one change per Entity per window, and folded, two writer runs changing
// the same Entities so each window's second change folds into its first. The
// records are made off the clock, 1024 Entities at a time. ns/op is per record
// in the log, so the folded arm's reader delivers half of what it is charged
// for.
func BenchmarkHookReaderChanges(b *testing.B) {
	const entities = 1024
	for _, shape := range []string{"raw", "folded"} {
		for _, readers := range []int{1, 4} {
			b.Run(fmt.Sprintf("%s/%d-readers", shape, readers), func(b *testing.B) {
				var hooks []*Hooks[body, HookAll]
				var reader any = func(h *Hooks[body, HookAll]) { hooks = append(hooks[:0], h) }
				if readers == 4 {
					reader = func(h0, h1, h2, h3 *Hooks[body, HookAll]) { hooks = append(hooks[:0], h0, h1, h2, h3) }
				}
				var set *Set[body]
				population, components, engine := newWorld(b, 2*entities, func(registrar *kernel.Registrar) {
					registrar.Subscribe[hookCaptureSystem](ToHandler[app.UpdateEvent](registrar, func(s *Set[body]) { set = s }))
					registrar.Subscribe[hookReaderSystem](ToHandler[app.UpdateEvent](registrar, reader))
				})
				ids := make([]Entity, 2*entities)
				for i := range ids {
					ids[i] = population.alloc()
					components.bodies.Set(ids[i], body{})
				}
				frame(b, engine, 1)
				produce := func(batch int) {
					for j := range entities {
						ref, _ := set.Ref(ids[(batch+j)%len(ids)])
						ref.X++
					}
					set.changes.compare()
				}
				perRecord := entities
				if shape == "folded" {
					perRecord = 2 * entities
				}
				var sum float32
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i += perRecord {
					b.StopTimer()
					produce(i)
					if shape == "folded" {
						produce(i)
					}
					b.StartTimer()
					for _, h := range hooks {
						h.beginRun()
						for _, hook := range h.All() {
							sum += hook.Value.X
						}
						h.endRun()
					}
				}
				_ = sum
			})
		}
	}
}
