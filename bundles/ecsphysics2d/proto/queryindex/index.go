package queryindex

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/libs/m"
)

// Index is what the benchmarks drive. The prototype puts an interface here so
// one harness covers every structure; cog#306 forbids one in the real surface,
// and the benchmarks call through it identically for every candidate.
type Index interface {
	Name() string
	Build(items []Placed)
	Sweep(from, to m.Vec2, radius float32, exclude ecs.Entity) (Hit, bool)
	SweepAll(dst []Hit, from, to m.Vec2, radius float32, exclude ecs.Entity) []Hit
	Blocked(from, to m.Vec2, radius float32, exclude ecs.Entity) bool
	Overlap(dst []ecs.Entity, at m.Vec2, radius float32, exclude ecs.Entity) []ecs.Entity
}

// entityOf is the prototype's stand-in for an Entity: slot i is Entity i+1.
func entityOf(i int32) ecs.Entity { return ecs.Entity(i + 1) }

type aabb struct{ minX, minY, maxX, maxY float32 }

func boundsOf(p Placed) aabb {
	e := p.Shape.extent()
	return aabb{p.At.X - e.X, p.At.Y - e.Y, p.At.X + e.X, p.At.Y + e.Y}
}

// store is the shapes every index keeps beside its structure, in slot order.
type store struct {
	shapes []Shape
	at     []m.Vec2
	box    []aabb
}

func (s *store) load(items []Placed) {
	s.shapes, s.at, s.box = s.shapes[:0], s.at[:0], s.box[:0]
	for _, it := range items {
		s.shapes = append(s.shapes, it.Shape)
		s.at = append(s.at, it.At)
		s.box = append(s.box, boundsOf(it))
	}
}

// insertSorted appends h keeping dst[from:] ordered by T, then entity.
func insertSorted(dst []Hit, from int, h Hit) []Hit {
	dst = append(dst, h)
	i := len(dst) - 1
	for i > from && less(h, dst[i-1]) {
		dst[i] = dst[i-1]
		i--
	}
	dst[i] = h
	return dst
}

func less(a, b Hit) bool { return a.T < b.T || (a.T == b.T && a.Entity < b.Entity) }

// sortDedup orders dst[from:] by T then entity and drops repeats of one
// entity, which a grid produces when a shape spans several cells.
func sortDedup(dst []Hit, from int) []Hit {
	for i := from + 1; i < len(dst); i++ {
		h := dst[i]
		j := i
		for j > from && less(h, dst[j-1]) {
			dst[j] = dst[j-1]
			j--
		}
		dst[j] = h
	}
	w := from
	for i := from; i < len(dst); i++ {
		if i > from && dst[i].Entity == dst[w-1].Entity && dst[i].T == dst[w-1].T {
			continue
		}
		dst[w] = dst[i]
		w++
	}
	return dst[:w]
}

// ---------------------------------------------------------------------------
// Linear: every shape, every query. The correctness oracle, and the floor a
// structure has to beat.

type Linear struct{ store }

func (*Linear) Name() string           { return "linear" }
func (l *Linear) Build(items []Placed) { l.load(items) }

func (l *Linear) Sweep(from, to m.Vec2, radius float32, exclude ecs.Entity) (Hit, bool) {
	ry := makeRay(from, to, radius)
	best, found := Hit{T: 2}, false
	for i := range l.shapes {
		e := entityOf(int32(i))
		if e == exclude {
			continue
		}
		if h, ok := sweep(&ry, l.shapes[i], l.at[i]); ok && (h.T < best.T || (h.T == best.T && e < best.Entity)) {
			h.Entity = e
			best, found = h, true
		}
	}
	return best, found
}

func (l *Linear) SweepAll(dst []Hit, from, to m.Vec2, radius float32, exclude ecs.Entity) []Hit {
	ry := makeRay(from, to, radius)
	start := len(dst)
	for i := range l.shapes {
		e := entityOf(int32(i))
		if e == exclude {
			continue
		}
		if h, ok := sweep(&ry, l.shapes[i], l.at[i]); ok {
			h.Entity = e
			dst = insertSorted(dst, start, h)
		}
	}
	return dst
}

func (l *Linear) Blocked(from, to m.Vec2, radius float32, exclude ecs.Entity) bool {
	_, ok := l.Sweep(from, to, radius, exclude)
	return ok
}

func (l *Linear) Overlap(dst []ecs.Entity, at m.Vec2, radius float32, exclude ecs.Entity) []ecs.Entity {
	for i := range l.shapes {
		if e := entityOf(int32(i)); e != exclude && overlapCircle(at, radius, l.shapes[i], l.at[i]) {
			dst = append(dst, e)
		}
	}
	return dst
}
