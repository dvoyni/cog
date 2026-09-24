package internal

import (
	"flag"
	"fmt"
	"sync"
	"testing"
	"unsafe"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// This file and splitbench_test.go are a spike, and nothing here is production
// code: https://github.com/dvoyni/cog/issues/278 asks whether one All() loop can
// be split across worker goroutines that already exist and are joined before the
// System returns, under the lock set the System was already granted, and whether
// that pays on a real engine frame.
//
// The split dodges the kernel's ~2.2 us scheduling floor rather than paying it,
// because it adds no scheduled tasks: the workers are not the kernel's, they run
// inside one System call, and they are joined before it returns.
//
// Why a Query-only System may be split at all. Its signature holds one Query and
// nothing else, so it holds read{*Entities}: no System can make a structural
// change while it runs, nothing can move a row, and every Entity in the driver's
// owners array is visited by exactly one worker. The backwards walk that makes
// "you may restructure the Entity you are visiting" true is what a split breaks,
// and a Query-only System cannot restructure anything.
//
// Two rules this deliberately breaks, inside a test and nowhere else:
//
//   - kernel.instructions forbids reading a handle from a goroutine the handler
//     started. The workers read no handle. They touch Store memory the System's
//     own locks already cover, through cursors bind resolved on the calling
//     goroutine, and they finish before the handler returns. Writing the carve-out
//     that separates escaping the handler from running joined inside it is the
//     build spec's job, not this spike's.
//   - A System is not re-entrant: one call owns its Query's fill buffer, walk and
//     cursors (#282). Workers joined inside one call are not re-entrancy, but they
//     share that one call's per-Query state, so each worker fills its own buffer,
//     allocated once at pool creation and never per tick.

// splitK is the serial fallback threshold in driver rows per worker, below which
// walkQuery runs the whole range on the calling goroutine. It is a flag because
// the sweep that picks it has to be run with the fallback off: -split.k=0 is the
// raw grid, and the default is what that grid chose.
var splitK = flag.Int("split.k", splitFallbackRows,
	"driver rows per worker below which the split walk runs serially; 0 disables the fallback")

// splitFallbackRows is K as the sweep picked it, and the sweep's most useful
// finding is that it should not exist in this shape.
//
// A row count is the wrong quantity. At one thousand Entities and eight workers
// - 125 rows a worker either way - the split loses at 0.78x on the trivial work
// level and wins at 1.31x on the ~25 ns one. Same rows, opposite verdicts: what
// decides is the work a worker gets, and a row count cannot see it.
//
// 128 is the only value that gates every measured loss without gating a measured
// win at the trivial level, against the serial arm the ticket defines - the real
// Query.All(). It holds the walk serial below 1 024 rows at W=8 and below 4 096
// at W=32, which covers the whole loss region, and it lets every winning point
// through at 5 000 Entities and above. What it costs is stated rather than
// hidden: at a thousand Entities it also refuses the 2.09x and 3.64x the heavier
// work levels measured there.
//
// Against the honest baseline - the same walk with no iterator, which is what
// the fallback path itself runs - no value works at all. Protecting W=8 at five
// thousand Entities needs K above 625 and letting W=32 through at twenty
// thousand needs it at or below 625, and those do not intersect. See the verdict
// on https://github.com/dvoyni/cog/issues/278 for the gate a build should take
// instead, and for what the iterator turns out to cost on its own.
const splitFallbackRows = 128

// splitAllocationResidue is how far above the serial arm a split arm's objects a
// frame may sit. It is not slack for the walk, which allocates nothing: it is the
// Go runtime's, and it is measured at about 0.04 objects a frame with 8 workers
// and 0.09 with 32, flat from a thousand Entities to twenty thousand.
const splitAllocationResidue = 0.15

// splitSystem is the spike's subscription identity. Every arm registers one, so
// the arms differ by the System's body and by nothing else the kernel can see.
type splitSystem kernel.Subscription[app.UpdateEvent]

// splitCacheLine is the padding unit for the per-worker fill buffers. Two
// buffers 64 bytes apart are never in one line whatever the backing array's
// alignment is, which is the whole of what the padding has to buy: a 16-byte
// buffer written every Entity by one worker must not share a line with the next
// worker's.
const splitCacheLine = 64

// splitBuffer is one worker's fill buffer. rows is what q.rows is to a serial
// run, and it is per worker because the fillers write into it every Entity.
type splitBuffer struct {
	rows typesMoveQuery
	_    [splitCacheLine - unsafe.Sizeof(typesMoveQuery{})]byte
}

// splitRange is one worker's share of the walk, sent by value so a dispatch
// allocates nothing: a contiguous half-open row range and the index of the
// buffer to fill.
type splitRange struct {
	lo, hi, worker int
}

// splitRun is what one call of the System hands its workers: the cursors bind
// resolved, the driver's owners array as that run captured it, and the work
// level. It is written on the calling goroutine before any job is sent, so the
// channel send is the happens-before edge every worker reads it behind, and
// Wait is the edge back.
type splitRun struct {
	driver, second queryCursor
	walk           []Entity
	iterations     int
}

// splitPool is the persistent worker pool: created in benchmark or test setup,
// before the timed loop, and torn down by a Cleanup. Its goroutines receive
// plain range values over channels and signal completion through a WaitGroup, so
// a tick builds no closure and allocates nothing.
//
// Worker 0 is the calling goroutine. A System that dispatched all W ranges and
// then blocked would pay W wakeups to do nothing itself; taking range 0 after
// the others are on their way costs one wakeup fewer and keeps the caller warm.
type splitPool struct {
	// jobs is one buffered channel per worker, capacity 1, so a dispatch never
	// blocks the caller behind a worker that has not parked yet. jobs[0] is nil:
	// worker 0 is the caller and is never sent to.
	jobs    []chan splitRange
	buffers []splitBuffer
	done    sync.WaitGroup
	run     splitRun
	// k is the fallback threshold in rows per worker, 0 to disable it.
	k int
	// call is the per-Entity function of the handler-driven arm, and nil in the
	// arm whose body is inlined into the walk. It is read once per range, never
	// per Entity, and it is what the third shape on the ticket costs: the walk
	// moves inside the framework and the body becomes an indirect call the
	// compiler cannot inline. Query's own design is built around avoiding
	// exactly that - see queryField.copy - so it is worth a number rather than
	// an opinion.
	call splitBody
}

// splitBody is the per-Entity function a handler-driven split calls. The Entity
// comes first for the reason All() yields it first: the driver's owners array is
// loaded anyway.
type splitBody func(e Entity, it *typesMoveQuery)

func newSplitPool(tb testing.TB, workers, k int) *splitPool {
	tb.Helper()
	pool := &splitPool{
		jobs:    make([]chan splitRange, workers),
		buffers: make([]splitBuffer, workers),
		k:       k,
	}
	for worker := 1; worker < workers; worker++ {
		jobs := make(chan splitRange, 1)
		pool.jobs[worker] = jobs
		go pool.serve(jobs)
	}
	tb.Cleanup(func() {
		// A negative hi is the sentinel rather than a closed channel or a select
		// on a quit channel: a select costs every dispatch a second case on the
		// hot path, for a shutdown that happens once.
		for worker := 1; worker < workers; worker++ {
			pool.jobs[worker] <- splitRange{hi: -1}
		}
	})
	return pool
}

// serve is one worker's whole life: park on the channel, walk the range, signal.
func (p *splitPool) serve(jobs chan splitRange) {
	for job := range jobs {
		if job.hi < 0 {
			return
		}
		p.walkRange(job.worker, job.lo, job.hi)
		p.done.Done()
	}
}

// walkQuery is the split walk: bind on the calling goroutine, cut the driver's
// rows into as many contiguous ranges as there are workers, dispatch all but the
// first, take the first, and join before returning.
//
// bind is reached directly, as the unrolled fillers do, because the whole point
// is to replace iterate2's loop and keep everything in front of it unchanged.
func (p *splitPool) walkQuery(q *Query[typesMoveQuery], iterations int) {
	q.bind()
	p.run.driver, p.run.second = q.fields[0].cursor, q.fields[1].cursor
	p.run.walk, p.run.iterations = q.walk, iterations

	rows, workers := len(p.run.walk), len(p.jobs)
	// The serial fallback. Below k rows a worker, the walk is cheaper than the
	// wakeups that would share it out.
	if rows < p.k*workers {
		p.walkRange(0, 0, rows)
		return
	}
	// The remainder is spread over the first ranges rather than piled onto the
	// last, so the longest range is never more than one row above the shortest.
	base, extra := rows/workers, rows%workers
	first := splitRange{lo: 0, hi: base, worker: 0}
	if extra > 0 {
		first.hi++
	}
	p.done.Add(workers - 1)
	lo := first.hi
	for worker := 1; worker < workers; worker++ {
		hi := lo + base
		if worker < extra {
			hi++
		}
		p.jobs[worker] <- splitRange{lo: lo, hi: hi, worker: worker}
		lo = hi
	}
	p.walkRange(first.worker, first.lo, first.hi)
	p.done.Wait()
}

// walkRange is iterate2's loop over one row range, filling this worker's own
// buffer. The cursors are copied into locals first, for the reason the fillers
// state: reached through the run they are memory a write through an
// unsafe.Pointer forces the compiler to reload.
//
// The pointer field is filled barrier-free, which is sound here for the reason
// Query.fill documents and for one more: the buffer is a heap object, but every
// pointer it can ever hold is an interior pointer into a live Store, and a Store
// is a kernel resource cell held for the engine lifetime.
func (p *splitPool) walkRange(worker, lo, hi int) {
	if p.call != nil {
		p.walkRangeCall(worker, lo, hi)
		return
	}
	run := &p.run
	buffer := unsafe.Pointer(&p.buffers[worker].rows)
	driver, second := run.driver, run.second
	walk, iterations := run.walk, run.iterations
	for row := hi - 1; row >= lo; row-- {
		e := walk[row]
		secondRow, ok := second.row(e)
		if !ok {
			continue
		}
		second.fill(secondRow, buffer)
		driver.fill(uintptr(row), buffer)
		splitWork((*typesMoveQuery)(buffer), iterations)
	}
}

// walkRangeCall is walkRange with the body reached through a func value instead
// of inlined: the same probe, the same fills, the same order, and one indirect
// call an Entity. The difference between the two is the whole cost of moving the
// loop inside the framework.
//
// The fill buffer is turned into *moveQuery once, outside the loop, because a
// handler-driven walk hands the same buffer to every Entity exactly as All()
// hands out the same q.rows.
func (p *splitPool) walkRangeCall(worker, lo, hi int) {
	run := &p.run
	buffer := unsafe.Pointer(&p.buffers[worker].rows)
	driver, second := run.driver, run.second
	walk := run.walk
	call := p.call
	it := (*typesMoveQuery)(buffer)
	for row := hi - 1; row >= lo; row-- {
		e := walk[row]
		secondRow, ok := second.row(e)
		if !ok {
			continue
		}
		second.fill(secondRow, buffer)
		driver.fill(uintptr(row), buffer)
		call(e, it)
	}
}

// The per-Entity bodies of the handler-driven arm, one per work level. They are
// top-level funcs taken as values, so the call through p.call is a real indirect
// one and not something the compiler can devirtualise back into the loop.
func splitCallWork2(_ Entity, it *typesMoveQuery)   { splitWork(it, splitWork2) }
func splitCallWork25(_ Entity, it *typesMoveQuery)  { splitWork(it, splitWork25) }
func splitCallWork100(_ Entity, it *typesMoveQuery) { splitWork(it, splitWork100) }
func splitCallWork400(_ Entity, it *typesMoveQuery) { splitWork(it, splitWork400) }

// splitWork is the loop body, and it is the same function in every arm, so the
// arms differ by how the walk is shared out and by nothing else.
//
// One thing they do differ by, and it is measured rather than assumed: the
// serial arm reaches the body through Query.All(), a range-over-func, and every
// split arm - the serial fallback included - reaches it through walkRange, which
// has no iterator. That is worth 1.1 ns an Entity, flat from a thousand Entities
// to twenty thousand, which is 30% of a trivial-work frame at twenty thousand
// and nothing at all once the body costs 100 ns. Forcing the fallback on with
// -split.k=1000000 measures it: that arm is this walk with no yield and no
// workers.
//
// Y is an accumulator, and it is what makes a visit countable: after f frames a
// body's Y is 2f exactly, so a skipped Entity and a twice-visited one are both
// visible in a float32 compare. X carries the synthetic load, so the work cannot
// be optimised away and the write through the pointer field has to land.
func splitWork(it *typesMoveQuery, iterations int) {
	it.Body.Y += it.Velocity.Y
	it.Body.X = spin(iterations, it.Body.X+it.Velocity.X)
}

// spin is the synthetic entity-local work: a dependent multiply-add chain over
// the Entity's own Components, at a fixed cost an iteration and no memory
// traffic. The recurrence converges rather than growing, so no work level can
// reach an infinity or a denormal and make itself slower than the level below.
//
// iterations is a variable and never a constant, so no arm gets its loop
// unrolled or folded away.
func spin(iterations int, seed float32) float32 {
	x := seed
	for range iterations {
		x = x*0.5 + 0.25
	}
	return x
}

// The work levels, named by the per-Entity cost they were calibrated to. The
// nominal name is not what a benchmark reports: every arm reports the per-Entity
// cost it measured.
const (
	splitWork2   = 0
	splitWork25  = 36
	splitWork100 = 112
	splitWork400 = 360
)

// Two more levels, used at one thousand Entities only, and for one purpose: the
// sweep brackets the break-even between 3.8 us of serial walk a run, where the
// split loses, and 17.3 us, where it wins, and a four-fold bracket is not a
// break-even. Holding the row count fixed and moving the work is the cleaner of
// the two ways to close it, because it varies the one quantity the gate would
// have to be stated in.
const (
	splitWork8  = 5
	splitWork15 = 15
)

// splitSerialWalk is the serial arm: the real Query.All() over the real
// iterate2, with the same body every other arm runs.
func splitSerialWalk(q *Query[typesMoveQuery], iterations int) {
	for _, it := range q.All() {
		splitWork(it, iterations)
	}
}

// The four serial Systems are separate top-level funcs rather than one closure
// over the work level, and that is not a style choice. A System that is a
// closure puts the range statement's loop state in a captured environment
// instead of on the stack, it escapes, and the serial arm then pays three
// allocations a frame that no split arm pays. Two arms that allocate
// differently are two different measurements.
func splitSerialWork2(q *Query[typesMoveQuery])   { splitSerialWalk(q, splitWork2) }
func splitSerialWork8(q *Query[typesMoveQuery])   { splitSerialWalk(q, splitWork8) }
func splitSerialWork15(q *Query[typesMoveQuery])  { splitSerialWalk(q, splitWork15) }
func splitSerialWork25(q *Query[typesMoveQuery])  { splitSerialWalk(q, splitWork25) }
func splitSerialWork100(q *Query[typesMoveQuery]) { splitSerialWalk(q, splitWork100) }
func splitSerialWork400(q *Query[typesMoveQuery]) { splitSerialWalk(q, splitWork400) }

func subscribeSplitSerial(system any) func(*kernel.Registrar) {
	return func(registrar *kernel.Registrar) {
		registrar.Subscribe[splitSystem](ToHandler[app.UpdateEvent](registrar, system))
	}
}

func subscribeSplitPool(pool *splitPool, iterations int) func(*kernel.Registrar) {
	return func(registrar *kernel.Registrar) {
		registrar.Subscribe[splitSystem](ToHandler[app.UpdateEvent](registrar,
			func(q *Query[typesMoveQuery]) { pool.walkQuery(q, iterations) }))
	}
}

// TestASplitWalkVisitsEveryEntityExactlyOncePerFrame is the correctness claim,
// and it is made per frame rather than over the run: an accumulator checked only
// at the end cannot tell a frame that visited an Entity twice from a frame that
// skipped it and a later one that made up for it.
//
// The reference is the same recurrence run serially in the test, so the claim is
// not only that every write landed but that it landed with the value a serial
// walk would have produced.
func TestASplitWalkVisitsEveryEntityExactlyOncePerFrame(t *testing.T) {
	const frames = 500
	// driven is the third shape on the ticket: the framework owns the walk and the
	// body is a func value. It is an arm here and not only in the benchmarks,
	// because the buffer it hands every Entity is one buffer, exactly as All()
	// hands out one q.rows, and a handler-driven walk that got that wrong would
	// be wrong in the same invisible way.
	for _, n := range []int{1_000, 5_000} {
		for _, workers := range []int{8, 32} {
			for _, k := range []int{0, splitFallbackRows} {
				for _, driven := range []splitBody{nil, splitCallWork25} {
					name := fmt.Sprintf("N%dW%d%s%s", n, workers, fallbackName(k), drivenName(driven))
					t.Run(name, func(t *testing.T) {
						pool := newSplitPool(t, workers, k)
						pool.call = driven
						entities, components, engine := newWorld(t, uint32(n), subscribeSplitPool(pool, splitWork25))
						populate(entities, components, n)
						checkFrames(t, components, engine, n, frames)
					})
				}
			}
		}
	}
	t.Run("Serial", func(t *testing.T) {
		const n = 1_000
		entities, components, engine := newWorld(t, uint32(n), subscribeSplitSerial(splitSerialWork25))
		populate(entities, components, n)
		checkFrames(t, components, engine, n, frames)
	})
}

func drivenName(call splitBody) string {
	if call != nil {
		return "Called"
	}
	return "Inlined"
}

func fallbackName(k int) string {
	if k > 0 {
		return "FallbackOn"
	}
	return "FallbackOff"
}

// checkFrames drives frames frames and, after each one, compares every Entity's
// body against what a serial walk of the same recurrence would have written.
func checkFrames(t *testing.T, components *componentsPlugin, engine *kernel.Engine, n, frames int) {
	t.Helper()
	x, y := float32(0), float32(0)
	for f := range frames {
		frame(t, engine, 1)
		x = spin(splitWork25, x+1)
		y += 2
		if !bodiesAre(t, components, n, x, y) {
			t.Fatalf("frame %d: the bodies diverged from the serial reference", f+1)
		}
	}
}

// bodiesAre reports whether every populated Entity's body is exactly the
// reference. It walks the Store directly, which only a test in this package can
// do, because reading it through a Query would run the very walk under test.
func bodiesAre(t *testing.T, components *componentsPlugin, n int, x, y float32) bool {
	t.Helper()
	store := components.bodies
	if len(store.owners) != n {
		t.Fatalf("the body Store holds %d rows, want %d", len(store.owners), n)
	}
	want := body{X: x, Y: y}
	for row := range store.dense {
		if store.dense[row] != want {
			t.Errorf("the body of %v holds %v, want %v", store.owners[row], store.dense[row], want)
			return false
		}
	}
	return true
}

// TestTheSplitFrameSitsOnTheEnginesAllocationLine is
// TestTheFrameSitsOnTheEnginesAllocationLine for the split arms: a steady state
// over a thousand frames rather than an average over b.N, at two entity counts,
// against the serial arm of the same work level.
//
// A per-tick closure, a per-tick range slice or a channel that boxed its value
// would all show up here, so the claim is both that a split frame costs what a
// serial one costs, up to a residue the Go runtime and not the walk is
// responsible for, and that nothing in it grows with the entity count.
func TestTheSplitFrameSitsOnTheEnginesAllocationLine(t *testing.T) {
	if raceEnabled {
		t.Skip("allocation counts are not meaningful under -race")
	}
	const frames = 1_000
	measure := func(n int, subscribe func(*kernel.Registrar)) float64 {
		entities, components, engine := newWorld(t, uint32(n), subscribe)
		populate(entities, components, n)
		executioner := engine.Executioner()
		// Warm every pool the first frames fill, so what is measured is steady
		// state and not the first tick.
		for range 100 {
			frame(t, engine, 1)
		}
		mallocs := allocationsDuring(func() {
			for range frames {
				executioner.PublishEvent(app.UpdateEvent{Dt: 1}).Wait()
			}
		})
		return float64(mallocs) / frames
	}

	counts := []int{1_000, 5_000, 20_000}
	split := map[int]map[int]float64{8: {}, 32: {}}
	for _, n := range counts {
		serial := measure(n, subscribeSplitSerial(splitSerialWork25))
		for _, workers := range []int{8, 32} {
			pool := newSplitPool(t, workers, 0)
			split[workers][n] = measure(n, subscribeSplitPool(pool, splitWork25))
			t.Logf("objects a frame at %d entities: serial %.3f, W=%d %.3f",
				n, serial, workers, split[workers][n])
			// The residue over serial is the runtime's own, not the walk's: a
			// worker parking on its channel and a Wait waking take a sudog each,
			// and the per-P sudog caches are refilled from the central pool in
			// batches, so a small fraction of the wakeups malloc. It tracks the
			// worker count and not the entity count, which is what the scaling
			// check below is the actual claim about.
			if split[workers][n] > serial+splitAllocationResidue {
				t.Fatalf("the split walk at %d entities with W=%d costs %.3f objects a frame against the serial %.3f",
					n, workers, split[workers][n], serial)
			}
		}
	}
	// The claim the arms are here for: nothing in the split walk allocates per
	// Entity. A per-tick closure, a per-tick range slice or a channel that boxed
	// its value would all be flat in the entity count too, and they are excluded
	// by the comparison against serial above; this excludes the rest.
	// The bound is the residue's own scale and not tighter, because the residue is
	// what is being differenced: it is worker-shaped, it moves by about 0.05
	// between runs, and a real per-Entity allocation would be nineteen thousand
	// objects a frame across this range rather than a twentieth of one. There is
	// no threshold between those two that is a judgement call.
	for _, workers := range []int{8, 32} {
		if grown := split[workers][20_000] - split[workers][1_000]; grown > splitAllocationResidue {
			t.Fatalf("the split walk with W=%d allocates per Entity: %.3f objects a frame at 1k, %.3f at 20k",
				workers, split[workers][1_000], split[workers][20_000])
		}
	}
}
