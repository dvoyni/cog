package types

import (
	"fmt"
	"testing"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// The wide and typed frames: the two Query shapes the unrolled fillers do not
// reach, each as a whole frame beside a hand-written baseline, the way
// BenchmarkFrameQuery sits beside BenchmarkFrameHandWritten (#257).
//
//   - wide is width5, five pointer-free Components, so it runs iterateWide, the
//     per-field loop.
//   - typed is two Components, one of which holds a string, so prepare routes it
//     to that same per-field loop for its typed copy, although it is only two
//     wide.
//
// Both populate every Entity with every Component in the same order, as the
// width sweep does, so every Store is the same length and the first-declared
// field drives.

// label is a Component holding a string, which is what routes a Query naming it
// to the per-field loop.
type label struct {
	X, Y float32
	Name string
}

// labelPlugin owns label, apart from componentsPlugin and widthPlugin so both
// keep the Stores they had.
type labelPlugin struct {
	ids    uint32
	labels *Store[label]
}

func (p *labelPlugin) Name() kernel.PluginName { return "label" }

func (p *labelPlugin) Dependencies() []kernel.PluginName { return []kernel.PluginName{Name} }

func (p *labelPlugin) Register(registrar *kernel.Registrar, _ any) error {
	p.labels = RegisterComponent[label](registrar, p.ids)
	return nil
}

// typedQuery is the typed shape: two fields, the read one holding a string.
type typedQuery struct {
	W0    *w0
	Label label
}

type fillSystem kernel.Subscription[app.UpdateEvent]

// fillWorld composes an engine with the width and label plugins beside the
// usual two, and gives n Entities w0 … w4 and a label.
func fillWorld(tb testing.TB, n int, subscribe func(*kernel.Registrar)) (*widthPlugin, *labelPlugin, *kernel.Engine) {
	tb.Helper()
	width := &widthPlugin{ids: uint32(n)}
	labels := &labelPlugin{ids: uint32(n)}
	entities, _, engine := newWorldWith(tb, uint32(n), subscribe,
		[]kernel.PluginName{Name, "components", "width", "label"}, width, labels)
	for i := range n {
		e := entities.alloc()
		width.s0.Set(e, w0{})
		width.s1.Set(e, w1{X: 1})
		width.s2.Set(e, w2{X: 2})
		width.s3.Set(e, w3{X: 3})
		width.s4.Set(e, w4{X: 4})
		labels.labels.Set(e, label{X: 1, Y: float32(i), Name: "label"})
	}
	return width, labels, engine
}

// benchmarkFillFrame is benchmarkFrameWith over fillWorld.
func benchmarkFillFrame(b *testing.B, n int, subscribe func(*kernel.Registrar)) {
	_, _, engine := fillWorld(b, n, subscribe)
	executioner := engine.Executioner()
	b.ReportAllocs()
	b.ResetTimer()
	// The classic b.N form, for the reason benchmarkFrameWith gives.
	for i := 0; i < b.N; i++ {
		executioner.PublishEvent(app.UpdateEvent{Dt: 1}).Wait()
	}
}

// The loop bodies, one per shape, shared by every arm so the arms differ only in
// how the Query is filled. Each reads every field it was handed, so no fill is
// dead work.

func wideQueryAll(q *Query[width5]) {
	for _, it := range q.All() {
		it.W0.X += it.W1.X + it.W2.X + it.W3.X + it.W4.X
	}
}

func typedQueryAll(q *Query[typedQuery]) {
	for _, it := range q.All() {
		it.W0.X += it.Label.X + float32(len(it.Label.Name))
	}
}

func subscribeSystem[Q any](system func(*Query[Q])) func(*kernel.Registrar) {
	return func(registrar *kernel.Registrar) {
		registrar.Subscribe[fillSystem](ToHandler[app.UpdateEvent](registrar, system))
	}
}

// wideHandWritten is the wide walk written out over the typed Stores: owners
// backwards, four probes, every probed value copied to a local.
func wideHandWritten() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var entities kernel.Read[*Entities]
	var s0 kernel.Write[*Store[w0]]
	var s1 kernel.Read[*Store[w1]]
	var s2 kernel.Read[*Store[w2]]
	var s3 kernel.Read[*Store[w3]]
	var s4 kernel.Read[*Store[w4]]
	return func(access kernel.ResourceAccess) {
			entities = access.GetRead[*Entities]()
			s0 = access.GetWrite[*Store[w0]]()
			s1 = access.GetRead[*Store[w1]]()
			s2 = access.GetRead[*Store[w2]]()
			s3 = access.GetRead[*Store[w3]]()
			s4 = access.GetRead[*Store[w4]]()
		}, func(_ kernel.Kernel, _ app.UpdateEvent) {
			_ = entities
			d, p1, p2, p3, p4 := s0.Get(), s1.Get(), s2.Get(), s3.Get(), s4.Get()
			for row := len(d.owners) - 1; row >= 0; row-- {
				e := d.owners[row]
				r1, ok := p1.probe(e)
				if !ok {
					continue
				}
				r2, ok := p2.probe(e)
				if !ok {
					continue
				}
				r3, ok := p3.probe(e)
				if !ok {
					continue
				}
				r4, ok := p4.probe(e)
				if !ok {
					continue
				}
				v1, v2, v3, v4 := p1.dense[r1], p2.dense[r2], p3.dense[r3], p4.dense[r4]
				d.dense[row].X += v1.X + v2.X + v3.X + v4.X
			}
		}
}

// typedHandWritten is the typed walk written out: one probe, the label copied
// to a local, string header and all.
func typedHandWritten() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var entities kernel.Read[*Entities]
	var s0 kernel.Write[*Store[w0]]
	var labels kernel.Read[*Store[label]]
	return func(access kernel.ResourceAccess) {
			entities = access.GetRead[*Entities]()
			s0 = access.GetWrite[*Store[w0]]()
			labels = access.GetRead[*Store[label]]()
		}, func(_ kernel.Kernel, _ app.UpdateEvent) {
			_ = entities
			d, p := s0.Get(), labels.Get()
			for row := len(d.owners) - 1; row >= 0; row-- {
				r, ok := p.probe(d.owners[row])
				if !ok {
					continue
				}
				v := p.dense[r]
				d.dense[row].X += v.X + float32(len(v.Name))
			}
		}
}

func subscribeWideHandWritten(registrar *kernel.Registrar) {
	registrar.Subscribe[fillSystem](wideHandWritten)
}

func subscribeTypedHandWritten(registrar *kernel.Registrar) {
	registrar.Subscribe[fillSystem](typedHandWritten)
}

func BenchmarkFrameWide1k(b *testing.B) { benchmarkFillFrame(b, 1_000, subscribeSystem(wideQueryAll)) }
func BenchmarkFrameWide10k(b *testing.B) {
	benchmarkFillFrame(b, 10_000, subscribeSystem(wideQueryAll))
}

func BenchmarkFrameWideHandWritten1k(b *testing.B) {
	benchmarkFillFrame(b, 1_000, subscribeWideHandWritten)
}

func BenchmarkFrameWideHandWritten10k(b *testing.B) {
	benchmarkFillFrame(b, 10_000, subscribeWideHandWritten)
}

func BenchmarkFrameTyped1k(b *testing.B) {
	benchmarkFillFrame(b, 1_000, subscribeSystem(typedQueryAll))
}
func BenchmarkFrameTyped10k(b *testing.B) {
	benchmarkFillFrame(b, 10_000, subscribeSystem(typedQueryAll))
}

func BenchmarkFrameTypedHandWritten1k(b *testing.B) {
	benchmarkFillFrame(b, 1_000, subscribeTypedHandWritten)
}

func BenchmarkFrameTypedHandWritten10k(b *testing.B) {
	benchmarkFillFrame(b, 10_000, subscribeTypedHandWritten)
}

// fillFrameAllocations is the steady-state count TestTheFrameSitsOnTheEnginesAllocationLine
// takes, over fillWorld: objects a frame across ten thousand frames, after a
// hundred to warm every pool.
func fillFrameAllocations(t *testing.T, n int, subscribe func(*kernel.Registrar)) float64 {
	t.Helper()
	const frames = 10_000
	_, _, engine := fillWorld(t, n, subscribe)
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

// TestTheWideAndTypedFramesSitOnTheAllocationLine holds the two new shapes to
// the line the two-Component Query sits on: what the hand-written walk costs,
// and flat in the Entity count.
func TestTheWideAndTypedFramesSitOnTheAllocationLine(t *testing.T) {
	if raceEnabled {
		t.Skip("allocation counts are not meaningful under -race")
	}
	hand := fillFrameAllocations(t, 1_000, subscribeWideHandWritten)
	for _, arm := range []struct {
		name      string
		subscribe func(*kernel.Registrar)
	}{
		{"wide", subscribeSystem(wideQueryAll)},
		{"typed", subscribeSystem(typedQueryAll)},
		{"typed hand-written", subscribeTypedHandWritten},
	} {
		at1k := fillFrameAllocations(t, 1_000, arm.subscribe)
		at10k := fillFrameAllocations(t, 10_000, arm.subscribe)
		t.Logf("%s: %.3f objects a frame at 1k, %.3f at 10k (wide hand-written %.3f)", arm.name, at1k, at10k, hand)
		if at1k > hand+0.05 {
			t.Errorf("%s costs %.3f objects a frame against the hand-written %.3f", arm.name, at1k, hand)
		}
		if at10k > at1k+0.05 {
			t.Errorf("%s allocates per Entity: %.3f a frame at 1k, %.3f at 10k", arm.name, at1k, at10k)
		}
	}
}

// TestTheWideAndTypedShapesTakeThePerFieldLoop pins what the two benchmarks
// measure: both Queries are planned to the per-field loop, the first-declared
// field drives, and one frame applies the loop body to every Entity.
func TestTheWideAndTypedShapesTakeThePerFieldLoop(t *testing.T) {
	pinPerFieldLoop(t, wideQueryAll, 1+2+3+4)
	pinPerFieldLoop(t, typedQueryAll, 1+float32(len("label")))
}

func pinPerFieldLoop[Q any](t *testing.T, system func(*Query[Q]), want float32) {
	t.Helper()
	const n = 100
	var query *Query[Q]
	width, _, engine := fillWorld(t, n, subscribeSystem(func(q *Query[Q]) {
		query = q
		system(q)
	}))
	frame(t, engine, 1)
	name := fmt.Sprintf("%T", *new(Q))
	if query.shape != wideShape {
		t.Fatalf("%s: shape %d, want %d", name, query.shape, wideShape)
	}
	if query.fields[0].cursor.offset != 0 {
		t.Fatalf("%s: the field at offset %d drives, want the first-declared", name, query.fields[0].cursor.offset)
	}
	for row := range width.s0.dense {
		if got := width.s0.dense[row].X; got != want {
			t.Fatalf("%s: row %d: W0.X = %v after one frame, want %v", name, row, got, want)
		}
	}
}
