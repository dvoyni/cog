package types

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/libs/m"
)

// The index's half of the path: the Insert that records where a Shape was when
// the tick began, and the entry lookups Detect reads it back through. It marks
// two things with the one path bit: a moving circle Sensor, whose sweep is in
// contacts-sensors.go, and a solid Body past continuous collision's gate
// (continuous-collision.md § The gate), whose path test and stop are in
// contacts-paths.go.

// InsertMoving is Insert for a Shape that may have moved during the tick,
// recording where it stood when the tick began. It is what the Body index is
// filled through.
//
// Only a moving circle Sensor, or a solid Body that moved at least its own
// minimum extent, keeps anything. Every other Shape is inserted exactly as
// Insert inserts it, so the path costs the rebuild one compare a Body and
// nothing else.
func (idx *BodyIndex) InsertMoving(
	entity ecs.Entity, shape Shape,
	at, previous m.Vec2d, angle, previousAngle float64,
	verts []m.Vec2d,
) {
	slot := idx.insert(entity, shape, at, angle, verts)
	idx.markPath(slot, shape, at, previous, previousAngle)
}

// InsertMoving is the Body index's own, which the static index never needs: a
// Static never moves, and a Static Sensor is never Probed. It is kept so that
// both indices take the same calls.
func (idx *StaticIndex) InsertMoving(
	entity ecs.Entity, shape Shape,
	at, previous m.Vec2d, angle, previousAngle float64,
	verts []m.Vec2d,
) {
	slot := idx.insert(entity, shape, at, angle, verts)
	idx.markPath(slot, shape, at, previous, previousAngle)
}

// markPath sets the path bit on the entry an Insert just filled, when the
// Shape is a moving circle Sensor or a solid Body past the gate, and records
// where its path starts.
func (idx *index) markPath(slot int32, shape Shape, at, previous m.Vec2d, previousAngle float64) {
	if slot < 0 {
		return
	}
	if shape.Sensor {
		idx.sweepFrom(slot, shape, previous, previousAngle)
		return
	}

	// The gate: |Current − Previous| ≥ the minimum extent, compared squared.
	// The step is fixed, so the displacement is the whole test. An extent of
	// 0, a point or a bare segment, engages on any motion, which is the swept
	// Sensor's own rule; the second clause is what keeps it from engaging at
	// rest. Written as a negation, so a NaN movement is not marked.
	moved := at.Sub(previous)
	distance, extent := moved.Dot(moved), MinimumExtent(shape)
	if !(distance >= extent*extent && distance > 0) {
		return
	}
	e := &idx.entries[slot]
	if e.worldLen == 0 {
		return
	}
	// The path is held at the end angle, so it starts at the end pose
	// translated by Previous − Current: a circle's centre now, moved back
	// along the Position's chord, and any other Shape's Position, Previous.
	from := previous
	if shape.Kind == ShapeCircle {
		from = idx.slab[e.world].Sub(moved)
	}
	e.previousCentre, e.path = from, true
	idx.listPath(slot, moved.Negate())
}

// sweepFrom marks the entry an Insert just filled as a moving circle Sensor,
// when it is one, and records the centre its path starts at.
func (idx *index) sweepFrom(slot int32, shape Shape, previous m.Vec2d, previousAngle float64) {
	if shape.Kind != ShapeCircle {
		return
	}
	e := &idx.entries[slot]
	if e.worldLen == 0 {
		return
	}

	// The Probe is the circle's own centre from where it was to where it is.
	// A circle about the Position itself — which is every projectile — needs no
	// transform for the start: the offset is zero, so the rotation cannot move
	// it and the previous Position is the previous centre.
	from := previous
	if shape.verts[0] != (m.Vec2d{}) {
		from = NewTransformRigid(previous, previousAngle).Point(shape.verts[0])
	}
	if from == idx.slab[e.world] {
		// A Sensor that did not move is not a moving Sensor: it is tested
		// discretely, reports T = 1 and carries the overlap where the tick
		// ended, which is what an app that teleported it asked for by setting
		// Previous = Current.
		return
	}
	e.previousCentre, e.path = from, true
	idx.listPath(slot, from.Sub(idx.slab[e.world]))
}

// listPath lists a marked entry by its path box, the union of its start and
// end boxes, where Insert listed it by its end box alone
// (continuous-collision.md § Index: one bit and a path box). back is the start
// pose less the end pose: the path is held at the end angle, so the start box
// is the end box translated by it.
//
// Without it, two fast Bodies crossing at an angle are never tested: neither
// ends in a cell the other's path reaches. The entry's box stays its end box,
// so a query meets more candidates around a marked entry and still tests the
// end pose, and no answer changes. The cells already listed are skipped, the
// path box holding the end box, and an entry whose path stays in its own cells
// is left as it is.
func (idx *index) listPath(slot int32, back m.Vec2d) {
	e := &idx.entries[slot]
	box := e.box.Merge(e.box.Offset(back))
	if e.right < e.left || !finiteBB(box) {
		return
	}
	left, bottom := idx.cell(box.L), idx.cell(box.B)
	right, top := idx.cell(box.R), idx.cell(box.T)
	if left == e.left && bottom == e.bottom && right == e.right && top == e.top {
		return
	}
	e.left, e.bottom, e.right, e.top = left, bottom, right, top
	cells := (int(right-left) + 1) * (int(top-bottom) + 1)
	if idx.listings+cells > len(idx.buckets) {
		idx.growBuckets(idx.listings + cells)
		return
	}
	idx.pushCells(slot)
}

// pathBox is a marked entry's path box: its end box and the box it started the
// tick in, which is the end box moved back along its path.
func pathBox(e *entry, world []m.Vec2d) BB {
	return e.box.Merge(e.box.Offset(pathDelta(e, world).Negate()))
}

// pathDelta is a marked entry's path through the tick, from where it starts to
// where the tick left it: a circle's centre's chord, and any other Shape's
// Position's.
func pathDelta(e *entry, world []m.Vec2d) m.Vec2d {
	if e.shape.Kind == ShapeCircle {
		return world[0].Sub(e.previousCentre)
	}
	return m.Vec2d{X: e.transform.TX, Y: e.transform.TY}.Sub(e.previousCentre)
}

// lookup is the static index's own entry for an Entity it holds, with the
// entry's slot beside it. A Hit names an Entity, and the sweep needs the Shape
// behind it.
//
// The Body index has no counterpart, keeping no Entity to slot table: a sweep
// asks it through probeAllSlots, whose Hits come with their slots already.
func (idx *StaticIndex) lookup(entity ecs.Entity) (*entry, int32, bool) {
	slot, ok := idx.slots[entity]
	if !ok {
		return nil, -1, false
	}
	return &idx.entries[slot], slot, true
}
