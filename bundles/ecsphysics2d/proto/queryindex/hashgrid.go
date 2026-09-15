package queryindex

import (
	"fmt"
	"math"
	"math/bits"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/libs/m"
)

// HashGrid is Grid without an extent: cells are addressed by integer cell
// coordinates hashed into a power-of-two bucket table, so the plane is
// unbounded. A bucket holds entries tagged with their cell, and a visit skips
// entries another cell collided into. Traversal is Grid's: a rectangle scan
// for short sweeps, an ordered walk with early-out for long ones.
type HashGrid struct {
	store
	cell, inv float32
	buckets   [][]hashEntry
	mask      uint32
	first     []cellXY
	touched   []uint32
	listed    []bool
	RectLimit int
}

type hashEntry struct {
	x, y int32
	slot int32
}

func NewHashGrid(cell float32) *HashGrid {
	return &HashGrid{cell: cell, inv: 1 / cell, RectLimit: 16}
}

func (g *HashGrid) Name() string { return fmt.Sprintf("hash%gm", g.cell) }

// coord is the unclamped cell coordinate.
func (g *HashGrid) coord(v float32) int {
	return int(floor32(v * g.inv))
}

func (g *HashGrid) bucket(x, y int) uint32 {
	h := uint32(x)*0x9E3779B1 ^ uint32(y)*0x85EBCA77
	h ^= h >> 15
	return h & g.mask
}

// Build sizes the table to twice the cell listings, rounded up to a power of
// two, and reuses it when that size has not changed.
func (g *HashGrid) Build(items []Placed) {
	g.load(items)
	listings := 0
	for _, b := range g.box {
		listings += (g.coord(b.maxX) - g.coord(b.minX) + 1) * (g.coord(b.maxY) - g.coord(b.minY) + 1)
	}
	size := 1 << bits.Len(uint(max(2*listings, 1024)-1))
	if size > len(g.buckets) { // grow only, so alternating builds settle
		g.buckets = make([][]hashEntry, size)
		g.listed = make([]bool, size)
		g.touched = g.touched[:0]
		g.mask = uint32(size - 1)
	}
	for _, bi := range g.touched {
		g.buckets[bi] = g.buckets[bi][:0]
		g.listed[bi] = false
	}
	g.touched = g.touched[:0]
	g.first = g.first[:0]
	for i := range g.box {
		g.first = append(g.first, cellXY{})
		g.insertCells(int32(i))
	}
}

func (g *HashGrid) insertCells(i int32) {
	b := g.box[i]
	x0, x1, y0, y1 := g.coord(b.minX), g.coord(b.maxX), g.coord(b.minY), g.coord(b.maxY)
	g.first[i] = cellXY{int32(x0), int32(y0)}
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			bi := g.bucket(x, y)
			if !g.listed[bi] {
				g.listed[bi] = true
				g.touched = append(g.touched, bi)
			}
			g.buckets[bi] = append(g.buckets[bi], hashEntry{int32(x), int32(y), i})
		}
	}
}

// Replace swaps slot i for a new shape in place.
func (g *HashGrid) Replace(i int32, p Placed) {
	b := g.box[i]
	x0, x1, y0, y1 := g.coord(b.minX), g.coord(b.maxX), g.coord(b.minY), g.coord(b.maxY)
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			bi := g.bucket(x, y)
			c := g.buckets[bi]
			for k, e := range c {
				if e.slot == i && int(e.x) == x && int(e.y) == y {
					c[k] = c[len(c)-1]
					g.buckets[bi] = c[:len(c)-1]
					break
				}
			}
		}
	}
	g.shapes[i], g.at[i], g.box[i] = p.Shape, p.At, boundsOf(p)
	g.insertCells(i)
}

// Update moves the same slots, re-listing only a slot whose cell range changed.
func (g *HashGrid) Update(items []Placed) {
	for i, it := range items {
		nb := boundsOf(it)
		ob := g.box[i]
		if g.coord(nb.minX) == g.coord(ob.minX) && g.coord(nb.maxX) == g.coord(ob.maxX) &&
			g.coord(nb.minY) == g.coord(ob.minY) && g.coord(nb.maxY) == g.coord(ob.maxY) {
			g.shapes[i], g.at[i], g.box[i] = it.Shape, it.At, nb
			continue
		}
		g.Replace(int32(i), it)
	}
}

func (g *HashGrid) sweepRect(ry *ray) rect {
	minX, maxX := min32(ry.p0.X, ry.p0.X+ry.d.X)-ry.r, max32(ry.p0.X, ry.p0.X+ry.d.X)+ry.r
	minY, maxY := min32(ry.p0.Y, ry.p0.Y+ry.d.Y)-ry.r, max32(ry.p0.Y, ry.p0.Y+ry.d.Y)+ry.r
	return rect{g.coord(minX), g.coord(maxX), g.coord(minY), g.coord(maxY)}
}

func (g *HashGrid) home(i int32, x, y int, q rect) bool {
	f := g.first[i]
	return x == max(int(f.x), q.x0) && y == max(int(f.y), q.y0)
}

// hashWalker is Grid's walker over unbounded cell coordinates.
type hashWalker struct {
	g         *HashGrid
	ry        *ray
	k, cx, cy int
	sx, sy    int
	batch     rect
	bx, by    int
	stepX     bool
}

func (g *HashGrid) walker(ry *ray) hashWalker {
	w := hashWalker{g: g, ry: ry, k: int(ceil32(ry.r * g.inv)), cx: g.coord(ry.p0.X), cy: g.coord(ry.p0.Y), sx: 1, sy: 1}
	if ry.d.X < 0 {
		w.sx = -1
	}
	if ry.d.Y < 0 {
		w.sy = -1
	}
	w.setBatch(rect{w.cx - w.k, w.cx + w.k, w.cy - w.k, w.cy + w.k})
	return w
}

func (w *hashWalker) setBatch(q rect) { w.batch, w.bx, w.by = q, q.x0, q.y0 }

// next yields the next cell of the current batch as coordinates.
func (w *hashWalker) next() (int, int, bool) {
	q := &w.batch
	if w.by > q.y1 {
		return 0, 0, false
	}
	x, y := w.bx, w.by
	w.bx++
	if w.bx > q.x1 {
		w.bx = q.x0
		w.by++
	}
	return x, y, true
}

func (w *hashWalker) exit() float32 {
	g, ry := w.g, w.ry
	tx, ty := float32(math.Inf(1)), float32(math.Inf(1))
	if ry.d.X != 0 {
		bx := w.cx
		if w.sx > 0 {
			bx++
		}
		tx = (float32(bx)*g.cell - ry.p0.X) * ry.inv.X
	}
	if ry.d.Y != 0 {
		by := w.cy
		if w.sy > 0 {
			by++
		}
		ty = (float32(by)*g.cell - ry.p0.Y) * ry.inv.Y
	}
	w.stepX = tx < ty
	return min32(tx, ty)
}

func (w *hashWalker) advance() {
	if w.stepX {
		w.cx += w.sx
		x := w.cx + w.sx*w.k
		w.setBatch(rect{x, x, w.cy - w.k, w.cy + w.k})
	} else {
		w.cy += w.sy
		y := w.cy + w.sy*w.k
		w.setBatch(rect{w.cx - w.k, w.cx + w.k, y, y})
	}
}

func (g *HashGrid) Sweep(from, to m.Vec2, radius float32, exclude ecs.Entity) (Hit, bool) {
	ry := makeRay(from, to, radius)
	best, found := Hit{T: 2}, false
	if q := g.sweepRect(&ry); q.cells() <= g.RectLimit {
		for y := q.y0; y <= q.y1; y++ {
			for x := q.x0; x <= q.x1; x++ {
				for _, en := range g.buckets[g.bucket(x, y)] {
					i := en.slot
					e := entityOf(i)
					if int(en.x) != x || int(en.y) != y || e == exclude || !g.home(i, x, y, q) {
						continue
					}
					if h, ok := sweep(&ry, g.shapes[i], g.at[i]); ok && (h.T < best.T || (h.T == best.T && e < best.Entity)) {
						h.Entity = e
						best, found = h, true
					}
				}
			}
		}
		return best, found
	}
	margin := 1e-3 * ry.inv.X
	if abs32(ry.d.Y) > abs32(ry.d.X) {
		margin = 1e-3 * ry.inv.Y
	}
	margin = abs32(margin)
	w := g.walker(&ry)
	for {
		for x, y, ok := w.next(); ok; x, y, ok = w.next() {
			for _, en := range g.buckets[g.bucket(x, y)] {
				i := en.slot
				e := entityOf(i)
				if int(en.x) != x || int(en.y) != y || e == exclude {
					continue
				}
				if h, ok := sweep(&ry, g.shapes[i], g.at[i]); ok && (h.T < best.T || (h.T == best.T && e < best.Entity)) {
					h.Entity = e
					best, found = h, true
				}
			}
		}
		exit := w.exit()
		if exit > 1 || (found && best.T+margin < exit) {
			return best, found
		}
		w.advance()
	}
}

func (g *HashGrid) SweepAll(dst []Hit, from, to m.Vec2, radius float32, exclude ecs.Entity) []Hit {
	ry := makeRay(from, to, radius)
	start := len(dst)
	if q := g.sweepRect(&ry); q.cells() <= g.RectLimit {
		for y := q.y0; y <= q.y1; y++ {
			for x := q.x0; x <= q.x1; x++ {
				for _, en := range g.buckets[g.bucket(x, y)] {
					i := en.slot
					e := entityOf(i)
					if int(en.x) != x || int(en.y) != y || e == exclude || !g.home(i, x, y, q) {
						continue
					}
					if h, ok := sweep(&ry, g.shapes[i], g.at[i]); ok {
						h.Entity = e
						dst = insertSorted(dst, start, h)
					}
				}
			}
		}
		return dst
	}
	w := g.walker(&ry)
	for {
		for x, y, ok := w.next(); ok; x, y, ok = w.next() {
			for _, en := range g.buckets[g.bucket(x, y)] {
				i := en.slot
				e := entityOf(i)
				if int(en.x) != x || int(en.y) != y || e == exclude {
					continue
				}
				if h, ok := sweep(&ry, g.shapes[i], g.at[i]); ok {
					h.Entity = e
					dst = append(dst, h)
				}
			}
		}
		if w.exit() > 1 {
			return sortDedup(dst, start)
		}
		w.advance()
	}
}

func (g *HashGrid) Blocked(from, to m.Vec2, radius float32, exclude ecs.Entity) bool {
	ry := makeRay(from, to, radius)
	if q := g.sweepRect(&ry); q.cells() <= g.RectLimit {
		for y := q.y0; y <= q.y1; y++ {
			for x := q.x0; x <= q.x1; x++ {
				for _, en := range g.buckets[g.bucket(x, y)] {
					i := en.slot
					if int(en.x) != x || int(en.y) != y || entityOf(i) == exclude || !g.home(i, x, y, q) {
						continue
					}
					if _, ok := sweep(&ry, g.shapes[i], g.at[i]); ok {
						return true
					}
				}
			}
		}
		return false
	}
	w := g.walker(&ry)
	for {
		for x, y, ok := w.next(); ok; x, y, ok = w.next() {
			for _, en := range g.buckets[g.bucket(x, y)] {
				i := en.slot
				if int(en.x) != x || int(en.y) != y || entityOf(i) == exclude {
					continue
				}
				if _, ok := sweep(&ry, g.shapes[i], g.at[i]); ok {
					return true
				}
			}
		}
		if w.exit() > 1 {
			return false
		}
		w.advance()
	}
}

func (g *HashGrid) Overlap(dst []ecs.Entity, at m.Vec2, radius float32, exclude ecs.Entity) []ecs.Entity {
	q := rect{g.coord(at.X - radius), g.coord(at.X + radius), g.coord(at.Y - radius), g.coord(at.Y + radius)}
	for y := q.y0; y <= q.y1; y++ {
		for x := q.x0; x <= q.x1; x++ {
			for _, en := range g.buckets[g.bucket(x, y)] {
				i := en.slot
				if e := entityOf(i); int(en.x) == x && int(en.y) == y && e != exclude && g.home(i, x, y, q) && overlapCircle(at, radius, g.shapes[i], g.at[i]) {
					dst = append(dst, e)
				}
			}
		}
	}
	return dst
}
