package queryindex

import (
	"fmt"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/libs/m"
)

// BVH is a bounding-volume hierarchy of axis-aligned boxes over slots, built
// top-down by a median split on the longer axis of the centroids. Nodes are one
// flat slice; a node's children follow it. Queries descend nearer child first
// on a fixed stack, testing node boxes grown by the sweep's radius.
//
// Bodies move every sub-step, so Refit recomputes node boxes bottom-up without
// changing the tree; Build rebuilds it.
type BVH struct {
	store
	LeafSize int
	nodes    []bvhNode
	order    []int32 // slots, grouped by leaf
}

type bvhNode struct {
	box          aabb
	right        int32 // index of the right child; the left child is this node + 1
	start, count int32 // leaf: order[start:start+count]; count 0 means inner
}

func NewBVH(leaf int) *BVH { return &BVH{LeafSize: leaf} }

func (b *BVH) Name() string { return fmt.Sprintf("bvh%d", b.LeafSize) }

func (b *BVH) Build(items []Placed) {
	b.load(items)
	b.order = b.order[:0]
	for i := range b.box {
		b.order = append(b.order, int32(i))
	}
	b.nodes = b.nodes[:0]
	if len(b.order) > 0 {
		b.build(0, int32(len(b.order)))
	}
}

func (b *BVH) build(start, end int32) int32 {
	idx := int32(len(b.nodes))
	b.nodes = append(b.nodes, bvhNode{})
	box := b.box[b.order[start]]
	cb := centre(box)
	cmin, cmax := cb, cb
	for _, s := range b.order[start+1 : end] {
		box = union(box, b.box[s])
		c := centre(b.box[s])
		cmin = m.Vec2{X: min32(cmin.X, c.X), Y: min32(cmin.Y, c.Y)}
		cmax = m.Vec2{X: max32(cmax.X, c.X), Y: max32(cmax.Y, c.Y)}
	}
	if end-start <= int32(b.LeafSize) {
		b.nodes[idx] = bvhNode{box: box, start: start, count: end - start}
		return idx
	}
	axisX := cmax.X-cmin.X >= cmax.Y-cmin.Y
	mid := (start + end) / 2
	b.selectNth(start, end, mid, axisX)
	b.build(start, mid)
	right := b.build(mid, end)
	b.nodes[idx] = bvhNode{box: box, right: right}
	return idx
}

// selectNth partitions order[start:end] so order[nth] has the nth centroid on
// the axis (quickselect, no allocation).
func (b *BVH) selectNth(start, end, nth int32, axisX bool) {
	key := func(s int32) float32 {
		c := b.box[s]
		if axisX {
			return c.minX + c.maxX
		}
		return c.minY + c.maxY
	}
	lo, hi := start, end-1
	for lo < hi {
		pivot := key(b.order[(lo+hi)/2])
		i, j := lo, hi
		for i <= j {
			for key(b.order[i]) < pivot {
				i++
			}
			for key(b.order[j]) > pivot {
				j--
			}
			if i <= j {
				b.order[i], b.order[j] = b.order[j], b.order[i]
				i++
				j--
			}
		}
		if nth <= j {
			hi = j
		} else if nth >= i {
			lo = i
		} else {
			return
		}
	}
}

// Move updates the slots' positions in place, for Refit.
func (b *BVH) Move(items []Placed) {
	for i, it := range items {
		b.at[i] = it.At
		b.box[i] = boundsOf(it)
	}
}

// Refit recomputes every node box bottom-up. Children follow parents, so a
// reverse walk sees children first.
func (b *BVH) Refit() {
	for n := len(b.nodes) - 1; n >= 0; n-- {
		nd := &b.nodes[n]
		if nd.count > 0 {
			box := b.box[b.order[nd.start]]
			for _, s := range b.order[nd.start+1 : nd.start+nd.count] {
				box = union(box, b.box[s])
			}
			nd.box = box
		} else {
			nd.box = union(b.nodes[n+1].box, b.nodes[nd.right].box)
		}
	}
}

// slab returns the entry parameter of the ray into box grown by r, or false.
func slab(ry *ray, bx aabb) (float32, bool) {
	r := ry.r + slabEps
	t0, t1 := float32(0), float32(1)
	if ry.d.X != 0 {
		a, c := (bx.minX-r-ry.p0.X)*ry.inv.X, (bx.maxX+r-ry.p0.X)*ry.inv.X
		if a > c {
			a, c = c, a
		}
		t0, t1 = max32(t0, a), min32(t1, c)
	} else if ry.p0.X < bx.minX-r || ry.p0.X > bx.maxX+r {
		return 0, false
	}
	if ry.d.Y != 0 {
		a, c := (bx.minY-r-ry.p0.Y)*ry.inv.Y, (bx.maxY+r-ry.p0.Y)*ry.inv.Y
		if a > c {
			a, c = c, a
		}
		t0, t1 = max32(t0, a), min32(t1, c)
	} else if ry.p0.Y < bx.minY-r || ry.p0.Y > bx.maxY+r {
		return 0, false
	}
	return t0, t0 <= t1
}

const bvhStack = 64

// slabEps grows every node box by a millimetre so a culling test in float32
// never rejects a shape whose own exact test would hit on the boundary.
const slabEps = 1e-3

func (b *BVH) Sweep(from, to m.Vec2, radius float32, exclude ecs.Entity) (Hit, bool) {
	best, found := Hit{T: 2}, false
	if len(b.nodes) == 0 {
		return best, false
	}
	ry := makeRay(from, to, radius)
	var stack [bvhStack]int32
	sp := 0
	n := int32(0)
	if _, ok := slab(&ry, b.nodes[0].box); !ok {
		return best, false
	}
	for {
		nd := &b.nodes[n]
		if nd.count > 0 {
			for _, s := range b.order[nd.start : nd.start+nd.count] {
				e := entityOf(s)
				if e == exclude {
					continue
				}
				if h, ok := sweep(&ry, b.shapes[s], b.at[s]); ok && (h.T < best.T || (h.T == best.T && e < best.Entity)) {
					h.Entity = e
					best, found = h, true
				}
			}
		} else {
			l, r := n+1, nd.right
			tl, okl := slab(&ry, b.nodes[l].box)
			tr, okr := slab(&ry, b.nodes[r].box)
			okl, okr = okl && tl <= best.T, okr && tr <= best.T
			switch {
			case okl && okr:
				if tr < tl {
					l, r = r, l
				}
				stack[sp] = r
				sp++
				n = l
				continue
			case okl:
				n = l
				continue
			case okr:
				n = r
				continue
			}
		}
		for {
			if sp == 0 {
				return best, found
			}
			sp--
			n = stack[sp]
			// re-check against the best found since it was pushed
			if t, ok := slab(&ry, b.nodes[n].box); ok && t <= best.T {
				break
			}
		}
	}
}

func (b *BVH) SweepAll(dst []Hit, from, to m.Vec2, radius float32, exclude ecs.Entity) []Hit {
	start := len(dst)
	if len(b.nodes) == 0 {
		return dst
	}
	ry := makeRay(from, to, radius)
	var stack [bvhStack]int32
	stack[0] = 0
	sp := 1
	for sp > 0 {
		sp--
		n := stack[sp]
		nd := &b.nodes[n]
		if _, ok := slab(&ry, nd.box); !ok {
			continue
		}
		if nd.count == 0 {
			stack[sp] = nd.right
			stack[sp+1] = n + 1
			sp += 2
			continue
		}
		for _, s := range b.order[nd.start : nd.start+nd.count] {
			e := entityOf(s)
			if e == exclude {
				continue
			}
			if h, ok := sweep(&ry, b.shapes[s], b.at[s]); ok {
				h.Entity = e
				dst = insertSorted(dst, start, h)
			}
		}
	}
	return dst
}

func (b *BVH) Blocked(from, to m.Vec2, radius float32, exclude ecs.Entity) bool {
	if len(b.nodes) == 0 {
		return false
	}
	ry := makeRay(from, to, radius)
	var stack [bvhStack]int32
	sp := 1
	for sp > 0 {
		sp--
		n := stack[sp]
		nd := &b.nodes[n]
		if _, ok := slab(&ry, nd.box); !ok {
			continue
		}
		if nd.count == 0 {
			stack[sp] = nd.right
			stack[sp+1] = n + 1
			sp += 2
			continue
		}
		for _, s := range b.order[nd.start : nd.start+nd.count] {
			if entityOf(s) == exclude {
				continue
			}
			if _, ok := sweep(&ry, b.shapes[s], b.at[s]); ok {
				return true
			}
		}
	}
	return false
}

func (b *BVH) Overlap(dst []ecs.Entity, at m.Vec2, radius float32, exclude ecs.Entity) []ecs.Entity {
	if len(b.nodes) == 0 {
		return dst
	}
	q := aabb{at.X - radius, at.Y - radius, at.X + radius, at.Y + radius}
	var stack [bvhStack]int32
	sp := 1
	for sp > 0 {
		sp--
		n := stack[sp]
		nd := &b.nodes[n]
		if nd.box.minX > q.maxX || nd.box.maxX < q.minX || nd.box.minY > q.maxY || nd.box.maxY < q.minY {
			continue
		}
		if nd.count == 0 {
			stack[sp] = nd.right
			stack[sp+1] = n + 1
			sp += 2
			continue
		}
		for _, s := range b.order[nd.start : nd.start+nd.count] {
			if e := entityOf(s); e != exclude && overlapCircle(at, radius, b.shapes[s], b.at[s]) {
				dst = append(dst, e)
			}
		}
	}
	return dst
}

func centre(b aabb) m.Vec2 { return m.Vec2{X: (b.minX + b.maxX) / 2, Y: (b.minY + b.maxY) / 2} }

func union(a, b aabb) aabb {
	return aabb{min32(a.minX, b.minX), min32(a.minY, b.minY), max32(a.maxX, b.maxX), max32(a.maxY, b.maxY)}
}
