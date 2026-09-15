package queryindex

import (
	"fmt"
	"math"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/libs/m"
)

// Grid is a uniform grid of cells, each holding the slots whose bounds overlap
// it. A shape larger than a cell is listed in every cell it covers. Cell size
// is the index's own parameter.
//
// Short sweeps scan the rectangle of cells their grown bounds cover, testing a
// shape only in the first cell of the rectangle it appears in, so nothing is
// tested twice and no per-query state is kept. Long sweeps walk the cells
// along the line in order (Amanatides & Woo), visiting a band of
// ceil(radius/cell) cells either side, and stop once the best hit lies before
// the cell being left; a shape spanning several cells on that walk is re-tested.
type Grid struct {
	store
	cell, inv float32
	origin    float32
	n         int
	cells     [][]int32
	first     []cellXY // the lowest cell each slot is listed in
	touched   []int32
	listed    []bool // cell is in touched
	RectLimit int    // sweeps whose rectangle covers at most this many cells scan it
}

type cellXY struct{ x, y int32 }

const gridMargin = 32

func NewGrid(cell float32) *Grid {
	n := int(math.Ceil(float64((MapSize + 2*gridMargin) / cell)))
	return &Grid{cell: cell, inv: 1 / cell, origin: -gridMargin, n: n, cells: make([][]int32, n*n), listed: make([]bool, n*n), RectLimit: 16}
}

func (g *Grid) Name() string { return fmt.Sprintf("grid%gm", g.cell) }

func (g *Grid) coord(v float32) int {
	f := (v - g.origin) * g.inv
	if f < 0 { // also keeps truncation equal to floor below
		return 0
	}
	c := int(f)
	if c >= g.n {
		return g.n - 1
	}
	return c
}

// Build clears only the cells the last build touched, so a rebuild costs the
// shapes' cell coverage and not the grid's area.
func (g *Grid) Build(items []Placed) {
	for _, ci := range g.touched {
		g.cells[ci] = g.cells[ci][:0]
		g.listed[ci] = false
	}
	g.touched = g.touched[:0]
	g.load(items)
	g.first = g.first[:0]
	for i := range g.box {
		g.first = append(g.first, cellXY{})
		g.insertCells(int32(i))
	}
}

func (g *Grid) insertCells(i int32) {
	b := g.box[i]
	x0, x1, y0, y1 := g.coord(b.minX), g.coord(b.maxX), g.coord(b.minY), g.coord(b.maxY)
	g.first[i] = cellXY{int32(x0), int32(y0)}
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			ci := y*g.n + x
			if !g.listed[ci] {
				g.listed[ci] = true
				g.touched = append(g.touched, int32(ci))
			}
			g.cells[ci] = append(g.cells[ci], i)
		}
	}
}

// Replace swaps slot i for a new shape in place: what a hook drain does when
// the app replaces one static Entity with another (a door opening).
func (g *Grid) Replace(i int32, p Placed) {
	b := g.box[i]
	x0, x1, y0, y1 := g.coord(b.minX), g.coord(b.maxX), g.coord(b.minY), g.coord(b.maxY)
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			c := g.cells[y*g.n+x]
			for k, s := range c {
				if s == i {
					c[k] = c[len(c)-1]
					g.cells[y*g.n+x] = c[:len(c)-1]
					break
				}
			}
		}
	}
	g.shapes[i], g.at[i], g.box[i] = p.Shape, p.At, boundsOf(p)
	g.insertCells(i)
}

type rect struct{ x0, x1, y0, y1 int }

func (q rect) cells() int { return (q.x1 - q.x0 + 1) * (q.y1 - q.y0 + 1) }

func (g *Grid) sweepRect(ry *ray) rect {
	minX, maxX := min32(ry.p0.X, ry.p0.X+ry.d.X)-ry.r, max32(ry.p0.X, ry.p0.X+ry.d.X)+ry.r
	minY, maxY := min32(ry.p0.Y, ry.p0.Y+ry.d.Y)-ry.r, max32(ry.p0.Y, ry.p0.Y+ry.d.Y)+ry.r
	return rect{g.coord(minX), g.coord(maxX), g.coord(minY), g.coord(maxY)}
}

// home reports whether cell (x, y) is the first cell of rectangle q that slot
// i appears in, so a rectangle scan tests each shape once.
func (g *Grid) home(i int32, x, y int, q rect) bool {
	f := g.first[i]
	return x == max(int(f.x), q.x0) && y == max(int(f.y), q.y0)
}

// walker yields the cells within ceil(r/cell) of a line, in order along it,
// in batches: the starting block, then the leading row or column each step
// adds. It lives on the caller's stack.
type walker struct {
	g            *Grid
	ry           *ray
	k, cx, cy    int
	sx, sy       int
	batch        rect
	bx, by       int
	stepX, stepY bool // which axis the pending step moves
}

func (g *Grid) walker(ry *ray) walker {
	w := walker{g: g, ry: ry, k: int(ceil32(ry.r * g.inv)), cx: g.coord(ry.p0.X), cy: g.coord(ry.p0.Y), sx: 1, sy: 1}
	if ry.d.X < 0 {
		w.sx = -1
	}
	if ry.d.Y < 0 {
		w.sy = -1
	}
	w.setBatch(rect{w.cx - w.k, w.cx + w.k, w.cy - w.k, w.cy + w.k})
	return w
}

func (w *walker) setBatch(q rect) {
	n := w.g.n - 1
	q.x0, q.y0 = max(q.x0, 0), max(q.y0, 0)
	q.x1, q.y1 = min(q.x1, n), min(q.y1, n)
	w.batch, w.bx, w.by = q, q.x0, q.y0
}

// next yields the next cell of the current batch.
func (w *walker) next() (int, bool) {
	q := &w.batch
	if w.by > q.y1 || q.x0 > q.x1 {
		return 0, false
	}
	ci := w.by*w.g.n + w.bx
	w.bx++
	if w.bx > q.x1 {
		w.bx = q.x0
		w.by++
	}
	return ci, true
}

// exit is the parameter at which the line leaves the current centre cell.
func (w *walker) exit() float32 {
	g, ry := w.g, w.ry
	tx, ty := float32(math.Inf(1)), float32(math.Inf(1))
	if ry.d.X != 0 {
		bx := w.cx
		if w.sx > 0 {
			bx++
		}
		tx = (float32(bx)*g.cell + g.origin - ry.p0.X) * ry.inv.X
	}
	if ry.d.Y != 0 {
		by := w.cy
		if w.sy > 0 {
			by++
		}
		ty = (float32(by)*g.cell + g.origin - ry.p0.Y) * ry.inv.Y
	}
	w.stepX = tx < ty
	return min32(tx, ty)
}

// advance steps the centre cell across the boundary exit found.
func (w *walker) advance() {
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

func (g *Grid) Sweep(from, to m.Vec2, radius float32, exclude ecs.Entity) (Hit, bool) {
	ry := makeRay(from, to, radius)
	best, found := Hit{T: 2}, false
	if q := g.sweepRect(&ry); q.cells() <= g.RectLimit {
		for y := q.y0; y <= q.y1; y++ {
			for x := q.x0; x <= q.x1; x++ {
				for _, i := range g.cells[y*g.n+x] {
					e := entityOf(i)
					if e == exclude || !g.home(i, x, y, q) {
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
	// a millimetre of parameter, so float32 error at a cell boundary cannot
	// stop the walk before an equal-or-earlier hit in the next cell
	margin := 1e-3 * ry.inv.X
	if abs32(ry.d.Y) > abs32(ry.d.X) {
		margin = 1e-3 * ry.inv.Y
	}
	margin = abs32(margin)
	w := g.walker(&ry)
	for {
		for ci, ok := w.next(); ok; ci, ok = w.next() {
			for _, i := range g.cells[ci] {
				e := entityOf(i)
				if e == exclude {
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

func (g *Grid) SweepAll(dst []Hit, from, to m.Vec2, radius float32, exclude ecs.Entity) []Hit {
	ry := makeRay(from, to, radius)
	start := len(dst)
	if q := g.sweepRect(&ry); q.cells() <= g.RectLimit {
		for y := q.y0; y <= q.y1; y++ {
			for x := q.x0; x <= q.x1; x++ {
				for _, i := range g.cells[y*g.n+x] {
					e := entityOf(i)
					if e == exclude || !g.home(i, x, y, q) {
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
		for ci, ok := w.next(); ok; ci, ok = w.next() {
			for _, i := range g.cells[ci] {
				e := entityOf(i)
				if e == exclude {
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

func (g *Grid) Blocked(from, to m.Vec2, radius float32, exclude ecs.Entity) bool {
	ry := makeRay(from, to, radius)
	if q := g.sweepRect(&ry); q.cells() <= g.RectLimit {
		for y := q.y0; y <= q.y1; y++ {
			for x := q.x0; x <= q.x1; x++ {
				for _, i := range g.cells[y*g.n+x] {
					if entityOf(i) == exclude || !g.home(i, x, y, q) {
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
		for ci, ok := w.next(); ok; ci, ok = w.next() {
			for _, i := range g.cells[ci] {
				if entityOf(i) == exclude {
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

func (g *Grid) Overlap(dst []ecs.Entity, at m.Vec2, radius float32, exclude ecs.Entity) []ecs.Entity {
	q := rect{g.coord(at.X - radius), g.coord(at.X + radius), g.coord(at.Y - radius), g.coord(at.Y + radius)}
	for y := q.y0; y <= q.y1; y++ {
		for x := q.x0; x <= q.x1; x++ {
			for _, i := range g.cells[y*g.n+x] {
				if e := entityOf(i); e != exclude && g.home(i, x, y, q) && overlapCircle(at, radius, g.shapes[i], g.at[i]) {
					dst = append(dst, e)
				}
			}
		}
	}
	return dst
}

func floor32(v float32) float32 { return float32(math.Floor(float64(v))) }
func ceil32(v float32) float32  { return float32(math.Ceil(float64(v))) }

// Update moves the same slots to new positions, touching cell lists only for
// a slot whose covered cell range changed. A body moving 0.11 m a sub-step
// mostly stays in its cells, so this costs a bounds check per body.
func (g *Grid) Update(items []Placed) {
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
