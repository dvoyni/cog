package types

import (
	"testing"

	"github.com/dvoyni/cog/kernel"
)

// What ShrinkCmd gives back of what Hooks hold: hooks.md § Memory goes back
// only when the app asks. Every System here is a command, so a test decides
// exactly which System runs, and whether it has run, when the shrink executes.

type (
	spikeWriteCmd kernel.Command[hookRequest, hookResponse]
	spikeReadCmd  kernel.Command[hookRequest, hookResponse]
	spikeLateCmd  kernel.Command[hookRequest, hookResponse]
)

// hookSpikeWorld is a world with one writer of collider holding every route a
// Hook records through, and two readers watching every kind: the reader, which
// runs after each spike, and the late reader, which runs only when a test says.
type hookSpikeWorld struct {
	entities   *Entities
	components *componentsPlugin
	engine     *kernel.Engine
	write      func(q *Query[colliderQuery], set *Set[collider], we *WriteableEntities)
	// reader and late are the two readers' parameters, and changes the writer's
	// row copy of collider, each caught from inside its System.
	reader, late     *Hooks[collider, HookAll]
	changes          *rowCopy
	heard, lateHeard []heard
}

func newHookSpikeWorld(tb testing.TB) *hookSpikeWorld {
	tb.Helper()
	w := &hookSpikeWorld{}
	w.entities, w.components, w.engine = newWorld(tb, 16, func(registrar *kernel.Registrar) {
		registrar.HandleCommand[spikeWriteCmd](ToExecute[hookRequest, hookResponse](registrar,
			func(q *Query[colliderQuery], set *Set[collider], we *WriteableEntities) {
				w.changes = set.changes
				w.write(q, set, we)
			}))
		registrar.HandleCommand[spikeReadCmd](ToExecute[hookRequest, hookResponse](registrar,
			func(h *Hooks[collider, HookAll]) {
				w.reader, w.heard = h, w.heard[:0]
				listen(h, &w.heard, radius)
			}))
		registrar.HandleCommand[spikeLateCmd](ToExecute[hookRequest, hookResponse](registrar,
			func(h *Hooks[collider, HookAll]) {
				w.late, w.lateHeard = h, w.lateHeard[:0]
				listen(h, &w.lateHeard, radius)
			}))
	})
	return w
}

func (w *hookSpikeWorld) run(tb testing.TB, execute func(kernel.Executioner) error) {
	tb.Helper()
	if err := execute(w.engine.Executioner()); err != nil {
		tb.Fatalf("running a System: %v", err)
	}
}

func (w *hookSpikeWorld) written(tb testing.TB, write func(q *Query[colliderQuery], set *Set[collider], we *WriteableEntities)) {
	tb.Helper()
	w.write = write
	w.run(tb, func(x kernel.Executioner) error {
		x.ExecuteCommand[spikeWriteCmd](hookRequest{})
		return nil
	})
}

func (w *hookSpikeWorld) read(tb testing.TB) {
	tb.Helper()
	w.run(tb, func(x kernel.Executioner) error {
		x.ExecuteCommand[spikeReadCmd](hookRequest{})
		return nil
	})
}

func (w *hookSpikeWorld) readLate(tb testing.TB) {
	tb.Helper()
	w.run(tb, func(x kernel.Executioner) error {
		x.ExecuteCommand[spikeLateCmd](hookRequest{})
		return nil
	})
}

func (w *hookSpikeWorld) shrink(tb testing.TB, request ShrinkRequest) ShrinkResponse {
	tb.Helper()
	response := w.engine.Executioner().ExecuteCommand[shrinkCmd](request)
	return response
}

// spike is a spike of additions, changes and removals on collider, which both
// readers watch: n Entities gain it, every row is written through Ref and then
// through a Query and then marked changed, and all but spikeSurvivor's few are despawned. The reader
// runs after each act, so its copy reaches the spike's size, and the late
// reader runs at the end, so the log is compacted to empty and holds only its
// capacity. It returns the survivors.
func (w *hookSpikeWorld) spike(tb testing.TB, n int) []Entity {
	tb.Helper()
	ids := make([]Entity, n)
	for i := range ids {
		ids[i] = w.entities.alloc()
	}
	w.written(tb, func(_ *Query[colliderQuery], set *Set[collider], _ *WriteableEntities) {
		for i, e := range ids {
			set.UpdateFor(e, collider{Radius: float32(i)})
		}
	})
	w.read(tb)
	w.written(tb, func(_ *Query[colliderQuery], set *Set[collider], _ *WriteableEntities) {
		for _, e := range ids {
			if row, ok := set.Ref(e); ok {
				row.Radius++
			}
		}
	})
	w.read(tb)
	w.written(tb, func(q *Query[colliderQuery], _ *Set[collider], _ *WriteableEntities) {
		for _, it := range q.All() {
			it.Collider.Radius++
		}
	})
	w.read(tb)
	w.written(tb, func(_ *Query[colliderQuery], set *Set[collider], _ *WriteableEntities) {
		for _, e := range ids {
			set.MarkChanged(e)
		}
	})
	w.read(tb)
	var survivors []Entity
	w.written(tb, func(_ *Query[colliderQuery], _ *Set[collider], we *WriteableEntities) {
		for i, e := range ids {
			if spikeSurvivor(i) {
				survivors = append(survivors, e)
				continue
			}
			we.Despawn(e)
		}
	})
	w.read(tb)
	w.readLate(tb)
	return survivors
}

// hookCapacity is what the Hooks area holds, as the capacities a shrink cuts:
// the log's records and retained values, and the reader's copy, its fills and
// its fold marks.
type hookCapacity struct{ records, retained, out, fills, marks int }

func (w *hookSpikeWorld) hookCapacity() hookCapacity {
	log, h := w.components.colliders.hooks, w.reader
	return hookCapacity{cap(log.records), cap(log.retained), cap(h.out), cap(h.fills), cap(h.marks)}
}

// copyCapacity is what the writer's row copy of collider holds, its marks
// included.
type copyCapacity struct{ owners, rows, stamps, marks, marked int }

func (w *hookSpikeWorld) copyCapacity() copyCapacity {
	c := w.changes
	return copyCapacity{cap(c.owners), cap(c.rows), cap(c.stamps), cap(c.marks), cap(c.marked)}
}

func TestTheZeroRequestGivesBackWhatHooksHold(t *testing.T) {
	w := newHookSpikeWorld(t)
	w.spike(t, spikePeak)
	if got := w.hookCapacity(); got.records < spikePeak || got.out < spikePeak || got.marks < spikePeak {
		t.Fatalf("the Hooks area holds %+v after the spike, want every array at least %d: the spike is not measuring it", got, spikePeak)
	}
	if got := w.copyCapacity(); got.owners < spikePeak || got.stamps < spikePeak || got.marks < spikePeak || got.marked < spikePeak {
		t.Fatalf("the row copy holds %+v after the spike, want at least %d: the spike is not measuring it", got, spikePeak)
	}

	released := w.shrink(t, ShrinkRequest{})

	if released.Hooks == 0 || released.Scratch == 0 {
		t.Fatalf("the zero request after a spike on watched Stores released %+v, want Hooks and Scratch above 0", released)
	}
	// Every reader has passed every record, so each array's length is 0 and a
	// shrink to its length leaves nothing.
	if got := w.hookCapacity(); got != (hookCapacity{}) {
		t.Fatalf("the Hooks area holds %+v after the zero request, want nothing: every record was read", got)
	}
	if got := w.copyCapacity(); got != (copyCapacity{}) {
		t.Fatalf("the row copy holds %+v after the zero request, want nothing", got)
	}
	if again := w.shrink(t, ShrinkRequest{}); again.Hooks != 0 || again.Scratch != 0 {
		t.Fatalf("a second shrink released %+v, want 0 Hooks and Scratch bytes: both were already at their length", again)
	}
}

func TestKeepHooksReleasesNothingFromAHookLogOrAReader(t *testing.T) {
	w := newHookSpikeWorld(t)
	w.spike(t, spikePeak)
	before := w.hookCapacity()

	released := w.shrink(t, ShrinkRequest{KeepHooks: true})

	if released.Hooks != 0 {
		t.Fatalf("KeepHooks released %d Hooks bytes, want 0", released.Hooks)
	}
	if after := w.hookCapacity(); after != before {
		t.Fatalf("KeepHooks changed the Hooks area from %+v to %+v", before, after)
	}
	if after := w.copyCapacity(); after != (copyCapacity{}) {
		t.Fatalf("KeepHooks kept the row copy at %+v: a row copy is Scratch, not Hooks", after)
	}
}

func TestKeepScratchReleasesNothingFromARowCopy(t *testing.T) {
	w := newHookSpikeWorld(t)
	w.spike(t, spikePeak)
	before := w.copyCapacity()

	released := w.shrink(t, ShrinkRequest{KeepScratch: true})

	if released.Scratch != 0 {
		t.Fatalf("KeepScratch released %d Scratch bytes, want 0", released.Scratch)
	}
	if after := w.copyCapacity(); after != before {
		t.Fatalf("KeepScratch changed the row copy from %+v to %+v", before, after)
	}
	if released.Hooks == 0 {
		t.Fatalf("KeepScratch kept the Hooks area too: it released %+v", released)
	}
}

// TestRecordsNotYetReadSurviveAShrink is a shrink between a reader that has run
// and one that has not. The log holds the late reader's records at a length far
// below the spike's capacity, so the shrink moves them into new arrays, and the
// late reader still receives every one, in order, with its value: an addition's
// from the live Store, a Despawn's retained at the act, and two changes. The
// runs after it regrow the row copy and the readers' copies from nothing, and
// deliver as before.
func TestRecordsNotYetReadSurviveAShrink(t *testing.T) {
	w := newHookSpikeWorld(t)
	survivors := w.spike(t, spikePeak)
	kept, gone, touched := survivors[0], survivors[1], survivors[2]
	added := w.entities.alloc()

	w.written(t, func(_ *Query[colliderQuery], set *Set[collider], we *WriteableEntities) {
		set.UpdateFor(added, collider{Radius: 5})
		set.UpdateFor(kept, collider{Radius: 6})
		we.Despawn(gone)
	})
	w.written(t, func(_ *Query[colliderQuery], set *Set[collider], _ *WriteableEntities) {
		row, _ := set.Ref(touched)
		row.Radius = 9
	})
	w.read(t)
	// The survivors carry their spike index, 7, 1007 and 2007, plus the two
	// writes every row took.
	want := []heard{
		{added, "added+changed", 5},
		{gone, "despawned+removed", 1009},
		{kept, "changed", 6},
		{touched, "changed", 9},
	}
	expectHeard(t, "the reader, before the shrink", w.heard, want)
	log := w.components.colliders.hooks
	if cap(log.records) <= len(want) {
		t.Fatalf("the log's records have capacity %d, want the spike's: the shrink would move nothing", cap(log.records))
	}

	released := w.shrink(t, ShrinkRequest{})

	if released.Hooks == 0 {
		t.Fatalf("the zero request released %+v, want Hooks above 0", released)
	}
	if len(log.records) != len(want) || cap(log.records) != len(want) || len(log.retained) != 1 || cap(log.retained) != 1 {
		t.Fatalf("the log holds %d records of capacity %d and %d values of capacity %d after the shrink, want %d and 1, each at capacity",
			len(log.records), cap(log.records), len(log.retained), cap(log.retained), len(want))
	}
	w.readLate(t)
	expectHeard(t, "the late reader, after the shrink", w.lateHeard, want)

	w.written(t, func(_ *Query[colliderQuery], set *Set[collider], _ *WriteableEntities) {
		row, _ := set.Ref(kept)
		row.Radius = 7
	})
	w.read(t)
	w.readLate(t)
	next := []heard{{kept, "changed", 7}}
	expectHeard(t, "the reader, on the runs after the shrink", w.heard, next)
	expectHeard(t, "the late reader, on the runs after the shrink", w.lateHeard, next)
}
