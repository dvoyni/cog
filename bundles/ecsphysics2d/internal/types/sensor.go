package types

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/libs/m"
)

// The swept Sensor, which is what keeps a point from tunnelling.
//
// The package has no projectile concept. A projectile is an ordinary Dynamic
// body with a circle Shape marked a Sensor, which is the general mechanism, and
// it serves pressure plates and area damage as much as arrows. What makes it
// not pass through a wall is here: every moving circle Sensor is Probed once a
// tick, from where it stood when the tick began to where Integrate left it, so
// its Hits are a superset of what a discrete test at the tick's end would find
// — what it overlapped at the start is Hit at T = 0 and what it overlaps at the
// end is Hit before T = 1. There is no opt-in flag and no speed threshold.
//
// cp has no counterpart to sweep. Its own answer to tunnelling is the app's:
// cpSpaceSegmentQuery by hand, or a Shape large enough that one step cannot
// clear it. So this is the ECS layout departing from cp in the one direction
// the layout allows — the Probe it is built on is cp's own segment query,
// unchanged, against cp's own broadphase walk.
//
// Three things are deliberately not covered, and each is an accepted hole
// rather than an oversight:
//
//   - Box and segment Sensors are tested discretely, so a fast one can miss.
//     Sweeping a rotating polygon is a different algorithm — conservative
//     advancement — and it is not cp's either.
//   - A Sensor's path is a chord and not the polyline it really flew, so a
//     sharply curving Sensor can clip a corner within one tick.
//   - Two moving Sensors are tested against each other's end positions rather
//     than against their relative motion, so two with a radius crossing within
//     one tick can miss each other. Where both do find each other, the Hit with
//     the smaller T is the one kept.
//
// And two things the plugin never does. It never moves a Sensor back and never
// stops one: snap-back, reflection, an explosion and an expiry are all app
// writes, made from the Contacts the tick reported — the app reads the first
// entry and writes Position.Current = Previous. And it never invents a path: an
// app that teleports a Sensor sets Previous = Current, which is what render
// interpolation already requires of it. A Sensor that Integrate does not move,
// having no Velocity of its own, has a Previous no System maintains, so it is
// the app's to write; the stationary Sensor a pressure plate is made of is a
// Static one, which is never Probed at all.

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

// sweepSensors is the Probe half of Detect: every moving circle Sensor in the
// Body index, swept along its path through both indices, and every Shape it met
// written as a Contact.
//
// It runs before the discrete walk, so one Sensor's entries sit together and in
// order of T, ahead of every solid pair and well ahead of the Ended run. The
// discrete walk then leaves every pair a swept Sensor is party to alone, its
// Hits being the superset.
//
// The one buffer it needs is the list's own, refilled once a Sensor, so a
// steady scene allocates nothing here.
func (c *Contacts) sweepSensors(bodies *BodyIndex, statics *StaticIndex) {
	moving := &bodies.index
	for slot := range moving.entries {
		sensor := &moving.entries[slot]
		if !sensor.swept {
			continue
		}
		world := moving.world(sensor)
		from, to := sensor.previousCentre, world[0]
		radius := sensor.shape.Radius
		bits, collidesWith := sensor.shape.CollisionBits, sensor.shape.CollidesWith

		// A Probe that starts inside something reports it, and a Sensor is in
		// the Body index itself, so it would Hit itself first every tick — one
		// excluded Entity, which is exactly what exclude is for. The groups are
		// the Sensor's own, so a Sensor that collides with nothing is invisible
		// to its own Probe too, and nothing here skips a Sensor: Overlap has to
		// be able to find one, which is where the port departs from cp's point
		// and segment queries.
		c.probes = bodies.ProbeAll(c.probes[:0], from, to, radius, bits, collidesWith, sensor.entity)
		split := len(c.probes)
		c.probes = statics.ProbeAll(c.probes, from, to, radius, bits, collidesWith, sensor.entity)

		// Each run is already ordered by T and the two never name the same
		// Entity, an Entity being in one index or the other, so merging them is
		// the whole of the ordering.
		for at, still := 0, split; at < split || still < len(c.probes); {
			var hit Hit
			var other *entry
			var otherSlot int32
			var otherWorld []m.Vec2d
			if still >= len(c.probes) || (at < split && c.probes[at].T <= c.probes[still].T) {
				hit, at = c.probes[at], at+1
				found, foundSlot, ok := moving.lookup(hit.Entity)
				if !ok {
					continue
				}
				other, otherSlot, otherWorld = found, foundSlot, moving.world(found)
			} else {
				hit, still = c.probes[still], still+1
				found, _, ok := statics.lookup(hit.Entity)
				if !ok {
					continue
				}
				// A Static party is one immovable row in the solver's gather
				// however many of them there are, which is what −1 says. A
				// Sensor entry is never solved at all, but the two runs of the
				// list are read by the same code and say the same thing.
				other, otherSlot, otherWorld = found, -1, statics.world(found)
			}
			c.sensorHit(sensor, int32(slot), from, radius, world, hit, other, otherSlot, otherWorld)
		}
	}
}

// sensorHit writes one Hit along a Sensor's path as a Contact entry.
//
// A is the Sensor that Probed. That is the entry's own rule — A is the Sensor —
// and where both parties are Sensors it is the only reading the Hit supports:
// the Hit's Point is on the Shape the Probe met and its Normal faces the
// Prober, which is exactly what "Normal is B's surface facing A" means.
//
// The solver offsets r1 and r2 are left zero. Solve skips Sensor entries, and a
// swept entry has no one position to be offsets from — the two parties were in
// different places at T than they are at the tick's end.
func (c *Contacts) sensorHit(
	sensor *entry, sensorSlot int32, from m.Vec2d, radius float64, sensorWorld []m.Vec2d,
	hit Hit, other *entry, otherSlot int32, otherWorld []m.Vec2d,
) {
	// Two moving Sensors are tested against each other's end positions, so each
	// Probes the other and both find the pair. The smaller T wins, and the
	// loser drops its own Hit here rather than overwriting the winner's entry,
	// which would break both the ordering and the pair table's one rule that a
	// pair is inserted at most once a tick. Re-running the other Sensor's Probe
	// against this one's end pose is the same pair primitive its own walk
	// reached, the walk deciding only which candidates are tested; so the two
	// sides reach opposite conclusions and exactly one entry is written. The
	// lower Entity settles a tie, which is the same tie-break the pair table
	// orders by.
	if other.swept {
		theirs, ok := probeWorld(
			other.previousCentre, otherWorld[0], other.shape.Radius,
			sensor.shape, sensorWorld,
		)
		if ok && (theirs.T < hit.T || (theirs.T == hit.T && other.entity < sensor.entity)) {
			return
		}
	}

	// T and Depth mean one thing on every entry. A Probed Sensor carries the
	// Probe's T and a Depth of 0 — it met the Shape rather than overlapping it
	// — except one that started inside something, which reports T = 0 with the
	// overlap it started in. That overlap is what probeWorld's own
	// starts-inside branch tested the radius against.
	depth := 0.0
	if hit.T == 0 {
		if _, distance, _ := pointQueryWorld(from, other.shape, otherWorld); distance < radius {
			depth = radius - distance
		}
	}

	var made Contact
	made.A, made.B = sensor.entity, hit.Entity
	made.Normal = hit.Normal
	made.T = hit.T
	made.Count = 1
	made.Sensor = true
	// cp's combination rules, both plain products, as the discrete pass applies
	// them. A Sensor's material is never spent, nothing solving the pair, but
	// the two runs of the list read the same and a filter may read either.
	made.Friction = sensor.shape.Friction * other.shape.Friction
	made.Restitution = sensor.shape.Restitution * other.shape.Restitution
	made.Points[0] = ContactPoint{Point: hit.Point, Depth: depth, id: pointID(0, 0)}

	c.carry(&made)
	c.append(made, contactAux{slotA: sensorSlot, slotB: otherSlot})
}
