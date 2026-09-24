package internal

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/libs/m"
)

// The index's half of the swept Sensor: the Insert that records where a Shape
// was when the tick began, and the entry lookups the sweep reads it back
// through. Why the sweep exists, and what it deliberately does not cover, is
// in contacts-sensors.go.

// InsertMoving is Insert for a Shape that may have moved during the tick,
// recording where it stood when the tick began. It is what the Body index is
// filled through.
//
// Only a moving circle Sensor keeps anything. Every other Shape is inserted
// exactly as Insert inserts it, so the path costs the rebuild one predictable
// branch a Body and nothing else.
func (idx *BodyIndex) InsertMoving(
	entity ecs.Entity, shape Shape,
	at, previous m.Vec2d, angle, previousAngle float64,
	verts []m.Vec2d,
) {
	slot := idx.insert(entity, shape, at, angle, verts)
	idx.sweepFrom(slot, shape, previous, previousAngle)
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
	idx.sweepFrom(slot, shape, previous, previousAngle)
}

// sweepFrom marks the entry an Insert just filled as a moving circle Sensor,
// when it is one, and records the centre its path starts at.
func (idx *index) sweepFrom(slot int32, shape Shape, previous m.Vec2d, previousAngle float64) {
	if slot < 0 || !shape.Sensor || shape.Kind != ShapeCircle {
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
	if shape.Verts[0] != (m.Vec2d{}) {
		from = NewTransformRigid(previous, previousAngle).Point(shape.Verts[0])
	}
	if from == idx.slab[e.world] {
		// A Sensor that did not move is not a moving Sensor: it is tested
		// discretely, reports T = 1 and carries the overlap where the tick
		// ended, which is what an app that teleported it asked for by setting
		// Previous = Current.
		return
	}
	e.previousCentre, e.swept = from, true
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
