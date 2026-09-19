package types

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/libs/m"
)

// The index's half of the swept Sensor: the Insert that records where a Shape
// was when the tick began, and the entry lookup the sweep reads it back
// through. Why the sweep exists, and what it deliberately does not cover, is
// in contacts-sensors.go.

// InsertMoving is Insert for a Shape that may have moved during the tick,
// recording where it stood when the tick began. It is what the Body index is
// filled through, the static index never needing it: a Static never moves, and
// a Static Sensor is never Probed.
//
// Only a moving circle Sensor keeps anything. Every other Shape is inserted
// exactly as Insert inserts it, so the path costs the rebuild one predictable
// branch a Body and nothing else.
func (idx *index) InsertMoving(
	entity ecs.Entity, shape Shape,
	at, previous m.Vec2d, angle, previousAngle float64,
	verts []m.Vec2d,
) {
	idx.Insert(entity, shape, at, angle, verts)
	if !shape.Sensor || shape.Kind != ShapeCircle {
		return
	}
	slot, ok := idx.slots[entity]
	if !ok {
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
	e.previousCentre, e.swept = from, true
}

// lookup is the index's own entry for an Entity it holds, with the entry's slot
// beside it. A Hit names an Entity, and the sweep needs the Shape behind it.
func (idx *index) lookup(entity ecs.Entity) (*entry, int32, bool) {
	slot, ok := idx.slots[entity]
	if !ok {
		return nil, -1, false
	}
	return &idx.entries[slot], slot, true
}
