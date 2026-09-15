package queryindex

// PROTOTYPE (cog#345). Throwaway; never merges.
//
// The reference workload, taken from nox as the ticket allows. Every layout is
// generated from a fixed seed so every index, and the third-party baselines in
// ../baseline, see identical geometry and identical query sets.

import (
	"math/rand/v2"

	"github.com/dvoyni/cog/libs/m"
)

// MapSize is nox's worst-case map bound: 256 cells of 2 m.
const MapSize = 512

// Placed is one shape standing on the plane.
type Placed struct {
	Shape Shape
	At    m.Vec2
}

// Query is one sweep: from, to and a radius.
type Query struct {
	From, To m.Vec2
	Radius   float32
}

func rng(seed uint64) *rand.Rand { return rand.New(rand.NewPCG(seed, 0x345)) }

func uniform(r *rand.Rand, lo, hi float32) float32 { return lo + (hi-lo)*r.Float32() }

// Scatter is the survey's randomised case: 4096 two-metre axis-aligned wall
// segments on random 2 m cell edges across the whole map.
func Scatter() []Placed {
	r := rng(1)
	out := make([]Placed, 0, 4096)
	for range 4096 {
		cx, cy := float32(r.IntN(256))*2, float32(r.IntN(256))*2
		if r.IntN(2) == 0 {
			out = append(out, Placed{Segment(m.Vec2{X: 1}), m.Vec2{X: cx + 1, Y: cy}})
		} else {
			out = append(out, Placed{Segment(m.Vec2{Y: 1}), m.Vec2{X: cx, Y: cy + 1}})
		}
	}
	return out
}

// Rooms is a building: wall lines every pitch metres in both axes, each span
// between crossings broken by one 2 m doorway at a random 2 m offset. merged
// emits each run between doorways as one segment (the app merged its colinear
// cells); unmerged emits one segment per 2 m cell.
func Rooms(pitch int, merged bool) []Placed {
	r := rng(2 + uint64(pitch))
	var out []Placed
	emit := func(horizontal bool, line, a, b float32) { // run from a to b along the line
		if b <= a {
			return
		}
		if !merged {
			for s := a; s < b; s += 2 {
				emitRun(&out, horizontal, line, s, s+2)
			}
			return
		}
		emitRun(&out, horizontal, line, a, b)
	}
	cellsPerSpan := pitch / 2
	for axis := range 2 {
		for li := 0; li*pitch <= MapSize; li++ {
			line := float32(li * pitch)
			for sp := 0; sp*pitch < MapSize; sp++ {
				base := float32(sp * pitch)
				door := base + float32(r.IntN(cellsPerSpan))*2
				emit(axis == 0, line, base, door)
				emit(axis == 0, line, door+2, base+float32(pitch))
			}
		}
	}
	return out
}

func emitRun(out *[]Placed, horizontal bool, line, a, b float32) {
	half, mid := (b-a)/2, (a+b)/2
	if horizontal {
		*out = append(*out, Placed{Segment(m.Vec2{X: half}), m.Vec2{X: mid, Y: line}})
	} else {
		*out = append(*out, Placed{Segment(m.Vec2{Y: half}), m.Vec2{X: line, Y: mid}})
	}
}

// Bodies places n bodies uniformly in a side-metre square centred on the map,
// in nox's shape mix (§2): 70% creatures (circles 0.49–0.74 m), 10% boulders
// (circle 1.97 m), 10% crates (box 2.1 × 1.0 m), 10% blocks (box 2.6 m).
// Overlaps are allowed; nothing here resolves contacts.
func Bodies(n int, side float32, seed uint64) []Placed {
	r := rng(100 + seed)
	lo := float32(MapSize)/2 - side/2
	out := make([]Placed, n)
	for i := range out {
		at := m.Vec2{X: lo + side*r.Float32(), Y: lo + side*r.Float32()}
		var s Shape
		switch k := r.IntN(10); {
		case k < 7:
			s = Circle(uniform(r, 0.49, 0.74))
		case k == 7:
			s = Circle(1.97)
		case k == 8:
			s = Box(m.Vec2{X: 1.05, Y: 0.5})
		default:
			s = Box(m.Vec2{X: 1.3, Y: 1.3})
		}
		out[i] = Placed{s, at}
	}
	return out
}

// Moved returns the same bodies displaced by up to step metres each, which is
// what one sub-step of motion does to the body index.
func Moved(bodies []Placed, step float32, seed uint64) []Placed {
	r := rng(200 + seed)
	out := make([]Placed, len(bodies))
	for i, b := range bodies {
		b.At.X += uniform(r, -step, step)
		b.At.Y += uniform(r, -step, step)
		out[i] = b
	}
	return out
}

// Queries is a query set of n sweeps starting uniformly in a side-metre square
// centred on the map, in a uniformly random direction, with length drawn from
// [minLen, maxLen] and radius from [minR, maxR]. A negative minLen means a
// full-map line: both endpoints uniform over the square.
func Queries(n int, side, minLen, maxLen, minR, maxR float32, seed uint64) []Query {
	r := rng(300 + seed)
	lo := float32(MapSize)/2 - side/2
	out := make([]Query, n)
	for i := range out {
		from := m.Vec2{X: lo + side*r.Float32(), Y: lo + side*r.Float32()}
		var to m.Vec2
		if minLen < 0 {
			to = m.Vec2{X: lo + side*r.Float32(), Y: lo + side*r.Float32()}
		} else {
			a := uniform(r, 0, 2*3.14159265)
			l := uniform(r, minLen, maxLen)
			to = m.Vec2{X: from.X + l*cos32(a), Y: from.Y + l*sin32(a)}
		}
		out[i] = Query{from, to, uniform(r, minR, maxR)}
	}
	return out
}

// The query mixes, per §6.
//
//	proj     Q1/Q2: a point projectile's sub-step, 0.4–1.0 m (§6's 0.44–0.96 m)
//	body     a body-radius sweep: r 0.49–0.74 m over 0.1–1.0 m
//	lospair  Q3: line of sight between a candidate contact pair, 1–4 m
//	sight    Q4: AI sight, 5–31.5 m
//	losmap   the survey's full-map line of sight
type Mix struct {
	Name                       string
	MinLen, MaxLen, MinR, MaxR float32
}

var Mixes = []Mix{
	{"proj", 0.4, 1.0, 0, 0},
	{"body", 0.1, 1.0, 0.49, 0.74},
	{"lospair", 1, 4, 0, 0},
	{"sight", 5, 31.5, 0, 0},
	{"losmap", -1, -1, 0, 0},
}

// StaticLayouts are the static geometry sets.
//
//	scatter       the survey's 4096 random 2 m segments (reused from cog#284/#285)
//	rooms16       16 m rooms, runs merged between doorways: ~7 m segments
//	rooms16cells  the same walls, one segment per 2 m cell
//	halls64       64 m halls, merged: ~31 m segments, far larger than a cell
var StaticLayouts = []struct {
	Name string
	Make func() []Placed
}{
	{"scatter", Scatter},
	{"rooms16", func() []Placed { return Rooms(16, true) }},
	{"rooms16cells", func() []Placed { return Rooms(16, false) }},
	{"halls64", func() []Placed { return Rooms(64, true) }},
}

// BodyLayouts are the body sets: a melee crowd, a busy arena, and the whole
// map populated.
var BodyLayouts = []struct {
	Name string
	N    int
	Side float32
}{
	{"crowd256", 256, 32},
	{"arena1024", 1024, 128},
	{"map4096", 4096, 512},
}

// QueryCount is the size of every query set; benchmarks cycle through it.
const QueryCount = 4096

// QuerySet is mix's query set over a side-metre square, seeded by the mix.
func QuerySet(mix Mix, side float32) []Query {
	seed := uint64(0)
	for _, c := range mix.Name {
		seed = seed*31 + uint64(c)
	}
	return Queries(QueryCount, side, mix.MinLen, mix.MaxLen, mix.MinR, mix.MaxR, seed)
}
