package types

import (
	"math"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/libs/m"
)

// defaultCellSize is the documented default for both indices, tuned for a
// metre-scaled world: dense crowds are cheapest around 1 m and long merged
// walls around 4 to 8 m, and 2 m is best or close to it for every frequent
// query on the reference workload. It is a default, not a content assumption,
// and it is what a non-positive cell size falls back to.
const defaultCellSize = 2.0

// defaultBuckets is where a fresh index's hash table starts. It doubles
// whenever the listings outgrow it.
const defaultBuckets = 256

// StaticIndex holds the Entities carrying the Static Tag: geometry that never
// changes in place, world-cached once when it is inserted and never again.
//
// It is a kernel Resource of its own type, apart from BodyIndex, so that
// rebuilding the Bodies write-locks only the Bodies and a line-of-sight query
// never waits for it. Which index a query asks is the caller's choice.
//
// Its queries are reads and hold no per-query state, so any number of them run
// together.
type StaticIndex struct {
	index
	// slots is the Entity to slot table. The static index is kept current
	// incrementally — a Shape added is inserted and a Shape removed is taken
	// out — so it genuinely has to find an existing entry by its Entity, which
	// is the one job a table keyed by Entity does and a position cannot.
	slots map[ecs.Entity]int32
	// removals counts the Shapes that have left the index, by Remove, by an
	// Insert replacing one, or by Clear. The sleep System compares it against
	// the count it last saw, so a tick on which no Static left pays one
	// comparison for the rule that removing a support wakes what rests on it.
	removals uint64
}

// BodyIndex holds every other Entity with a Shape, Kinematic and Dynamic alike,
// and is rebuilt from the Bodies' positions each tick. Shapeless Bodies are in
// neither index.
//
// It keeps no Entity to slot table. It is Cleared and refilled from scratch
// every tick, so the slot a Shape takes is the walk's own position — the Nth
// Shape inserted is in slot N — and nothing on the rebuild ever has to find an
// entry by its Entity. The Go map it once kept for that cost more than the
// whole rebuild had been argued at.
//
// Sleeping bodies are kept in a second grid of its own, which is not rebuilt
// every tick: Index moves a Body into it when its Island falls asleep and out
// of it when the Island wakes. Detect tests awake Bodies against it and never
// tests it against itself or against the static index, and every query here
// asks both grids, so what a query answers does not depend on what sleeps. The
// static index never holds a sleeper. cp moves a sleeping Body's Shapes into
// its static tree instead; the port's two indices are public and the caller
// chooses which to ask, so a Probe on the Body index would then miss a
// sleeping crate and one on the static index would find it.
type BodyIndex struct {
	index
	// held is how many live Shapes the entries run holds, which is its length
	// less whatever Remove has taken out since the last Clear.
	held int
	// sleepers is the Sleeping bodies' grid. Its slots are stable while a Body
	// sleeps and recycled when one wakes; the solver numbers them after the
	// awake grid's, so a slot here is len(entries) plus its own.
	sleepers index
	// sleeping is how many live Shapes the sleepers' grid holds.
	sleeping int
	// woken is the set of Entities the drain is taking out of the sleepers'
	// grid this tick, so that one pass over that grid takes out any number of
	// them. It is scratch, written only under the index's own write.
	woken entityTable
}

// NewStaticIndex is an empty static index with that cell size in metres. A cell
// size of 0 or less takes the documented default of 2 m.
func NewStaticIndex(cellSize float64) *StaticIndex {
	idx := &StaticIndex{slots: make(map[ecs.Entity]int32)}
	idx.init(cellSize)
	return idx
}

// NewBodyIndex is an empty Body index with that cell size in metres. A cell
// size of 0 or less takes the documented default of 2 m.
func NewBodyIndex(cellSize float64) *BodyIndex {
	idx := &BodyIndex{}
	idx.init(cellSize)
	idx.sleepers.init(cellSize)
	return idx
}

// entry is one Shape in an index: the Entity a query hands back, the Shape
// itself, the bounding box the grid keys on, and the world cache beside it —
// the transform, and the run of the index's world slab holding the world-space
// geometry the closed forms read.
//
// The cache is here rather than in a Component because adding or removing a
// Component is a structural change — write on its own Store, every tick a Body
// comes or goes — and because mirroring derived data doubles the memory. Being
// internal it is free of the Shape's inline vertex cap.
type entry struct {
	entity                    ecs.Entity
	shape                     Shape
	box                       BB
	transform                 Transform
	world, worldLen, worldCap int32
	left, bottom              int32
	right, top                int32
	live                      bool
	// path is the one path bit: Detect tests this entry along its path
	// through the tick instead of only where the tick left it. InsertMoving
	// sets it on a moving circle Sensor, and on a solid Body whose movement
	// passes continuous collision's gate, and the Sensor flag beside it picks
	// what Detect's path pass keeps. Deciding it once at insert is what keeps
	// the detection walk's own test a field read.
	path bool
	// previousCentre is where the path starts; the end is where the tick left
	// the entry. For a circle it is the centre, and for any other Shape the
	// Position. A Sensor's circle starts at its centre at the previous pose; a
	// solid Body's path is held at its end angle, so it starts at its end pose
	// translated by Previous − Current. InsertMoving writes it, and it means
	// nothing unless path is set.
	previousCentre m.Vec2d
}

// link is one listing of an entry in one hash bucket, in a flat arena rather
// than cp's pooled linked bins behind a sync.Pool: the leaf holds a slot, and
// the slot holds a caller-owned ecs.Entity, which is what makes an index legal
// beside an ECS.
type link struct{ entry, next int32 }

// probing is one Probe's state on the stack while the walk runs. It is not
// state an index holds: a Resource concurrent readers share can hold none, and
// that is why ordering needs the caller's own slice.
type probing struct {
	dst          []Hit
	first        bool
	start        int
	from, to     m.Vec2d
	radius       float64
	bits         uint32
	collidesWith uint32
	exclude      ecs.Entity
	// box is the whole Probe's own bounding box, grown by its radius: the first
	// and cheapest rejection of a candidate that merely shares a cell.
	box   BB
	best  Hit
	found bool
	exit  float64
	// band is set while the walk is scanning cells beside the ray rather than
	// the ray's own, which only a Probe with a radius does.
	band bool
	// slots, when it is not nil, is the caller's run of entry slots kept
	// beside dst, one a Hit in the same order, which is how a sweep over the
	// Body index learns the slot behind each Hit without a table to ask. It is
	// a pointer so that every other Probe pays one word for it and no more.
	slots *[]int32
	// slotBase is added to every slot kept in slots: 0 for the Body index's
	// own grid, and the length of that grid for its sleepers', whose slots the
	// solver numbers after the awake ones.
	slotBase int32
}

// index is the hashed uniform grid behind both Resources. The structure is not
// part of the contract — no signature names a cell — so a BVH can replace it
// later. It replaces cp's BBTree, which never rebalances, has no Optimize and
// whose Reindex panics.
//
// Cells are hashed, so there is no world extent and memory follows the Shapes
// rather than the world. A Shape is listed in every cell its box covers, and a
// cell holds a slot rather than a copy, so a scan tests each Shape once however
// many cells it spans.
type index struct {
	cellSize    float64
	inverseCell float64

	entries     []entry
	freeEntries []int32

	buckets   []int32
	links     []link
	freeLinks []int32
	listings  int

	// slab is the world cache's vectors for every entry, which is what keeps
	// the cache free of the Shape's inline vertex cap.
	slab []m.Vec2d
}

func (idx *index) init(cellSize float64) {
	if !(cellSize > 0) {
		cellSize = defaultCellSize
	}
	idx.cellSize = cellSize
	idx.inverseCell = 1 / cellSize
	idx.buckets = make([]int32, defaultBuckets)
	idx.clearBuckets()
}

// CellSize is the grid's cell size in metres.
func (idx *index) CellSize() float64 { return idx.cellSize }

// Len is how many Shapes the index holds.
func (idx *StaticIndex) Len() int { return len(idx.slots) }

// Len is how many Shapes the index holds, sleeping ones included.
func (idx *BodyIndex) Len() int { return idx.held + idx.sleeping }

// Insert puts an Entity's Shape into the index, placed at a position and an
// angle, replacing whatever the index held for that Entity. The world cache is
// built here and never again, which is what "a static is cached once, at
// insert" means.
//
// verts is the Polygon Component's vertices and is nil for every kind but Poly.
func (idx *StaticIndex) Insert(entity ecs.Entity, shape Shape, at m.Vec2d, angle float64, verts []m.Vec2d) {
	idx.insert(entity, shape, at, angle, verts)
}

// insert is Insert, answering the slot it filled, or −1 for NoEntity.
func (idx *StaticIndex) insert(entity ecs.Entity, shape Shape, at m.Vec2d, angle float64, verts []m.Vec2d) int32 {
	if entity == ecs.NoEntity {
		return -1
	}
	idx.Remove(entity)
	slot := idx.allocEntry()
	idx.place(slot, entity, shape, at, angle, verts)
	idx.slots[entity] = slot
	return slot
}

// Insert puts an Entity's Shape into the index, placed at a position and an
// angle, in the next slot: the Nth Shape inserted since the last Clear is in
// slot N. The index is Cleared and refilled each tick, which is how it is kept
// current, so it never asks whether it already holds the Entity — an Entity
// inserted twice between two Clears is held twice, and the rebuild inserts
// each once.
//
// verts is the Polygon Component's vertices and is nil for every kind but Poly.
func (idx *BodyIndex) Insert(entity ecs.Entity, shape Shape, at m.Vec2d, angle float64, verts []m.Vec2d) {
	idx.insert(entity, shape, at, angle, verts)
}

// insert is Insert, answering the slot it filled, or −1 for NoEntity.
func (idx *BodyIndex) insert(entity ecs.Entity, shape Shape, at m.Vec2d, angle float64, verts []m.Vec2d) int32 {
	if entity == ecs.NoEntity {
		return -1
	}
	slot := int32(len(idx.entries))
	idx.entries = append(idx.entries, entry{})
	idx.place(slot, entity, shape, at, angle, verts)
	idx.held++
	return slot
}

// place fills one entry slot with an Entity's Shape, builds its world cache and
// lists it in every cell its box covers. Which slot it is, and how the index
// finds it again, is each index's own.
func (idx *index) place(slot int32, entity ecs.Entity, shape Shape, at m.Vec2d, angle float64, verts []m.Vec2d) {
	needed := int32(worldLenFor(shape, verts))
	if needed > idx.entries[slot].worldCap {
		// The abandoned run is reclaimed by the next Clear, which resets the
		// slab without giving its capacity back.
		idx.entries[slot].world = int32(len(idx.slab))
		idx.entries[slot].worldCap = needed
		for range needed {
			idx.slab = append(idx.slab, m.Vec2d{})
		}
	}

	e := &idx.entries[slot]
	e.entity = entity
	e.shape = shape
	e.transform = NewTransformRigid(at, angle)
	// A Shape inserted without a path did not move: InsertMoving is the one
	// thing that marks one, and a recycled slot must not inherit the last
	// Shape's answer.
	e.live, e.path = true, false

	used, box := cacheWorldAt(shape, e.transform, verts, idx.slab[e.world:e.world+e.worldCap])
	e.worldLen = int32(used)
	e.box = box

	idx.list(slot)
}

// Remove takes an Entity's Shape out of the index and does nothing when the
// index does not hold it.
func (idx *StaticIndex) Remove(entity ecs.Entity) {
	slot, ok := idx.slots[entity]
	if !ok {
		return
	}
	idx.drop(slot)
	delete(idx.slots, entity)
	idx.freeEntries = append(idx.freeEntries, slot)
	idx.removals++
}

// Remove takes an Entity's Shape out of the index and does nothing when the
// index does not hold it.
//
// Having no Entity to slot table, it walks the entries to find the Entity,
// which costs in proportion to the index's size; the plugin never calls it,
// keeping the index current by the Clear and refill instead. The slot it
// empties stays empty until the next Clear, so every other Shape keeps the slot
// the walk gave it.
func (idx *BodyIndex) Remove(entity ecs.Entity) {
	if entity == ecs.NoEntity {
		return
	}
	for slot := range idx.entries {
		if idx.entries[slot].live && idx.entries[slot].entity == entity {
			idx.drop(int32(slot))
			idx.held--
		}
	}
	for slot := range idx.sleepers.entries {
		if idx.sleepers.entries[slot].live && idx.sleepers.entries[slot].entity == entity {
			idx.dropSleeper(int32(slot))
		}
	}
}

// drop takes one slot's entry out of every cell and marks it dead.
func (idx *index) drop(slot int32) {
	idx.unlist(slot)
	idx.entries[slot].live = false
	idx.entries[slot].entity = ecs.NoEntity
}

// Clear empties the index, keeping every buffer it has grown so that refilling
// it allocates nothing.
func (idx *StaticIndex) Clear() {
	if len(idx.slots) > 0 {
		idx.removals++
	}
	idx.index.clear()
	clear(idx.slots)
}

// Clear empties the index, keeping every buffer it has grown so that refilling
// it allocates nothing. It is how the index is rebuilt each tick.
//
// It empties the grid the rebuild refills and leaves the Sleeping bodies' grid
// alone, which is kept current as Islands fall asleep and wake rather than
// rebuilt.
func (idx *BodyIndex) Clear() {
	idx.index.clear()
	idx.held = 0
}

func (idx *index) clear() {
	idx.entries = idx.entries[:0]
	idx.freeEntries = idx.freeEntries[:0]
	idx.links = idx.links[:0]
	idx.freeLinks = idx.freeLinks[:0]
	idx.slab = idx.slab[:0]
	idx.listings = 0
	idx.clearBuckets()
}

// Probe moves a circle of that radius from one point to another and reports the
// nearest Hit, or false when it meets nothing. Line of sight is that bool.
//
// bits and collidesWith are the groups the Prober is in and the groups it looks
// for, and both sides must agree; exclude is the one Entity the Probe ignores,
// which is what a Body Probing from inside its own Shape needs. A Probe that
// starts overlapping reports T = 0, and a zero-length Probe is legal.
func (idx *index) Probe(
	from, to m.Vec2d, radius float64,
	bits, collidesWith uint32, exclude ecs.Entity,
) (Hit, bool) {
	_, hit, ok := idx.probeWalk(nil, 0, nil, 0, true, from, to, radius, bits, collidesWith, exclude, nil)
	return hit, ok
}

// ProbeAll appends every Hit along the Probe to dst, ordered by T, and returns
// it. The caller owns the slice, which is the scratch an ordering needs and the
// reason there is no iterator: a Resource concurrent readers share can hold no
// per-query state.
//
// The idiom is dst = idx.ProbeAll(dst[:0], …), which settles to no allocation
// once the buffer is big enough.
func (idx *index) ProbeAll(
	dst []Hit, from, to m.Vec2d, radius float64,
	bits, collidesWith uint32, exclude ecs.Entity,
) []Hit {
	dst, _, _ = idx.probeWalk(dst, len(dst), nil, 0, false, from, to, radius, bits, collidesWith, exclude, nil)
	return dst
}

// ProbeWith moves a Shape at a fixed angle from one Position to another and
// reports the nearest Hit, or false when it meets nothing: Probe for a Shape
// that is not a circle, and whether a crate fits through a gap is its bool.
//
// from and to are the Shape's Position at each end, and angle is held the whole
// way, so the Shape does not turn. A circle Shape is Probe of its own radius
// about its placed centre. bits, collidesWith and exclude are Probe's, and the
// Hit means what Probe's does: T a fraction of from towards to, Point on the
// hit Shape's surface, Normal facing the mover, T = 0 for a Shape that starts
// overlapping, and a zero-length Probe legal.
//
// verts is the Polygon Component's vertices and is nil for every kind but Poly.
func (idx *index) ProbeWith(
	shape Shape, from, to m.Vec2d, angle float64, verts []m.Vec2d,
	bits, collidesWith uint32, exclude ecs.Entity,
) (Hit, bool) {
	if shape.Kind == ShapeCircle {
		from, to := circlePath(shape, from, to, angle)
		return idx.Probe(from, to, shape.Radius, bits, collidesWith, exclude)
	}
	var runs moverRuns
	mover, ok := newShapeProbe(&runs, shape, from, to, angle, verts)
	if !ok {
		return Hit{}, false
	}
	_, hit, ok := idx.shapeWalk(nil, 0, true, &mover, bits, collidesWith, exclude)
	return hit, ok
}

// ProbeAllWith appends every Hit of a Shape moved as ProbeWith moves it to
// dst, ordered by T, and returns it, as ProbeAll does for a circle.
func (idx *index) ProbeAllWith(
	dst []Hit, shape Shape, from, to m.Vec2d, angle float64, verts []m.Vec2d,
	bits, collidesWith uint32, exclude ecs.Entity,
) []Hit {
	if shape.Kind == ShapeCircle {
		from, to := circlePath(shape, from, to, angle)
		return idx.ProbeAll(dst, from, to, shape.Radius, bits, collidesWith, exclude)
	}
	var runs moverRuns
	mover, ok := newShapeProbe(&runs, shape, from, to, angle, verts)
	if !ok {
		return dst
	}
	dst, _, _ = idx.shapeWalk(dst, len(dst), false, &mover, bits, collidesWith, exclude)
	return dst
}

// shapeWalk is probeWalk for a moving Shape. The grid is walked along the path
// of the centre of the mover's box, with the band beside it as wide as that
// box's half-diagonal, which reaches everything the box does wherever it is
// along its path: so a Shape is found in the same cells, and the nearest Probe
// stops as early, as a circle about the box would be. The leaf rejection is
// against the box the mover sweeps, which is tighter than that circle's.
func (idx *index) shapeWalk(
	dst []Hit, start int, first bool, mover *shapeProbe,
	bits, collidesWith uint32, exclude ecs.Entity,
) ([]Hit, Hit, bool) {
	box := mover.box
	return idx.probeWalk(dst, start, nil, 0, first,
		mover.centre, mover.centre.Add(mover.delta), 0.5*math.Hypot(box.R-box.L, box.T-box.B),
		bits, collidesWith, exclude, mover)
}

// shapeAllSlots is shapeWalk's find-all form with each Hit's entry slot kept
// beside it, slotBase added, as probeAllSlots is ProbeAll's: the path test of
// a fast solid Body that is not a circle, over the Body index, which keeps no
// Entity to slot table to ask afterwards. start is where the ordered run
// begins in dst.
func (idx *index) shapeAllSlots(
	dst []Hit, start int, slots *[]int32, slotBase int32, mover *shapeProbe,
	bits, collidesWith uint32, exclude ecs.Entity,
) []Hit {
	box := mover.box
	dst, _, _ = idx.probeWalk(dst, start, slots, slotBase, false,
		mover.centre, mover.centre.Add(mover.delta), 0.5*math.Hypot(box.R-box.L, box.T-box.B),
		bits, collidesWith, exclude, mover)
	return dst
}

// circlePath is where a circle Shape's centre runs when its Position runs from
// from to to at a fixed angle: its offset turned by the angle, ahead of both.
func circlePath(shape Shape, from, to m.Vec2d, angle float64) (m.Vec2d, m.Vec2d) {
	centre := NewTransformRigid(from, angle).Point(shape.verts[0])
	return centre, centre.Add(to.Sub(from))
}

// Overlap appends every Entity the placed Shape touches to dst, unordered, and
// returns it. It counts as touching exactly what Contact detection counts, so a
// point query is a circle of radius 0 and two radius-0 segments crossing is a
// question for Probe rather than for Overlap.
//
// verts is the Polygon Component's vertices and is nil for every kind but Poly.
func (idx *index) Overlap(
	dst []ecs.Entity, shape Shape, at m.Vec2d, angle float64, verts []m.Vec2d,
	bits, collidesWith uint32, exclude ecs.Entity,
) []ecs.Entity {
	var scratch [worldScratchLen]m.Vec2d
	world := worldRunFor(scratch[:], shape, verts)
	transform := NewTransformRigid(at, angle)
	used, box := cacheWorldAt(shape, transform, verts, world)
	if used == 0 {
		return dst
	}

	left, bottom := idx.cell(box.L), idx.cell(box.B)
	right, top := idx.cell(box.R), idx.cell(box.T)

	for i := left; i <= right; i++ {
		for j := bottom; j <= top; j++ {
			for cursor := idx.buckets[idx.bucket(i, j)]; cursor >= 0; cursor = idx.links[cursor].next {
				e := &idx.entries[idx.links[cursor].entry]
				if !firstScannedCell(e, i, j, left, bottom) {
					continue
				}
				if e.entity == exclude || !collides(bits, collidesWith, e.shape.CollisionBits, e.shape.CollidesWith) {
					continue
				}
				if !box.Intersects(e.box) {
					continue
				}
				if _, _, ok := penetrateWorld(
					shape, transform, world[:used],
					e.shape, e.transform, idx.slab[e.world:e.world+e.worldLen],
				); ok {
					dst = append(dst, e.entity)
				}
			}
		}
	}
	return dst
}

// probeAllSlots is ProbeAll with each Hit's entry slot appended to *slots in
// the same order, (*slots)[i] naming the entry behind dst[len(dst)+i] as it
// stood on the way in; the caller empties *slots first. It is the swept
// Sensor's Probe of the Body index, which keeps no Entity to slot table to ask
// afterwards. slots is the caller's own long-lived buffer, so that pointing at
// it costs no allocation.
func (idx *index) probeAllSlots(
	dst []Hit, slots *[]int32, from, to m.Vec2d, radius float64,
	bits, collidesWith uint32, exclude ecs.Entity,
) []Hit {
	dst, _, _ = idx.probeWalk(dst, len(dst), slots, 0, false, from, to, radius, bits, collidesWith, exclude, nil)
	return dst
}

// world is an entry's run of the index's world-cache slab.
func (idx *index) world(e *entry) []m.Vec2d { return idx.slab[e.world : e.world+e.worldLen] }

// probeWalk is the broadphase descent both Probes share: cp's grid walk, one
// cell at a time in increasing order of the Probe's fraction, narrowing on each
// candidate with the closed forms.
//
// first shortens the walk as the nearest Hit improves, which is cp's
// SegmentQueryFirst; otherwise every Hit is inserted into dst in order of T,
// which is also what keeps a Shape spanning several cells from being reported
// twice. The nearest Probe needs no such guard, a repeated test giving the same
// answer.
//
// start is where the ordered run begins in dst, which is len(dst) for every
// Probe but the Body index's second walk over its sleepers: that walk inserts
// into the run the first one began, so the two grids answer as one ordered run.
// slots is nil but for a sweep's Probe, whose run it keeps beside dst, each
// slot offset by slotBase.
//
// mover is nil but for a ProbeWith, whose moving Shape every candidate is then
// tested against in place of the circle, and whose swept box is the leaf
// rejection. It is a parameter of its own and not a field of the walk because
// the walk's Hits outlive it, which escape analysis cannot tell from the
// mover's own stack runs: held in the walk, they would go to the heap with
// every query.
func (idx *index) probeWalk(
	dst []Hit, start int, slots *[]int32, slotBase int32, first bool, from, to m.Vec2d, radius float64,
	bits, collidesWith uint32, exclude ecs.Entity, mover *shapeProbe,
) ([]Hit, Hit, bool) {
	walk := probing{
		dst:          dst,
		slots:        slots,
		slotBase:     slotBase,
		first:        first,
		start:        start,
		from:         from,
		to:           to,
		radius:       radius,
		bits:         bits,
		collidesWith: collidesWith,
		exclude:      exclude,
		box: NewBB(
			math.Min(from.X, to.X)-radius, math.Min(from.Y, to.Y)-radius,
			math.Max(from.X, to.X)+radius, math.Max(from.Y, to.Y)+radius,
		),
		exit: 1,
	}
	if mover != nil {
		walk.box = mover.box.Merge(mover.box.Offset(mover.delta))
	}

	// The broadphase inflates by the query radius before descending, which is
	// cp's defect 5 fixed — in C too, where the un-inflated ray reaches the
	// index and a Probe with a radius misses roughly a fifth of its Hits. In a
	// grid the inflation is a band of cells around the cells the ray crosses,
	// wide enough that no cell the swept circle reaches is missed. At radius 0
	// the band is the one cell and the walk is cp's own.
	//
	// Only the first step scans the whole band: every step after it adds the one
	// row or column its move brought into reach, the rest having been scanned
	// already.
	reach := int32(0)
	if radius > 0 {
		reach = int32(radius*idx.inverseCell) + 1
		walk.band = true
	}

	// cp's grid walk, after "raytracing on a grid": step to whichever of the
	// next vertical and next horizontal cell boundary comes first.
	a := from.MulS(idx.inverseCell)
	b := to.MulS(idx.inverseCell)
	cellX, cellY := floorCell(a.X), floorCell(a.Y)

	var xInc, yInc int32
	var firstVertical, firstHorizontal float64

	if b.X > a.X {
		xInc = 1
		firstHorizontal = math.Floor(a.X+1.0) - a.X
	} else {
		xInc = -1
		firstHorizontal = a.X - math.Floor(a.X)
	}
	if b.Y > a.Y {
		yInc = 1
		firstVertical = math.Floor(a.Y+1.0) - a.Y
	} else {
		yInc = -1
		firstVertical = a.Y - math.Floor(a.Y)
	}

	dx, dy := math.Abs(b.X-a.X), math.Abs(b.Y-a.Y)
	dtdx, dtdy := infinity, infinity
	if dx != 0 {
		dtdx = 1.0 / dx
	}
	if dy != 0 {
		dtdy = 1.0 / dy
	}

	nextHorizontal, nextVertical := dtdx, dtdy
	if firstHorizontal != 0 {
		nextHorizontal = firstHorizontal * dtdx
	}
	if firstVertical != 0 {
		nextVertical = firstVertical * dtdy
	}

	var t float64
	for i := cellX - reach; i <= cellX+reach; i++ {
		for j := cellY - reach; j <= cellY+reach; j++ {
			if mover == nil {
				idx.probeCell(&walk, i, j)
			} else {
				idx.shapeCell(&walk, mover, i, j)
			}
		}
	}

	for {
		steppedInY := nextVertical < nextHorizontal
		if steppedInY {
			cellY += yInc
			t = nextVertical
			nextVertical += dtdy
		} else {
			cellX += xInc
			t = nextHorizontal
			nextHorizontal += dtdx
		}
		if t >= walk.exit {
			break
		}

		if steppedInY {
			j := cellY + yInc*reach
			for i := cellX - reach; i <= cellX+reach; i++ {
				if mover == nil {
					idx.probeCell(&walk, i, j)
				} else {
					idx.shapeCell(&walk, mover, i, j)
				}
			}
		} else {
			i := cellX + xInc*reach
			for j := cellY - reach; j <= cellY+reach; j++ {
				if mover == nil {
					idx.probeCell(&walk, i, j)
				} else {
					idx.shapeCell(&walk, mover, i, j)
				}
			}
		}
	}

	return walk.dst, walk.best, walk.found
}

// probeCell narrows on every candidate listed in one cell.
func (idx *index) probeCell(walk *probing, i, j int32) {
	if walk.band {
		// The band is a whole cell wide whatever the radius, so a cell in it may
		// be out of the swept circle's reach altogether. The cell's own box,
		// grown by the radius, says which, and it costs far less than testing
		// what the cell holds.
		left, bottom := float64(i)*idx.cellSize, float64(j)*idx.cellSize
		grown := NewBB(
			left-walk.radius, bottom-walk.radius,
			left+idx.cellSize+walk.radius, bottom+idx.cellSize+walk.radius,
		)
		if !grown.IntersectsSegment(walk.from, walk.to) {
			return
		}
	}

	for cursor := idx.buckets[idx.bucket(i, j)]; cursor >= 0; cursor = idx.links[cursor].next {
		e := &idx.entries[idx.links[cursor].entry]
		if e.entity == walk.exclude ||
			!collides(walk.bits, walk.collidesWith, e.shape.CollisionBits, e.shape.CollidesWith) {
			continue
		}
		// The leaf rejection, the Probe's own box against the entry's. It is the
		// same inflation the descent makes, and it keeps a closed form off a
		// Shape that merely shares a cell, at four comparisons.
		if !walk.box.Intersects(e.box) {
			continue
		}
		hit, ok := probeWorld(walk.from, walk.to, walk.radius, e.shape, idx.slab[e.world:e.world+e.worldLen])
		if !ok {
			continue
		}
		hit.Entity = e.entity

		if walk.first {
			if !walk.found || hit.T < walk.best.T {
				walk.best, walk.found = hit, true
				walk.exit = hit.T
			}
			continue
		}
		if walk.slots != nil {
			walk.dst, *walk.slots = insertHitSlot(walk.dst, *walk.slots, walk.start, hit,
				walk.slotBase+idx.links[cursor].entry)
			continue
		}
		walk.dst = insertHit(walk.dst, walk.start, hit)
	}
}

// shapeCell is probeCell for a moving Shape: the same band, filters and
// ordering, with each candidate tested by the swept convex test. Only a fast
// solid Body's path test keeps slots beside its Hits.
//
// It is a loop of its own rather than a branch in probeCell's, so that the
// circle's loop, which is every Probe's and the swept Sensor's, is as it was:
// the branch taken once a candidate cost ProbeAll 2 to 3%, read on the
// minimums of an interleaved A/B, and taken once a cell it costs nothing.
func (idx *index) shapeCell(walk *probing, mover *shapeProbe, i, j int32) {
	if walk.band {
		left, bottom := float64(i)*idx.cellSize, float64(j)*idx.cellSize
		grown := NewBB(
			left-walk.radius, bottom-walk.radius,
			left+idx.cellSize+walk.radius, bottom+idx.cellSize+walk.radius,
		)
		if !grown.IntersectsSegment(walk.from, walk.to) {
			return
		}
	}

	for cursor := idx.buckets[idx.bucket(i, j)]; cursor >= 0; cursor = idx.links[cursor].next {
		e := &idx.entries[idx.links[cursor].entry]
		if e.entity == walk.exclude ||
			!collides(walk.bits, walk.collidesWith, e.shape.CollisionBits, e.shape.CollidesWith) {
			continue
		}
		if !walk.box.Intersects(e.box) {
			continue
		}
		hit, ok := mover.against(e.shape, e.box, idx.slab[e.world:e.world+e.worldLen])
		if !ok {
			continue
		}
		hit.Entity = e.entity

		if walk.first {
			if !walk.found || hit.T < walk.best.T {
				walk.best, walk.found = hit, true
				walk.exit = hit.T
			}
			continue
		}
		if walk.slots != nil {
			walk.dst, *walk.slots = insertHitSlot(walk.dst, *walk.slots, walk.start, hit,
				walk.slotBase+idx.links[cursor].entry)
			continue
		}
		walk.dst = insertHit(walk.dst, walk.start, hit)
	}
}

func (idx *index) allocEntry() int32 {
	if n := len(idx.freeEntries); n > 0 {
		slot := idx.freeEntries[n-1]
		idx.freeEntries = idx.freeEntries[:n-1]
		return slot
	}
	idx.entries = append(idx.entries, entry{})
	return int32(len(idx.entries) - 1)
}

// cell is which cell a world coordinate is in. The clamp is a guard cp has no
// need of, its cell index being an int: a NaN or an infinite coordinate would
// otherwise make the conversion platform-defined rather than merely wrong.
func (idx *index) cell(v float64) int32 { return floorCell(v * idx.inverseCell) }

// bucket is cp's hashFunc, which mixes the two cell coordinates and folds them
// into the table. Two cells may share a bucket; every candidate is checked
// against its own box, so a collision costs a test and never an answer.
func (idx *index) bucket(i, j int32) int {
	return int((uint(int(i))*1640531513 ^ uint(int(j))*2654435789) % uint(len(idx.buckets)))
}

func (idx *index) clearBuckets() {
	for i := range idx.buckets {
		idx.buckets[i] = -1
	}
}

// list puts an entry into every cell its box covers, growing the table first
// when the listings would outgrow it.
func (idx *index) list(slot int32) {
	e := &idx.entries[slot]
	if e.worldLen == 0 || !finiteBB(e.box) {
		// A Shape placed at a NaN or an infinity is listed in no cell at all,
		// rather than in every cell between the two ends of the grid; cp never
		// meets this, its own index being a tree over the box itself. A Shape
		// with no world cache at all is listed in none either — which now means
		// a ShapePoly whose Polygon Component is missing or empty, every other
		// kind carrying its vertex count in its kind.
		e.left, e.bottom, e.right, e.top = 0, 0, -1, -1
		return
	}
	e.left, e.bottom = idx.cell(e.box.L), idx.cell(e.box.B)
	e.right, e.top = idx.cell(e.box.R), idx.cell(e.box.T)

	cells := (int(e.right-e.left) + 1) * (int(e.top-e.bottom) + 1)
	if idx.listings+cells > len(idx.buckets) {
		idx.growBuckets(idx.listings + cells)
		return
	}
	idx.pushCells(slot)
}

func (idx *index) pushCells(slot int32) {
	e := &idx.entries[slot]
	for i := e.left; i <= e.right; i++ {
		for j := e.bottom; j <= e.top; j++ {
			bucket := idx.bucket(i, j)
			if idx.bucketHolds(bucket, slot) {
				// Two of this entry's cells hash alike; one listing is enough,
				// which is cp's containsHandle test.
				continue
			}
			idx.buckets[bucket] = idx.allocLink(slot, idx.buckets[bucket])
			idx.listings++
		}
	}
}

func (idx *index) bucketHolds(bucket int, slot int32) bool {
	for at := idx.buckets[bucket]; at >= 0; at = idx.links[at].next {
		if idx.links[at].entry == slot {
			return true
		}
	}
	return false
}

func (idx *index) allocLink(slot, next int32) int32 {
	if n := len(idx.freeLinks); n > 0 {
		at := idx.freeLinks[n-1]
		idx.freeLinks = idx.freeLinks[:n-1]
		idx.links[at] = link{entry: slot, next: next}
		return at
	}
	idx.links = append(idx.links, link{entry: slot, next: next})
	return int32(len(idx.links) - 1)
}

// unlist takes an entry out of every cell it was listed in.
func (idx *index) unlist(slot int32) {
	e := &idx.entries[slot]
	for i := e.left; i <= e.right; i++ {
		for j := e.bottom; j <= e.top; j++ {
			bucket := idx.bucket(i, j)
			previous := int32(-1)
			for at := idx.buckets[bucket]; at >= 0; at = idx.links[at].next {
				if idx.links[at].entry != slot {
					previous = at
					continue
				}
				if previous < 0 {
					idx.buckets[bucket] = idx.links[at].next
				} else {
					idx.links[previous].next = idx.links[at].next
				}
				idx.freeLinks = append(idx.freeLinks, at)
				idx.listings--
				break
			}
		}
	}
}

// growBuckets doubles the table until it holds the wanted listings twice over
// and lists every live entry again.
func (idx *index) growBuckets(wanted int) {
	size := len(idx.buckets)
	for size < wanted*2 {
		size *= 2
	}
	if size != len(idx.buckets) {
		idx.buckets = make([]int32, size)
	}
	idx.clearBuckets()
	idx.links = idx.links[:0]
	idx.freeLinks = idx.freeLinks[:0]
	idx.listings = 0

	for slot := range idx.entries {
		if idx.entries[slot].live {
			idx.pushCells(int32(slot))
		}
	}
}

// insertHit puts a Hit into the run this call appended, in order of T, unless
// the Entity is already there — which is how a Shape listed in several cells is
// reported once without an index keeping a per-query stamp as cp's handles do.
func insertHit(dst []Hit, start int, hit Hit) []Hit {
	for i := start; i < len(dst); i++ {
		if dst[i].Entity == hit.Entity {
			return dst
		}
	}
	dst = append(dst, hit)
	for i := len(dst) - 1; i > start && dst[i-1].T > dst[i].T; i-- {
		dst[i-1], dst[i] = dst[i], dst[i-1]
	}
	return dst
}

// insertHitSlot is insertHit keeping a run of slots in step with the Hits:
// slots[i] belongs to dst[start+i], and moves when its Hit does.
func insertHitSlot(dst []Hit, slots []int32, start int, hit Hit, slot int32) ([]Hit, []int32) {
	for i := start; i < len(dst); i++ {
		if dst[i].Entity == hit.Entity {
			return dst, slots
		}
	}
	dst = append(dst, hit)
	slots = append(slots, slot)
	for i := len(dst) - 1; i > start && dst[i-1].T > dst[i].T; i-- {
		dst[i-1], dst[i] = dst[i], dst[i-1]
		slots[i-1-start], slots[i-start] = slots[i-start], slots[i-1-start]
	}
	return dst, slots
}

// firstScannedCell reports whether this is the one cell of a rectangle scan at
// which an entry is tested: the corner of the overlap between the entry's cells
// and the scanned ones. It replaces cp's per-handle stamp, which a Resource
// concurrent readers share cannot keep.
func firstScannedCell(e *entry, i, j, scanLeft, scanBottom int32) bool {
	return i == max(e.left, scanLeft) && j == max(e.bottom, scanBottom)
}

func floorCell(v float64) int32 {
	f := math.Floor(v)
	switch {
	case math.IsNaN(f):
		return 0
	case f >= math.MaxInt32:
		return math.MaxInt32
	case f <= math.MinInt32:
		return math.MinInt32
	}
	return int32(f)
}

// finiteBB reports whether every edge of a box is a number.
func finiteBB(bb BB) bool {
	return !math.IsNaN(bb.L) && !math.IsNaN(bb.B) && !math.IsNaN(bb.R) && !math.IsNaN(bb.T) &&
		!math.IsInf(bb.L, 0) && !math.IsInf(bb.B, 0) && !math.IsInf(bb.R, 0) && !math.IsInf(bb.T, 0)
}
