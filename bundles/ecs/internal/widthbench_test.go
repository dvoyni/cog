package internal

import (
	"fmt"
	"testing"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// The width sweep: what each additional field in a Query costs an Entity, on
// the shipped fillers. It is kept in the tree so that every number the README
// and the spec quote about Query width can be re-run (#280).
//
// Every row walks the same population: N Entities, each holding every width
// Component and both present Tags, set in the same order, so every Store's dense
// row index is the Entity's index and every Store is the same length. That is
// the cache-friendliest layout there is, deliberately; other layouts are out of
// scope. Equal lengths also pin the Driver: bind breaks the tie to the
// first-declared field, which TestTheWidthSweepDrivesOffItsFirstField asserts.
//
// The loop body is the same everywhere and touches only W0, so an extra field
// costs its probe and its fill and never any loop-body work.

// w0 … w7 are the sweep's Components: one pointer-free 8-byte shape, so every
// field takes fill's 8-byte case and the row width is constant across widths.
type (
	w0 struct{ X, Y float32 }
	w1 struct{ X, Y float32 }
	w2 struct{ X, Y float32 }
	w3 struct{ X, Y float32 }
	w4 struct{ X, Y float32 }
	w5 struct{ X, Y float32 }
	w6 struct{ X, Y float32 }
	w7 struct{ X, Y float32 }
)

// t0 and t1 are Tags every Entity holds; x0 and x1 are Tags no Entity ever
// receives, so a Without of either rejects nothing.
type (
	t0 struct{}
	t1 struct{}
	x0 struct{}
	x1 struct{}
)

// widthPlugin owns the sweep's Component types, apart from componentsPlugin so
// the shared fixture keeps its six Stores. It keeps each typed Store for the
// hand-written rows and the population, and x0's so a test can make a Without
// reject something.
type widthPlugin struct {
	ids uint32
	s0  *Store[w0]
	s1  *Store[w1]
	s2  *Store[w2]
	s3  *Store[w3]
	s4  *Store[w4]
	s5  *Store[w5]
	s6  *Store[w6]
	s7  *Store[w7]
	t0s *Store[t0]
	t1s *Store[t1]
	x0s *Store[x0]
}

func (p *widthPlugin) Name() kernel.PluginName { return "width" }

func (p *widthPlugin) Dependencies() []kernel.PluginName { return []kernel.PluginName{typesName} }

// Register registers every Store with ids slots, which is at least the
// population: NewStore preallocates the sparse index to ids slots of
// absentSlot, so a Without probe loads a real slot and compares it rather than
// taking row's cheaper out-of-range return.
func (p *widthPlugin) Register(registrar *kernel.Registrar, _ any) error {
	p.s0 = RegisterComponent[w0](registrar, p.ids)
	p.s1 = RegisterComponent[w1](registrar, p.ids)
	p.s2 = RegisterComponent[w2](registrar, p.ids)
	p.s3 = RegisterComponent[w3](registrar, p.ids)
	p.s4 = RegisterComponent[w4](registrar, p.ids)
	p.s5 = RegisterComponent[w5](registrar, p.ids)
	p.s6 = RegisterComponent[w6](registrar, p.ids)
	p.s7 = RegisterComponent[w7](registrar, p.ids)
	p.t0s = RegisterComponent[t0](registrar, p.ids)
	p.t1s = RegisterComponent[t1](registrar, p.ids)
	p.x0s = RegisterComponent[x0](registrar, p.ids)
	RegisterComponent[x1](registrar, p.ids)
	return nil
}

// The sweep's Queries. W0 is the one write in each, and the first-declared
// field, so it is the Driver.
type (
	width1 struct{ W0 *w0 }
	width2 struct {
		W0 *w0
		W1 w1
	}
	width3 struct {
		W0 *w0
		W1 w1
		W2 w2
	}
	width4 struct {
		W0 *w0
		W1 w1
		W2 w2
		W3 w3
	}
	width5 struct {
		W0 *w0
		W1 w1
		W2 w2
		W3 w3
		W4 w4
	}
	width6 struct {
		W0 *w0
		W1 w1
		W2 w2
		W3 w3
		W4 w4
		W5 w5
	}
	width7 struct {
		W0 *w0
		W1 w1
		W2 w2
		W3 w3
		W4 w4
		W5 w5
		W6 w6
	}
	width8 struct {
		W0 *w0
		W1 w1
		W2 w2
		W3 w3
		W4 w4
		W5 w5
		W6 w6
		W7 w7
	}
	// tag3 and tag4 name Tags every Entity holds as ordinary fields.
	tag3 struct {
		W0 *w0
		W1 w1
		T0 t0
	}
	tag4 struct {
		W0 *w0
		W1 w1
		T0 t0
		T1 t1
	}
	// without3 and without4 carry Withouts that reject nothing.
	without3 struct {
		W0 *w0
		W1 w1
		_  Without[x0]
	}
	without4 struct {
		W0 *w0
		W1 w1
		_  Without[x0]
		_  Without[x1]
	}
)

type widthSystem kernel.Subscription[app.UpdateEvent]

// widthWorld composes an engine whose one System captures a Query[Q], fills
// it with n aligned Entities, and runs one frame, so the Query is the one the
// kernel bound rather than one assembled by the test.
func widthWorld[Q any](tb testing.TB, n int) (*widthPlugin, *Query[Q]) {
	tb.Helper()
	var query *Query[Q]
	width := &widthPlugin{ids: uint32(n)}
	entities, _, engine := newWorldWith(tb, uint32(n),
		func(registrar *kernel.Registrar) {
			registrar.Subscribe[widthSystem](ToHandler[app.UpdateEvent](registrar, func(q *Query[Q]) { query = q }))
		},
		[]kernel.PluginName{typesName, "components", "width"}, width)
	for range n {
		e := entities.alloc()
		width.s0.Set(e, w0{})
		width.s1.Set(e, w1{})
		width.s2.Set(e, w2{})
		width.s3.Set(e, w3{})
		width.s4.Set(e, w4{})
		width.s5.Set(e, w5{})
		width.s6.Set(e, w6{})
		width.s7.Set(e, w7{})
		width.t0s.Set(e, t0{})
		width.t1s.Set(e, t1{})
	}
	frame(tb, engine, 1)
	if query == nil {
		tb.Fatal("the System never ran, so no Query was captured")
	}
	return width, query
}

// benchmarkWalk times walk, called once per op, and reports the time an
// Entity. walk is a closure because the Query type differs between rows; the
// range statement or iterate call inside it keeps every call in its own chain
// what the row says it is. The classic b.N form is deliberate, for the reason
// benchmarkFrameWith gives.
func benchmarkWalk(b *testing.B, n int, walk func()) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		walk()
	}
	b.StopTimer()
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*n), "ns/entity")
}

// widthCase is one row of the sweep: a benchmark body at a given population.
type widthCase struct {
	name string
	run  func(b *testing.B, n int)
}

// widthCases lists every row. The components-k rows walk through All(), which
// is what a System author pays; components-1 to components-3 take a walk
// written out inside All()'s literal, and components-4 delegates. The
// delegated-k rows walk the same Queries through q.iterate, where every width
// pays one indirect yield an Entity, and they are the rows #280's rule and
// #546's Y read.
var widthCases = []widthCase{
	{"components-1", func(b *testing.B, n int) {
		_, q := widthWorld[width1](b, n)
		benchmarkWalk(b, n, func() {
			for _, it := range q.All() {
				it.W0.X += 1
			}
		})
	}},
	{"components-2", func(b *testing.B, n int) {
		_, q := widthWorld[width2](b, n)
		benchmarkWalk(b, n, func() {
			for _, it := range q.All() {
				it.W0.X += 1
			}
		})
	}},
	{"components-3", func(b *testing.B, n int) {
		_, q := widthWorld[width3](b, n)
		benchmarkWalk(b, n, func() {
			for _, it := range q.All() {
				it.W0.X += 1
			}
		})
	}},
	{"components-4", func(b *testing.B, n int) {
		_, q := widthWorld[width4](b, n)
		benchmarkWalk(b, n, func() {
			for _, it := range q.All() {
				it.W0.X += 1
			}
		})
	}},
	{"components-5", func(b *testing.B, n int) {
		_, q := widthWorld[width5](b, n)
		benchmarkWalk(b, n, func() {
			for _, it := range q.All() {
				it.W0.X += 1
			}
		})
	}},
	{"components-6", func(b *testing.B, n int) {
		_, q := widthWorld[width6](b, n)
		benchmarkWalk(b, n, func() {
			for _, it := range q.All() {
				it.W0.X += 1
			}
		})
	}},
	{"components-7", func(b *testing.B, n int) {
		_, q := widthWorld[width7](b, n)
		benchmarkWalk(b, n, func() {
			for _, it := range q.All() {
				it.W0.X += 1
			}
		})
	}},
	{"components-8", func(b *testing.B, n int) {
		_, q := widthWorld[width8](b, n)
		benchmarkWalk(b, n, func() {
			for _, it := range q.All() {
				it.W0.X += 1
			}
		})
	}},
	{"delegated-1", func(b *testing.B, n int) {
		_, q := widthWorld[width1](b, n)
		benchmarkWalk(b, n, func() {
			q.iterate(func(_ Entity, it *width1) bool {
				it.W0.X += 1
				return true
			})
		})
	}},
	{"delegated-2", func(b *testing.B, n int) {
		_, q := widthWorld[width2](b, n)
		benchmarkWalk(b, n, func() {
			q.iterate(func(_ Entity, it *width2) bool {
				it.W0.X += 1
				return true
			})
		})
	}},
	{"delegated-3", func(b *testing.B, n int) {
		_, q := widthWorld[width3](b, n)
		benchmarkWalk(b, n, func() {
			q.iterate(func(_ Entity, it *width3) bool {
				it.W0.X += 1
				return true
			})
		})
	}},
	{"delegated-4", func(b *testing.B, n int) {
		_, q := widthWorld[width4](b, n)
		benchmarkWalk(b, n, func() {
			q.iterate(func(_ Entity, it *width4) bool {
				it.W0.X += 1
				return true
			})
		})
	}},
	{"tag-3", func(b *testing.B, n int) {
		_, q := widthWorld[tag3](b, n)
		benchmarkWalk(b, n, func() {
			for _, it := range q.All() {
				it.W0.X += 1
			}
		})
	}},
	{"tag-4", func(b *testing.B, n int) {
		_, q := widthWorld[tag4](b, n)
		benchmarkWalk(b, n, func() {
			for _, it := range q.All() {
				it.W0.X += 1
			}
		})
	}},
	{"without-3", func(b *testing.B, n int) {
		_, q := widthWorld[without3](b, n)
		benchmarkWalk(b, n, func() {
			for _, it := range q.All() {
				it.W0.X += 1
			}
		})
	}},
	{"without-4", func(b *testing.B, n int) {
		_, q := widthWorld[without4](b, n)
		benchmarkWalk(b, n, func() {
			for _, it := range q.All() {
				it.W0.X += 1
			}
		})
	}},
	// The hand-written rows do the Query's work over the typed Stores: walk
	// owners backwards, probe every other Store, copy every probed value to a
	// local as the Query's fill copies it to the buffer, and fold the copies
	// into W0 so the compiler cannot drop them.
	{"hand-1", func(b *testing.B, n int) {
		width, _ := widthWorld[width1](b, n)
		d := width.s0
		benchmarkWalk(b, n, func() {
			for row := len(d.owners) - 1; row >= 0; row-- {
				d.dense[row].X += 1
			}
		})
	}},
	{"hand-2", func(b *testing.B, n int) {
		width, _ := widthWorld[width2](b, n)
		d, s1 := width.s0, width.s1
		benchmarkWalk(b, n, func() {
			for row := len(d.owners) - 1; row >= 0; row-- {
				e := d.owners[row]
				r1, ok := s1.probe(e)
				if !ok {
					continue
				}
				v1 := s1.dense[r1]
				d.dense[row].X += 1 + v1.X
			}
		})
	}},
	{"hand-3", func(b *testing.B, n int) {
		width, _ := widthWorld[width3](b, n)
		d, s1, s2 := width.s0, width.s1, width.s2
		benchmarkWalk(b, n, func() {
			for row := len(d.owners) - 1; row >= 0; row-- {
				e := d.owners[row]
				r1, ok := s1.probe(e)
				if !ok {
					continue
				}
				r2, ok := s2.probe(e)
				if !ok {
					continue
				}
				v1, v2 := s1.dense[r1], s2.dense[r2]
				d.dense[row].X += 1 + v1.X + v2.X
			}
		})
	}},
	{"hand-4", func(b *testing.B, n int) {
		width, _ := widthWorld[width4](b, n)
		d, s1, s2, s3 := width.s0, width.s1, width.s2, width.s3
		benchmarkWalk(b, n, func() {
			for row := len(d.owners) - 1; row >= 0; row-- {
				e := d.owners[row]
				r1, ok := s1.probe(e)
				if !ok {
					continue
				}
				r2, ok := s2.probe(e)
				if !ok {
					continue
				}
				r3, ok := s3.probe(e)
				if !ok {
					continue
				}
				v1, v2, v3 := s1.dense[r1], s2.dense[r2], s3.dense[r3]
				d.dense[row].X += 1 + v1.X + v2.X + v3.X
			}
		})
	}},
}

// BenchmarkQueryWidth runs every row at two populations: at 1 000 every
// width's Stores fit in L2, at 10 000 the widest do not, so a step present at
// both is not a cache effect. Run one case per invocation, interleaved; the
// README's §What a wider Query costs describes the loop.
func BenchmarkQueryWidth(b *testing.B) {
	for _, c := range widthCases {
		b.Run(c.name, func(b *testing.B) {
			for _, n := range []int{1_000, 10_000} {
				b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) { c.run(b, n) })
			}
		})
	}
}

// pinWidth checks that a sweep Query measures what its row's name says: the
// first-declared field drives, the shape is the filler the width names, the walk
// is the whole population, and n Entities are yielded — through All(), and
// through iterate when a delegated row uses it.
func pinWidth[Q any](t *testing.T, n, width int, delegated bool) {
	t.Helper()
	_, q := widthWorld[Q](t, n)
	name := fmt.Sprintf("%T", *new(Q))
	q.bind()
	if q.fields[0].cursor.offset != 0 {
		t.Errorf("%s: the Driver is the field at offset %d, want the first-declared field", name, q.fields[0].cursor.offset)
	}
	if q.fields[0].filter {
		t.Errorf("%s: a filter drives", name)
	}
	if want := uint8(min(width, wideShape)); q.shape != want {
		t.Errorf("%s: shape %d, want %d", name, q.shape, want)
	}
	if len(q.walk) != n {
		t.Errorf("%s: the walk is %d long, want %d", name, len(q.walk), n)
	}
	yielded := 0
	for range q.All() {
		yielded++
	}
	if yielded != n {
		t.Errorf("%s: All() yielded %d Entities, want %d", name, yielded, n)
	}
	if delegated {
		yielded = 0
		q.iterate(func(Entity, *Q) bool { yielded++; return true })
		if yielded != n {
			t.Errorf("%s: iterate yielded %d Entities, want %d", name, yielded, n)
		}
	}
}

// TestTheWidthSweepDrivesOffItsFirstField is the one correctness test under
// BenchmarkQueryWidth. A benchmark over a wrongly-driven, wrongly-shaped or
// wrongly-filtered Query would still produce plausible numbers, so this pins the
// Driver slot, the shape, the walk length and the yield count together; it
// fails if a change to Driver eligibility or shape routing moves what the sweep
// measures. The delegated rows reuse the width-1 to width-4 Queries.
func TestTheWidthSweepDrivesOffItsFirstField(t *testing.T) {
	const n = 1_000
	pinWidth[width1](t, n, 1, true)
	pinWidth[width2](t, n, 2, true)
	pinWidth[width3](t, n, 3, true)
	pinWidth[width4](t, n, 4, true)
	pinWidth[width5](t, n, 5, false)
	pinWidth[width6](t, n, 6, false)
	pinWidth[width7](t, n, 7, false)
	pinWidth[width8](t, n, 8, false)
	pinWidth[tag3](t, n, 3, false)
	pinWidth[tag4](t, n, 4, false)
	pinWidth[without3](t, n, 3, false)
	pinWidth[without4](t, n, 4, false)
}
