package types

import (
	"iter"
	"sync/atomic"
	"testing"
	"unsafe"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// The #257 prototype: per-Query fillers written as a generator would write
// them, and the dispatch shapes that could reach them. It is measurement
// scaffolding, not a design.
//
// Every filler here is for one Query type and knows it statically: typed
// writes, the field count unrolled, sizes as constants, no reflection, and no
// unsafe beyond fill's own unsafe.Add. A write field is filled barrier-free,
// exactly as fill does, for the invariant fill documents; a read field is a
// typed assignment, so a label's string header is copied with whatever barrier
// it needs.
//
// The arms, per Query shape:
//
//   - Planned, dispatch shape (a), the control: the unrolled walk calls the
//     generated filler through a func value planned once, per Entity. No closure
//     crosses the call. It is expected to lose fill inlining.
//   - Generic, dispatch shape (b)'s candidate: the same walk with the filler a
//     type parameter, so the call is F.fill on a zero-size filler type. -m=2 and
//     the assembly show it is an indirect call through the dictionary.
//   - Value, dispatch shape (c): a generated function yielding (Entity, Q) by
//     value, the fields written into a local.
//   - Pointer, a diagnostic beside (c): the same generated walk writing the
//     Query's own buffer and yielding &q.rows, so the by-value spelling can be
//     told apart from the typed fill.
//
// Every generated walk handles the one Driver the benchmarks exercise, the
// first-declared field, and hands any other run to the shipped fillers.

// protoBind is the prototype's seam onto the bound cursors: it binds q and
// hands back the walk, every cursor in declared order, found by its offset,
// and the declared index of the Driver. The generator knows each field's offset
// statically.
func protoBind[Q any](q *Query[Q], offsets []uintptr, cursors []queryCursor) (walk []Entity, driver int) {
	q.bind()
	for i := range q.fields {
		cursor := q.fields[i].cursor
		for j, offset := range offsets {
			if cursor.offset == offset {
				cursors[j] = cursor
				if i == 0 {
					driver = j
				}
			}
		}
	}
	return q.walk, driver
}

// protoFallbacks counts the runs a generated walk handed to the shipped
// fillers, so a test can pin that a benchmark measured the generated one.
var protoFallbacks atomic.Int64

func fallbackPointer[Q any](q *Query[Q], yield func(Entity, *Q) bool) {
	protoFallbacks.Add(1)
	q.iterate(yield)
}

func fallbackValue[Q any](q *Query[Q], yield func(Entity, Q) bool) {
	protoFallbacks.Add(1)
	q.iterate(func(e Entity, it *Q) bool { return yield(e, *it) })
}

// ---- the fields' offsets, as the generator would emit them

var (
	moveOffsets  = [2]uintptr{unsafe.Offsetof(moveQuery{}.Body), unsafe.Offsetof(moveQuery{}.Velocity)}
	typedOffsets = [2]uintptr{unsafe.Offsetof(typedQuery{}.W0), unsafe.Offsetof(typedQuery{}.Label)}
	wideOffsets  = [5]uintptr{
		unsafe.Offsetof(width5{}.W0), unsafe.Offsetof(width5{}.W1), unsafe.Offsetof(width5{}.W2),
		unsafe.Offsetof(width5{}.W3), unsafe.Offsetof(width5{}.W4),
	}
)

// ---- the generated fillers, by pointer into the Query's buffer, for (a) and (b)

func fillMove(buffer *moveQuery, b0, b1 unsafe.Pointer, r0, r1 uintptr) {
	*(*unsafe.Pointer)(unsafe.Pointer(&buffer.Body)) = unsafe.Add(b0, r0*unsafe.Sizeof(body{}))
	buffer.Velocity = *(*velocity)(unsafe.Add(b1, r1*unsafe.Sizeof(velocity{})))
}

func fillTyped(buffer *typedQuery, b0, b1 unsafe.Pointer, r0, r1 uintptr) {
	*(*unsafe.Pointer)(unsafe.Pointer(&buffer.W0)) = unsafe.Add(b0, r0*unsafe.Sizeof(w0{}))
	buffer.Label = *(*label)(unsafe.Add(b1, r1*unsafe.Sizeof(label{})))
}

func fillWide(buffer *width5, b [5]unsafe.Pointer, r [5]uintptr) {
	*(*unsafe.Pointer)(unsafe.Pointer(&buffer.W0)) = unsafe.Add(b[0], r[0]*unsafe.Sizeof(w0{}))
	buffer.W1 = *(*w1)(unsafe.Add(b[1], r[1]*unsafe.Sizeof(w1{})))
	buffer.W2 = *(*w2)(unsafe.Add(b[2], r[2]*unsafe.Sizeof(w2{})))
	buffer.W3 = *(*w3)(unsafe.Add(b[3], r[3]*unsafe.Sizeof(w3{})))
	buffer.W4 = *(*w4)(unsafe.Add(b[4], r[4]*unsafe.Sizeof(w4{})))
}

// ---- (a) Planned: the filler as a func value, planned once

// The planned fillers are package variables so that nothing at the range site
// can see through them: the call stays indirect, as a func field on the Query
// would be.
var (
	plannedMove  = fillMove
	plannedTyped = fillTyped
	plannedWide  = fillWide
)

func allPlanned2[Q any](q *Query[Q], offsets *[2]uintptr,
	fill func(*Q, unsafe.Pointer, unsafe.Pointer, uintptr, uintptr),
) iter.Seq2[Entity, *Q] {
	return func(yield func(Entity, *Q) bool) {
		var c [2]queryCursor
		walk, driver := protoBind(q, offsets[:], c[:])
		if validate || driver != 0 {
			fallbackPointer(q, yield)
			return
		}
		c0, c1 := c[0], c[1]
		for row := len(walk) - 1; row >= 0; row-- {
			e := walk[row]
			r1, ok := c1.row(e)
			if !ok {
				continue
			}
			fill(&q.rows, c0.rows, c1.rows, uintptr(row), r1)
			if !yield(e, &q.rows) {
				return
			}
		}
	}
}

func allPlanned5[Q any](q *Query[Q], offsets *[5]uintptr,
	fill func(*Q, [5]unsafe.Pointer, [5]uintptr),
) iter.Seq2[Entity, *Q] {
	return func(yield func(Entity, *Q) bool) {
		var c [5]queryCursor
		walk, driver := protoBind(q, offsets[:], c[:])
		if validate || driver != 0 {
			fallbackPointer(q, yield)
			return
		}
		c1, c2, c3, c4 := c[1], c[2], c[3], c[4]
		bases := [5]unsafe.Pointer{c[0].rows, c1.rows, c2.rows, c3.rows, c4.rows}
		for row := len(walk) - 1; row >= 0; row-- {
			e := walk[row]
			r1, ok := c1.row(e)
			if !ok {
				continue
			}
			r2, ok := c2.row(e)
			if !ok {
				continue
			}
			r3, ok := c3.row(e)
			if !ok {
				continue
			}
			r4, ok := c4.row(e)
			if !ok {
				continue
			}
			fill(&q.rows, bases, [5]uintptr{uintptr(row), r1, r2, r3, r4})
			if !yield(e, &q.rows) {
				return
			}
		}
	}
}

// ---- (b) Generic: the filler as a type parameter

type filler2[Q any] interface {
	fill(buffer *Q, b0, b1 unsafe.Pointer, r0, r1 uintptr)
}

type filler5[Q any] interface {
	fill(buffer *Q, b [5]unsafe.Pointer, r [5]uintptr)
}

type (
	moveFiller  struct{}
	typedFiller struct{}
	wideFiller  struct{}
)

func (moveFiller) fill(buffer *moveQuery, b0, b1 unsafe.Pointer, r0, r1 uintptr) {
	fillMove(buffer, b0, b1, r0, r1)
}

func (typedFiller) fill(buffer *typedQuery, b0, b1 unsafe.Pointer, r0, r1 uintptr) {
	fillTyped(buffer, b0, b1, r0, r1)
}

func (wideFiller) fill(buffer *width5, b [5]unsafe.Pointer, r [5]uintptr) { fillWide(buffer, b, r) }

func allGeneric2[Q any, F filler2[Q]](q *Query[Q], offsets *[2]uintptr) iter.Seq2[Entity, *Q] {
	return func(yield func(Entity, *Q) bool) {
		var c [2]queryCursor
		walk, driver := protoBind(q, offsets[:], c[:])
		if validate || driver != 0 {
			fallbackPointer(q, yield)
			return
		}
		var f F
		c0, c1 := c[0], c[1]
		for row := len(walk) - 1; row >= 0; row-- {
			e := walk[row]
			r1, ok := c1.row(e)
			if !ok {
				continue
			}
			f.fill(&q.rows, c0.rows, c1.rows, uintptr(row), r1)
			if !yield(e, &q.rows) {
				return
			}
		}
	}
}

func allGeneric5[Q any, F filler5[Q]](q *Query[Q], offsets *[5]uintptr) iter.Seq2[Entity, *Q] {
	return func(yield func(Entity, *Q) bool) {
		var c [5]queryCursor
		walk, driver := protoBind(q, offsets[:], c[:])
		if validate || driver != 0 {
			fallbackPointer(q, yield)
			return
		}
		var f F
		c1, c2, c3, c4 := c[1], c[2], c[3], c[4]
		bases := [5]unsafe.Pointer{c[0].rows, c1.rows, c2.rows, c3.rows, c4.rows}
		for row := len(walk) - 1; row >= 0; row-- {
			e := walk[row]
			r1, ok := c1.row(e)
			if !ok {
				continue
			}
			r2, ok := c2.row(e)
			if !ok {
				continue
			}
			r3, ok := c3.row(e)
			if !ok {
				continue
			}
			r4, ok := c4.row(e)
			if !ok {
				continue
			}
			f.fill(&q.rows, bases, [5]uintptr{uintptr(row), r1, r2, r3, r4})
			if !yield(e, &q.rows) {
				return
			}
		}
	}
}

// ---- (c) Value: a generated function per Query, yielding (Entity, Q) by value

func allMoveValue(q *Query[moveQuery]) iter.Seq2[Entity, moveQuery] {
	return func(yield func(Entity, moveQuery) bool) {
		var c [2]queryCursor
		walk, driver := protoBind(q, moveOffsets[:], c[:])
		if validate || driver != 0 {
			fallbackValue(q, yield)
			return
		}
		c0, c1 := c[0], c[1]
		for row := len(walk) - 1; row >= 0; row-- {
			e := walk[row]
			r1, ok := c1.row(e)
			if !ok {
				continue
			}
			if !yield(e, moveQuery{
				Body:     (*body)(unsafe.Add(c0.rows, uintptr(row)*unsafe.Sizeof(body{}))),
				Velocity: *(*velocity)(unsafe.Add(c1.rows, r1*unsafe.Sizeof(velocity{}))),
			}) {
				return
			}
		}
	}
}

func allTypedValue(q *Query[typedQuery]) iter.Seq2[Entity, typedQuery] {
	return func(yield func(Entity, typedQuery) bool) {
		var c [2]queryCursor
		walk, driver := protoBind(q, typedOffsets[:], c[:])
		if validate || driver != 0 {
			fallbackValue(q, yield)
			return
		}
		c0, c1 := c[0], c[1]
		for row := len(walk) - 1; row >= 0; row-- {
			e := walk[row]
			r1, ok := c1.row(e)
			if !ok {
				continue
			}
			if !yield(e, typedQuery{
				W0:    (*w0)(unsafe.Add(c0.rows, uintptr(row)*unsafe.Sizeof(w0{}))),
				Label: *(*label)(unsafe.Add(c1.rows, r1*unsafe.Sizeof(label{}))),
			}) {
				return
			}
		}
	}
}

func allWideValue(q *Query[width5]) iter.Seq2[Entity, width5] {
	return func(yield func(Entity, width5) bool) {
		var c [5]queryCursor
		walk, driver := protoBind(q, wideOffsets[:], c[:])
		if validate || driver != 0 {
			fallbackValue(q, yield)
			return
		}
		c0, c1, c2, c3, c4 := c[0], c[1], c[2], c[3], c[4]
		for row := len(walk) - 1; row >= 0; row-- {
			e := walk[row]
			r1, ok := c1.row(e)
			if !ok {
				continue
			}
			r2, ok := c2.row(e)
			if !ok {
				continue
			}
			r3, ok := c3.row(e)
			if !ok {
				continue
			}
			r4, ok := c4.row(e)
			if !ok {
				continue
			}
			if !yield(e, width5{
				W0: (*w0)(unsafe.Add(c0.rows, uintptr(row)*unsafe.Sizeof(w0{}))),
				W1: *(*w1)(unsafe.Add(c1.rows, r1*unsafe.Sizeof(w1{}))),
				W2: *(*w2)(unsafe.Add(c2.rows, r2*unsafe.Sizeof(w2{}))),
				W3: *(*w3)(unsafe.Add(c3.rows, r3*unsafe.Sizeof(w3{}))),
				W4: *(*w4)(unsafe.Add(c4.rows, r4*unsafe.Sizeof(w4{}))),
			}) {
				return
			}
		}
	}
}

// ---- Pointer: the generated walk, writing the Query's buffer

func allMovePointer(q *Query[moveQuery]) iter.Seq2[Entity, *moveQuery] {
	return func(yield func(Entity, *moveQuery) bool) {
		var c [2]queryCursor
		walk, driver := protoBind(q, moveOffsets[:], c[:])
		if validate || driver != 0 {
			fallbackPointer(q, yield)
			return
		}
		c0, c1 := c[0], c[1]
		for row := len(walk) - 1; row >= 0; row-- {
			e := walk[row]
			r1, ok := c1.row(e)
			if !ok {
				continue
			}
			*(*unsafe.Pointer)(unsafe.Pointer(&q.rows.Body)) = unsafe.Add(c0.rows, uintptr(row)*unsafe.Sizeof(body{}))
			q.rows.Velocity = *(*velocity)(unsafe.Add(c1.rows, r1*unsafe.Sizeof(velocity{})))
			if !yield(e, &q.rows) {
				return
			}
		}
	}
}

func allTypedPointer(q *Query[typedQuery]) iter.Seq2[Entity, *typedQuery] {
	return func(yield func(Entity, *typedQuery) bool) {
		var c [2]queryCursor
		walk, driver := protoBind(q, typedOffsets[:], c[:])
		if validate || driver != 0 {
			fallbackPointer(q, yield)
			return
		}
		c0, c1 := c[0], c[1]
		for row := len(walk) - 1; row >= 0; row-- {
			e := walk[row]
			r1, ok := c1.row(e)
			if !ok {
				continue
			}
			*(*unsafe.Pointer)(unsafe.Pointer(&q.rows.W0)) = unsafe.Add(c0.rows, uintptr(row)*unsafe.Sizeof(w0{}))
			q.rows.Label = *(*label)(unsafe.Add(c1.rows, r1*unsafe.Sizeof(label{})))
			if !yield(e, &q.rows) {
				return
			}
		}
	}
}

func allWidePointer(q *Query[width5]) iter.Seq2[Entity, *width5] {
	return func(yield func(Entity, *width5) bool) {
		var c [5]queryCursor
		walk, driver := protoBind(q, wideOffsets[:], c[:])
		if validate || driver != 0 {
			fallbackPointer(q, yield)
			return
		}
		c0, c1, c2, c3, c4 := c[0], c[1], c[2], c[3], c[4]
		for row := len(walk) - 1; row >= 0; row-- {
			e := walk[row]
			r1, ok := c1.row(e)
			if !ok {
				continue
			}
			r2, ok := c2.row(e)
			if !ok {
				continue
			}
			r3, ok := c3.row(e)
			if !ok {
				continue
			}
			r4, ok := c4.row(e)
			if !ok {
				continue
			}
			*(*unsafe.Pointer)(unsafe.Pointer(&q.rows.W0)) = unsafe.Add(c0.rows, uintptr(row)*unsafe.Sizeof(w0{}))
			q.rows.W1 = *(*w1)(unsafe.Add(c1.rows, r1*unsafe.Sizeof(w1{})))
			q.rows.W2 = *(*w2)(unsafe.Add(c2.rows, r2*unsafe.Sizeof(w2{})))
			q.rows.W3 = *(*w3)(unsafe.Add(c3.rows, r3*unsafe.Sizeof(w3{})))
			q.rows.W4 = *(*w4)(unsafe.Add(c4.rows, r4*unsafe.Sizeof(w4{})))
			if !yield(e, &q.rows) {
				return
			}
		}
	}
}

// ---- the Systems, one per shape and arm, each with its shape's loop body

func movePlanned(q *Query[moveQuery]) {
	for _, it := range allPlanned2(q, &moveOffsets, plannedMove) {
		it.Body.X += it.Velocity.X
		it.Body.Y += it.Velocity.Y
	}
}

func moveGeneric(q *Query[moveQuery]) {
	for _, it := range allGeneric2[moveQuery, moveFiller](q, &moveOffsets) {
		it.Body.X += it.Velocity.X
		it.Body.Y += it.Velocity.Y
	}
}

func moveValue(q *Query[moveQuery]) {
	for _, it := range allMoveValue(q) {
		it.Body.X += it.Velocity.X
		it.Body.Y += it.Velocity.Y
	}
}

func movePointer(q *Query[moveQuery]) {
	for _, it := range allMovePointer(q) {
		it.Body.X += it.Velocity.X
		it.Body.Y += it.Velocity.Y
	}
}

func typedPlanned(q *Query[typedQuery]) {
	for _, it := range allPlanned2(q, &typedOffsets, plannedTyped) {
		it.W0.X += it.Label.X + float32(len(it.Label.Name))
	}
}

func typedGeneric(q *Query[typedQuery]) {
	for _, it := range allGeneric2[typedQuery, typedFiller](q, &typedOffsets) {
		it.W0.X += it.Label.X + float32(len(it.Label.Name))
	}
}

func typedValue(q *Query[typedQuery]) {
	for _, it := range allTypedValue(q) {
		it.W0.X += it.Label.X + float32(len(it.Label.Name))
	}
}

func typedPointer(q *Query[typedQuery]) {
	for _, it := range allTypedPointer(q) {
		it.W0.X += it.Label.X + float32(len(it.Label.Name))
	}
}

func widePlanned(q *Query[width5]) {
	for _, it := range allPlanned5(q, &wideOffsets, plannedWide) {
		it.W0.X += it.W1.X + it.W2.X + it.W3.X + it.W4.X
	}
}

func wideGeneric(q *Query[width5]) {
	for _, it := range allGeneric5[width5, wideFiller](q, &wideOffsets) {
		it.W0.X += it.W1.X + it.W2.X + it.W3.X + it.W4.X
	}
}

func wideValue(q *Query[width5]) {
	for _, it := range allWideValue(q) {
		it.W0.X += it.W1.X + it.W2.X + it.W3.X + it.W4.X
	}
}

func widePointer(q *Query[width5]) {
	for _, it := range allWidePointer(q) {
		it.W0.X += it.W1.X + it.W2.X + it.W3.X + it.W4.X
	}
}

// ---- benchmarks: the two shape over populate, wide and typed over fillWorld

func BenchmarkFrameQueryPlanned1k(b *testing.B) {
	benchmarkFrame(b, 1_000, subscribeSystem(movePlanned))
}
func BenchmarkFrameQueryPlanned10k(b *testing.B) {
	benchmarkFrame(b, 10_000, subscribeSystem(movePlanned))
}
func BenchmarkFrameQueryGeneric1k(b *testing.B) {
	benchmarkFrame(b, 1_000, subscribeSystem(moveGeneric))
}
func BenchmarkFrameQueryGeneric10k(b *testing.B) {
	benchmarkFrame(b, 10_000, subscribeSystem(moveGeneric))
}
func BenchmarkFrameQueryValue1k(b *testing.B)  { benchmarkFrame(b, 1_000, subscribeSystem(moveValue)) }
func BenchmarkFrameQueryValue10k(b *testing.B) { benchmarkFrame(b, 10_000, subscribeSystem(moveValue)) }
func BenchmarkFrameQueryPointer1k(b *testing.B) {
	benchmarkFrame(b, 1_000, subscribeSystem(movePointer))
}
func BenchmarkFrameQueryPointer10k(b *testing.B) {
	benchmarkFrame(b, 10_000, subscribeSystem(movePointer))
}

func BenchmarkFrameWidePlanned1k(b *testing.B) {
	benchmarkFillFrame(b, 1_000, subscribeSystem(widePlanned))
}
func BenchmarkFrameWidePlanned10k(b *testing.B) {
	benchmarkFillFrame(b, 10_000, subscribeSystem(widePlanned))
}
func BenchmarkFrameWideGeneric1k(b *testing.B) {
	benchmarkFillFrame(b, 1_000, subscribeSystem(wideGeneric))
}
func BenchmarkFrameWideGeneric10k(b *testing.B) {
	benchmarkFillFrame(b, 10_000, subscribeSystem(wideGeneric))
}
func BenchmarkFrameWideValue1k(b *testing.B) {
	benchmarkFillFrame(b, 1_000, subscribeSystem(wideValue))
}
func BenchmarkFrameWideValue10k(b *testing.B) {
	benchmarkFillFrame(b, 10_000, subscribeSystem(wideValue))
}
func BenchmarkFrameWidePointer1k(b *testing.B) {
	benchmarkFillFrame(b, 1_000, subscribeSystem(widePointer))
}
func BenchmarkFrameWidePointer10k(b *testing.B) {
	benchmarkFillFrame(b, 10_000, subscribeSystem(widePointer))
}

func BenchmarkFrameTypedPlanned1k(b *testing.B) {
	benchmarkFillFrame(b, 1_000, subscribeSystem(typedPlanned))
}
func BenchmarkFrameTypedPlanned10k(b *testing.B) {
	benchmarkFillFrame(b, 10_000, subscribeSystem(typedPlanned))
}
func BenchmarkFrameTypedGeneric1k(b *testing.B) {
	benchmarkFillFrame(b, 1_000, subscribeSystem(typedGeneric))
}
func BenchmarkFrameTypedGeneric10k(b *testing.B) {
	benchmarkFillFrame(b, 10_000, subscribeSystem(typedGeneric))
}
func BenchmarkFrameTypedValue1k(b *testing.B) {
	benchmarkFillFrame(b, 1_000, subscribeSystem(typedValue))
}
func BenchmarkFrameTypedValue10k(b *testing.B) {
	benchmarkFillFrame(b, 10_000, subscribeSystem(typedValue))
}
func BenchmarkFrameTypedPointer1k(b *testing.B) {
	benchmarkFillFrame(b, 1_000, subscribeSystem(typedPointer))
}
func BenchmarkFrameTypedPointer10k(b *testing.B) {
	benchmarkFillFrame(b, 10_000, subscribeSystem(typedPointer))
}

// ---- tests

// twoFrameAllocations is fillFrameAllocations over newWorld and populate.
func twoFrameAllocations(t *testing.T, n int, subscribe func(*kernel.Registrar)) float64 {
	t.Helper()
	const frames = 10_000
	entities, components, engine := newWorld(t, uint32(n), subscribe)
	populate(entities, components, n)
	executioner := engine.Executioner()
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

// TestEveryPrototypeArmSitsOnTheCurrentQuerysAllocationLine is go condition 2:
// a steady state for every arm, at 1 000 and 10 000 Entities, against the
// current Query of the same shape.
func TestEveryPrototypeArmSitsOnTheCurrentQuerysAllocationLine(t *testing.T) {
	if raceEnabled {
		t.Skip("allocation counts are not meaningful under -race")
	}
	type arm struct {
		name      string
		subscribe func(*kernel.Registrar)
	}
	for _, shape := range []struct {
		name    string
		measure func(*testing.T, int, func(*kernel.Registrar)) float64
		current func(*kernel.Registrar)
		arms    []arm
	}{
		{"two", twoFrameAllocations, subscribeMove, []arm{
			{"planned", subscribeSystem(movePlanned)}, {"generic", subscribeSystem(moveGeneric)},
			{"value", subscribeSystem(moveValue)}, {"pointer", subscribeSystem(movePointer)},
		}},
		{"wide", fillFrameAllocations, subscribeSystem(wideQueryAll), []arm{
			{"planned", subscribeSystem(widePlanned)}, {"generic", subscribeSystem(wideGeneric)},
			{"value", subscribeSystem(wideValue)}, {"pointer", subscribeSystem(widePointer)},
		}},
		{"typed", fillFrameAllocations, subscribeSystem(typedQueryAll), []arm{
			{"planned", subscribeSystem(typedPlanned)}, {"generic", subscribeSystem(typedGeneric)},
			{"value", subscribeSystem(typedValue)}, {"pointer", subscribeSystem(typedPointer)},
		}},
	} {
		current1k := shape.measure(t, 1_000, shape.current)
		current10k := shape.measure(t, 10_000, shape.current)
		t.Logf("%s current: %.3f at 1k, %.3f at 10k", shape.name, current1k, current10k)
		for _, a := range shape.arms {
			at1k := shape.measure(t, 1_000, a.subscribe)
			at10k := shape.measure(t, 10_000, a.subscribe)
			t.Logf("%s %s: %.3f at 1k, %.3f at 10k", shape.name, a.name, at1k, at10k)
			if at1k > current1k+0.05 || at10k > current10k+0.05 {
				t.Errorf("%s %s costs %.3f / %.3f objects a frame against the current Query's %.3f / %.3f",
					shape.name, a.name, at1k, at10k, current1k, current10k)
			}
		}
	}
}

// TestEveryPrototypeArmComputesWhatAllDoes checks each arm against the current
// Query's result: one frame on the benchmarks' population, where the generated
// walk must run and never fall back, and one on a population whose second field
// is shorter, so it drives and the run falls back to the shipped fillers.
func TestEveryPrototypeArmComputesWhatAllDoes(t *testing.T) {
	const n = 300
	for _, arm := range []struct {
		name  string
		two   func(*kernel.Registrar)
		wide  func(*kernel.Registrar)
		typed func(*kernel.Registrar)
	}{
		{"planned", subscribeSystem(movePlanned), subscribeSystem(widePlanned), subscribeSystem(typedPlanned)},
		{"generic", subscribeSystem(moveGeneric), subscribeSystem(wideGeneric), subscribeSystem(typedGeneric)},
		{"value", subscribeSystem(moveValue), subscribeSystem(wideValue), subscribeSystem(typedValue)},
		{"pointer", subscribeSystem(movePointer), subscribeSystem(widePointer), subscribeSystem(typedPointer)},
	} {
		t.Run(arm.name, func(t *testing.T) {
			before := protoFallbacks.Load()

			// two, on populate: every body moves by its velocity {1, 2}.
			entities, components, engine := newWorld(t, n, arm.two)
			populate(entities, components, n)
			frame(t, engine, 1)
			if !bodiesAre(t, components, n, 1, 2) {
				t.Fatalf("two: a body did not move by its velocity")
			}

			// wide and typed, on fillWorld: W0.X gains 1+2+3+4, or 1+len("label").
			for _, shape := range []struct {
				name      string
				subscribe func(*kernel.Registrar)
				want      float32
			}{{"wide", arm.wide, 10}, {"typed", arm.typed, 6}} {
				width, _, engine := fillWorld(t, n, shape.subscribe)
				frame(t, engine, 1)
				for row := range width.s0.dense {
					if got := width.s0.dense[row].X; got != shape.want {
						t.Fatalf("%s: row %d: W0.X = %v, want %v", shape.name, row, got, shape.want)
					}
				}
			}
			if fell := protoFallbacks.Load() - before; !validate && fell != 0 {
				t.Fatalf("%d runs fell back on the benchmarks' population, so the benchmarks would not measure the generated walk", fell)
			}

			// The fallback: a velocity on a third of the bodies, so it drives.
			entities, components, engine = newWorld(t, n, arm.two)
			var moved []Entity
			for i := range n {
				e := entities.alloc()
				components.bodies.Set(e, body{})
				if i%3 == 0 {
					components.velocities.Set(e, velocity{X: 1, Y: 2})
					moved = append(moved, e)
				}
			}
			before = protoFallbacks.Load()
			frame(t, engine, 1)
			if protoFallbacks.Load() == before {
				t.Fatalf("a run driven by the second field did not fall back")
			}
			for _, e := range moved {
				if value, _ := components.bodies.Get(e); value != (body{X: 1, Y: 2}) {
					t.Fatalf("fallback: body of %v = %v, want {1 2}", e, value)
				}
			}
		})
	}
}
