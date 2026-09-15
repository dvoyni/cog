package queryindex

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/libs/m"
)

// The prototype's tests only exist so the benchmarks measure indices that give
// the right answer: hand-computed primitive cases, every index against the
// linear oracle, and the allocation rule.

func v(x, y float32) m.Vec2 { return m.Vec2{X: x, Y: y} }

func near(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-4 }

func TestPrimitives(t *testing.T) {
	cases := []struct {
		name     string
		from, to m.Vec2
		r        float32
		s        Shape
		at       m.Vec2
		hit      bool
		T        float32
		point, n m.Vec2
	}{
		{"point vs segment", v(0, 0), v(10, 0), 0, Segment(v(0, 1)), v(5, 0), true, 0.5, v(5, 0), v(-1, 0)},
		{"circle vs segment face", v(0, 0), v(10, 0), 0.5, Segment(v(0, 1)), v(5, 0), true, 0.45, v(5, 0), v(-1, 0)},
		{"circle vs segment end", v(0, 1.3), v(10, 1.3), 0.5, Segment(v(0, 1)), v(5, 0), true, 0.46, v(5, 1), v(-0.8, 0.6)},
		{"circle misses segment end", v(0, 1.6), v(10, 1.6), 0.5, Segment(v(0, 1)), v(5, 0), false, 0, v(0, 0), v(0, 0)},
		{"point along segment", v(0, 0), v(10, 0), 0, Segment(v(1, 0)), v(5, 0), false, 0, v(0, 0), v(0, 0)},
		{"circle vs box corner", v(0, 1.3), v(10, 1.3), 0.5, Box(v(1, 1)), v(5, 0), true, 0.36, v(4, 1), v(-0.8, 0.6)},
		{"circle vs box face", v(0, 0.5), v(10, 0.5), 0.5, Box(v(1, 1)), v(5, 0), true, 0.35, v(4, 0.5), v(-1, 0)},
		{"point vs box from above", v(5, 5), v(5, -5), 0, Box(v(1, 1)), v(5, 0), true, 0.4, v(5, 1), v(0, 1)},
		{"point starts in box", v(5.5, 0.2), v(10, 0), 0, Box(v(1, 1)), v(5, 0), true, 0, v(6, 0.2), v(1, 0)},
		{"circle vs circle", v(0, 0), v(10, 0), 0.5, Circle(1), v(5, 0), true, 0.35, v(4, 0), v(-1, 0)},
		{"point vs point", v(0, 0), v(10, 0), 0, Circle(0), v(5, 0), false, 0, v(0, 0), v(0, 0)},
		{"point vs circle grazing", v(0, 1), v(10, 1), 0, Circle(1), v(5, 0), true, 0.5, v(5, 1), v(0, 1)},
		{"zero length inside", v(5, 0), v(5, 0), 0.5, Circle(1), v(5.5, 0), true, 0, v(4.5, 0), v(-1, 0)},
	}
	for _, c := range cases {
		h, ok := SweepShape(c.from, c.to, c.r, c.s, c.at)
		if ok != c.hit {
			t.Errorf("%s: hit %v, want %v (%+v)", c.name, ok, c.hit, h)
			continue
		}
		if ok && (!near(h.T, c.T) || !near(h.Point.X, c.point.X) || !near(h.Point.Y, c.point.Y) || !near(h.Normal.X, c.n.X) || !near(h.Normal.Y, c.n.Y)) {
			t.Errorf("%s: got T %v point %v normal %v, want %v %v %v", c.name, h.T, h.Point, h.Normal, c.T, c.point, c.n)
		}
	}
}

type candidate struct {
	name string
	make func() Index
}

func staticCandidates() []candidate {
	return []candidate{
		{"linear", func() Index { return &Linear{} }},
		{"grid1m", func() Index { return NewGrid(1) }},
		{"grid2m", func() Index { return NewGrid(2) }},
		{"grid4m", func() Index { return NewGrid(4) }},
		{"grid8m", func() Index { return NewGrid(8) }},
		{"hash1m", func() Index { return NewHashGrid(1) }},
		{"hash2m", func() Index { return NewHashGrid(2) }},
		{"hash4m", func() Index { return NewHashGrid(4) }},
		{"bvh2", func() Index { return NewBVH(2) }},
		{"bvh4", func() Index { return NewBVH(4) }},
		{"bvh8", func() Index { return NewBVH(8) }},
	}
}

func bodyCandidates() []candidate {
	return append(staticCandidates(),
		candidate{"bvh4stale4m", func() Index { return &StaleBVH{BVH: NewBVH(4), Drift: 4} }},
		candidate{"bvh4stale16m", func() Index { return &StaleBVH{BVH: NewBVH(4), Drift: 16} }},
	)
}

// StaleBVH is a BVH whose tree was built where the bodies stood Drift metres
// ago and has only been refit since.
type StaleBVH struct {
	*BVH
	Drift float32
}

func (s *StaleBVH) Name() string { return fmt.Sprintf("%s-stale%gm", s.BVH.Name(), s.Drift) }
func (s *StaleBVH) Build(items []Placed) {
	s.BVH.Build(Moved(items, s.Drift, 7))
	s.BVH.Move(items)
	s.BVH.Refit()
}

func bodyExclude(i int, n int) ecs.Entity { return entityOf(int32((i * 7919) % n)) }

func TestAgainstLinear(t *testing.T) {
	const n = 1024
	check := func(t *testing.T, items []Placed, qs []Query, idx Index, oracle *Linear, exclude func(int) ecs.Entity) {
		idx.Build(items)
		var got, want []Hit
		var ge, we []ecs.Entity
		bad := 0
		for i, q := range qs[:n] {
			ex := exclude(i)
			wh, wok := oracle.Sweep(q.From, q.To, q.Radius, ex)
			gh, gok := idx.Sweep(q.From, q.To, q.Radius, ex)
			if wok != gok || (wok && (!near(wh.T, gh.T) || (wh.Entity != gh.Entity && wh.T != gh.T))) {
				if bad++; bad <= 3 {
					t.Errorf("%s Sweep %d %+v: got %v %+v, want %v %+v", idx.Name(), i, q, gok, gh, wok, wh)
				}
			}
			if b := idx.Blocked(q.From, q.To, q.Radius, ex); b != wok {
				if bad++; bad <= 3 {
					t.Errorf("%s Blocked %d: got %v want %v", idx.Name(), i, b, wok)
				}
			}
			want = oracle.SweepAll(want[:0], q.From, q.To, q.Radius, ex)
			got = idx.SweepAll(got[:0], q.From, q.To, q.Radius, ex)
			if !sameHits(got, want) {
				if bad++; bad <= 3 {
					t.Errorf("%s SweepAll %d %+v: got %d hits %v, want %d %v", idx.Name(), i, q, len(got), got, len(want), want)
				}
			}
			r := []float32{0, 0.62, 3.1, 36.9}[i%4]
			we = oracle.Overlap(we[:0], q.From, r, ex)
			ge = idx.Overlap(ge[:0], q.From, r, ex)
			slices.Sort(we)
			slices.Sort(ge)
			if !slices.Equal(we, ge) {
				if bad++; bad <= 3 {
					t.Errorf("%s Overlap %d r %v: got %d, want %d", idx.Name(), i, r, len(ge), len(we))
				}
			}
		}
	}
	for _, l := range StaticLayouts {
		items := l.Make()
		oracle := &Linear{}
		oracle.Build(items)
		for _, mix := range Mixes {
			qs := QuerySet(mix, MapSize)
			for _, c := range staticCandidates()[1:] {
				t.Run(l.Name+"/"+mix.Name+"/"+c.name, func(t *testing.T) {
					check(t, items, qs, c.make(), oracle, func(int) ecs.Entity { return 0 })
				})
			}
		}
	}
	for _, l := range BodyLayouts {
		items := Bodies(l.N, l.Side, 0)
		oracle := &Linear{}
		oracle.Build(items)
		for _, mix := range Mixes[:4] {
			qs := QuerySet(mix, l.Side)
			for _, c := range bodyCandidates()[1:] {
				t.Run(l.Name+"/"+mix.Name+"/"+c.name, func(t *testing.T) {
					check(t, items, qs, c.make(), oracle, func(i int) ecs.Entity { return bodyExclude(i, l.N) })
				})
			}
		}
	}
}

func sameHits(a, b []Hit) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !near(a[i].T, b[i].T) {
			return false
		}
	}
	ea, eb := make([]ecs.Entity, len(a)), make([]ecs.Entity, len(b))
	for i := range a {
		ea[i], eb[i] = a[i].Entity, b[i].Entity
	}
	slices.Sort(ea)
	slices.Sort(eb)
	return slices.Equal(ea, eb)
}

func TestZeroAllocations(t *testing.T) {
	statics := Rooms(16, true)
	bodies := Bodies(1024, 128, 0)
	moved := Moved(bodies, 0.11, 1)
	i := 0
	for _, c := range bodyCandidates() {
		for _, set := range []struct {
			name  string
			items []Placed
			side  float32
		}{{"static", statics, MapSize}, {"bodies", bodies, 128}} {
			idx := c.make()
			idx.Build(set.items)
			for _, mix := range Mixes {
				qs := QuerySet(mix, set.side)
				dst := make([]Hit, 0, 4096)
				ents := make([]ecs.Entity, 0, 4096)
				i := 0
				allocs := testing.AllocsPerRun(200, func() {
					q := qs[i%len(qs)]
					i++
					idx.Sweep(q.From, q.To, q.Radius, 0)
					idx.Blocked(q.From, q.To, q.Radius, 0)
					dst = idx.SweepAll(dst[:0], q.From, q.To, q.Radius, 0)
					ents = idx.Overlap(ents[:0], q.From, 3.1, 0)
				})
				if allocs != 0 {
					t.Errorf("%s %s %s: %v allocs per query round", c.name, set.name, mix.Name, allocs)
				}
			}
		}
		// rebuild and refit, warmed; the stale wrapper allocates its displaced copy
		if strings.Contains(c.name, "stale") {
			continue
		}
		idx := c.make()
		for range 10 { // bucket capacities settle over a few alternations
			idx.Build(bodies)
			idx.Build(moved)
		}
		i := 0
		if allocs := testing.AllocsPerRun(50, func() {
			if i++; i%2 == 0 {
				idx.Build(bodies)
			} else {
				idx.Build(moved)
			}
		}); allocs != 0 {
			t.Errorf("%s rebuild: %v allocs", c.name, allocs)
		}
		if b, ok := idx.(*BVH); ok {
			if allocs := testing.AllocsPerRun(50, func() { b.Move(moved); b.Refit() }); allocs != 0 {
				t.Errorf("%s refit: %v allocs", c.name, allocs)
			}
		}
	}
	gu := NewHashGrid(2)
	gu.Build(bodies)
	gu.Update(moved)
	if allocs := testing.AllocsPerRun(50, func() {
		if i++; i%2 == 0 {
			gu.Update(bodies)
		} else {
			gu.Update(moved)
		}
	}); allocs != 0 {
		t.Errorf("hash update: %v allocs", allocs)
	}
	checkUpdated(t, gu, bodies)
	gd := NewGrid(2)
	gd.Build(bodies)
	gd.Update(moved)
	if allocs := testing.AllocsPerRun(50, func() {
		if i++; i%2 == 0 {
			gd.Update(bodies)
		} else {
			gd.Update(moved)
		}
	}); allocs != 0 {
		t.Errorf("grid update: %v allocs", allocs)
	}
	checkUpdated(t, gd, bodies)
	g := NewGrid(2)
	g.Build(statics)

	door := statics[10]
	open := Placed{Segment(m.Vec2{}), door.At}
	if allocs := testing.AllocsPerRun(50, func() {
		if i++; i%2 == 0 {
			g.Replace(10, door)
		} else {
			g.Replace(10, open)
		}
	}); allocs != 0 {
		t.Errorf("grid replace: %v allocs", allocs)
	}
}

type updater interface {
	Index
	Update(items []Placed)
}

// checkUpdated moves idx far from where it was built and checks it answers as
// the oracle does.
func checkUpdated(t *testing.T, idx updater, bodies []Placed) {
	far := Moved(bodies, 3, 9)
	idx.Update(far)
	oracle := &Linear{}
	oracle.Build(far)
	for _, q := range QuerySet(Mixes[3], 128)[:512] {
		wh, wok := oracle.Sweep(q.From, q.To, q.Radius, 0)
		gh, gok := idx.Sweep(q.From, q.To, q.Radius, 0)
		if wok != gok || (wok && !near(wh.T, gh.T)) {
			t.Fatalf("%s after Update disagrees with oracle: %+v vs %+v", idx.Name(), gh, wh)
		}
	}
}

// TestHashUnbounded places geometry far outside a dense grid's extent, at
// negative and large coordinates, which only the hash grid indexes as cells.
func TestHashUnbounded(t *testing.T) {
	off := v(-6000, 9000)
	shift := func(items []Placed) []Placed {
		out := make([]Placed, len(items))
		for i, it := range items {
			it.At = it.At.Add(off)
			out[i] = it
		}
		return out
	}
	items := shift(Rooms(16, true))
	oracle, h := &Linear{}, NewHashGrid(2)
	oracle.Build(items)
	h.Build(items)
	for _, mix := range Mixes {
		for i, q := range QuerySet(mix, MapSize)[:512] {
			q.From, q.To = q.From.Add(off), q.To.Add(off)
			wh, wok := oracle.Sweep(q.From, q.To, q.Radius, 0)
			gh, gok := h.Sweep(q.From, q.To, q.Radius, 0)
			if wok != gok || (wok && !near(wh.T, gh.T)) {
				t.Fatalf("%s %d: got %v %+v, want %v %+v", mix.Name, i, gok, gh, wok, wh)
			}
		}
	}
}
