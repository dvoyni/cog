package types

import (
	"cmp"
	"slices"

	"github.com/dvoyni/cog/libs/m"
)

// The swept Sensor, which is what keeps a point from tunnelling.
//
// The package has no projectile concept. A projectile is an ordinary Dynamic
// body with a Shape marked a Sensor, which is the general mechanism, and it
// serves pressure plates and area damage as much as arrows. What makes it not
// pass through a wall is here: every moving Sensor, whatever its Shape, is
// Probed once a tick, from where it stood when the tick began to where
// Integrate left it, so its Hits are a superset of what a discrete test at the
// tick's end would find — what it overlapped at the start is Hit at T = 0 and
// what it overlaps at the end is Hit before T = 1. There is no opt-in flag and
// no speed threshold (continuous-collision.md § Every moving Sensor is swept).
//
// cp has no counterpart to sweep. Its own answer to tunnelling is the app's:
// cpSpaceSegmentQuery by hand, or a Shape large enough that one step cannot
// clear it. So this is the ECS layout departing from cp in the one direction
// the layout allows. A circle's Probe is cp's own segment query, unchanged,
// against cp's own broadphase walk; any other Shape is moved at its end angle
// by the swept convex test a fast solid Body's path is tested with.
//
// One thing is deliberately not covered, and it is an accepted hole rather
// than an oversight: a Sensor's path is a chord and not the polyline it really
// flew, so a sharply curving Sensor can clip a corner within one tick. Solid
// Bodies share it.
//
// And two things the plugin never does. It never moves a Sensor back and never
// stops one: snap-back, reflection, an explosion and an expiry are all app
// writes, made from the Contacts the tick reported — the app reads the first
// entry and writes Position.Current = Previous. And it never invents a path: an
// app that teleports a Sensor sets Previous = Current, which is what render
// interpolation already requires of it. A Sensor that Integrate does not move,
// having no Velocity of its own, has a Previous no System maintains, so it is
// the app's to write; the stationary Sensor a pressure plate is made of is a
// Static one, which is never Probed at all. A fast solid Body crossing a Sensor
// that did not move reports it all the same (crossSensors).

// sweepSensor is the Sensor's half of the path pass (contacts-paths.go): one
// moving Sensor in the Body index, at slot, swept along its path through both
// indices, and every Shape it met written as a Contact.
//
// A target that is not marked is taken where the tick left it. A marked one
// meets the Sensor along the pair's relative motion instead, from both start
// poses, with one T: every fast solid Body is met here, the Body's own walk
// passing over a marked Sensor, and of two moving Sensors the lower Entity's
// walk writes the pair.
//
// It runs before the discrete walk, so one Sensor's entries sit together and in
// order of T, ahead of every solid pair and well ahead of the Ended run. The
// discrete walk then leaves every pair a swept Sensor is party to alone, its
// Hits being the superset.
//
// The buffers it needs are the list's own, refilled once a Sensor, so a steady
// scene allocates nothing here.
func (c *Contacts) sweepSensor(bodies *BodyIndex, statics *StaticIndex, sensor *entry, slot int) {
	moving := &bodies.index
	world := moving.world(sensor)
	bits, collidesWith := sensor.shape.CollisionBits, sensor.shape.CollidesWith
	path := bodyPath{body: sensor, world: world, from: sensor.previousCentre, delta: pathDelta(sensor, world), rests: -1}

	// The run a Shape that is not a circle is Probed in holds two movers: its
	// own path, and a partner's relative path when the overlap it started in
	// is asked for.
	used := len(world)
	if need := 4 * used; len(c.mover) < need {
		c.mover = make([]m.Vec2d, need)
	}

	// A Probe that starts inside something reports it, and a Sensor is in
	// the Body index itself, so it would Hit itself first every tick — one
	// excluded Entity, which is exactly what exclude is for. The groups are
	// the Sensor's own, so a Sensor that collides with nothing is invisible
	// to its own Probe too, and nothing here skips a Sensor: Overlap has to
	// be able to find one, which is where the port departs from cp's point
	// and segment queries.
	c.probeSlots = c.probeSlots[:0]
	var split int
	var mover shapeProbe
	if sensor.shape.Kind == ShapeCircle {
		from, to, radius := path.from, world[0], sensor.shape.Radius
		// The awake grid is asked directly while nothing sleeps, which keeps
		// the sweep the code it was before sleeping. The Body index's own
		// probeAllSlots asks both grids and does not inline, and going through
		// it measured 3% slower on the reference step at N = 1 024 with
		// sleeping off, interleaved; the profile puts less than that in the
		// sweep itself, so the number is quoted and the cause is not.
		if bodies.sleeping == 0 {
			c.probes = moving.probeAllSlots(c.probes[:0], &c.probeSlots,
				from, to, radius, bits, collidesWith, sensor.entity)
		} else {
			c.probes = bodies.probeAllSlots(c.probes[:0], &c.probeSlots,
				from, to, radius, bits, collidesWith, sensor.entity)
		}
		split = len(c.probes)
		c.probes = statics.ProbeAll(c.probes, from, to, radius, bits, collidesWith, sensor.entity)
	} else {
		mover = shapeProbeBack(c.mover[:2*used], sensor.shape, world, sensor.box, path.delta)
		c.probes = moving.shapeAllSlots(c.probes[:0], 0, &c.probeSlots, 0,
			&mover, bits, collidesWith, sensor.entity)
		if bodies.sleeping > 0 {
			c.probes = bodies.sleepers.shapeAllSlots(c.probes, 0, &c.probeSlots, bodies.awakeSlots(),
				&mover, bits, collidesWith, sensor.entity)
		}
		split = len(c.probes)
		c.probes = statics.shapeAllSlots(c.probes, split, nil, 0,
			&mover, bits, collidesWith, sensor.entity)
	}
	path.mover = &mover

	// The marked partners, each met along the pair's relative motion, kept in
	// order of T, which a handful of them is sorted into by insertion.
	c.findPartners(moving, &path, int32(slot))
	met := c.partners[:0]
	for _, p := range c.partners {
		if !p.ok {
			continue
		}
		at := len(met)
		met = append(met, p)
		for ; at > 0 && met[at-1].hit.T > p.hit.T; at-- {
			met[at] = met[at-1]
		}
		met[at] = p
	}

	// Each run is already ordered by T and the three never name the same
	// Entity: an Entity is in one index or the other, and a marked one, met
	// along the relative motion, is passed over in the first. So merging them
	// is the whole of the ordering.
	for at, still, partner := 0, split, 0; at < split || still < len(c.probes) || partner < len(met); {
		switch {
		case at < split && (still >= len(c.probes) || c.probes[at].T <= c.probes[still].T) &&
			(partner >= len(met) || c.probes[at].T <= met[partner].hit.T):
			hit := c.probes[at]
			// The Body index keeps no Entity to slot table, so its Probe
			// hands each Hit's slot back beside it — a Sleeping body's
			// numbered after the awake grid's, in the grid of its own it
			// is kept in. A sleeper met by a Sensor stays asleep: a Sensor
			// entry neither joins an Island nor wakes one.
			otherSlot := c.probeSlots[at]
			at++
			grid, other := bodies.entryAt(otherSlot)
			if other.path {
				continue
			}
			c.sensorHit(&path, int32(slot), hit, other, otherSlot, grid.world(other), m.Vec2d{})
		case still < len(c.probes) && (partner >= len(met) || c.probes[still].T <= met[partner].hit.T):
			hit := c.probes[still]
			still++
			other, _, ok := statics.lookup(hit.Entity)
			if !ok {
				continue
			}
			// A Static party is one immovable row in the solver's gather
			// however many of them there are, which is what −1 says. A
			// Sensor entry is never solved at all, but the two runs of the
			// list are read by the same code and say the same thing.
			c.sensorHit(&path, int32(slot), hit, other, -1, statics.world(other), m.Vec2d{})
		default:
			p := met[partner]
			partner++
			other := &moving.entries[p.slot]
			otherWorld := moving.world(other)
			c.sensorHit(&path, int32(slot), p.hit, other, p.slot, otherWorld, pathDelta(other, otherWorld))
		}
	}
}

// sensorHit writes one Hit along a Sensor's path as a Contact entry. otherDelta
// is the other party's own path when the Hit is along the pair's relative
// motion, and zero for a party that is not marked.
//
// A is the Sensor that Probed. That is the entry's own rule — A is the Sensor —
// and where both parties are Sensors it is the only reading the Hit supports:
// the Hit's Point is on the Shape the Probe met and its Normal faces the
// Prober, which is exactly what "Normal is B's surface facing A" means. Two
// moving Sensors are written by the lower Entity's walk, so that one is A.
//
// The solver offsets r1 and r2 are left zero. Solve skips Sensor entries, and a
// swept entry has no one position to be offsets from — the two parties were in
// different places at T than they are at the tick's end.
func (c *Contacts) sensorHit(
	path *bodyPath, sensorSlot int32,
	hit Hit, other *entry, otherSlot int32, otherWorld []m.Vec2d, otherDelta m.Vec2d,
) {
	sensor := path.body

	// T and Depth mean one thing on every entry. A Probed Sensor carries the
	// Probe's T and a Depth of 0 — it met the Shape rather than overlapping it
	// — except one that started inside something, which reports T = 0 with the
	// overlap it started in. Along the relative motion both parties start where
	// they started, shifted alike by the other's path, so the overlap is the
	// same one.
	depth := 0.0
	if hit.T == 0 {
		along := path
		if otherDelta != (m.Vec2d{}) {
			used := len(path.world)
			var relative shapeProbe
			moved := path.relativeTo(&relative, c.mover[2*used:4*used], other, otherWorld)
			along = &moved
		}
		depth = along.startOverlap(other, otherWorld)
	}

	var made Contact
	made.A, made.B = sensor.entity, other.entity
	made.Normal = hit.Normal
	made.T = hit.T
	made.Count = 1
	made.Sensor = true
	// cp's combination rules, both plain products, as the discrete pass applies
	// them. A Sensor's material is never spent, nothing solving the pair, but
	// the two runs of the list read the same and a filter may read either.
	made.Friction = sensor.shape.Friction * other.shape.Friction
	made.Restitution = sensor.shape.Restitution * other.shape.Restitution
	// A Hit along the relative motion is against the other party where the
	// tick left it, so its Point is moved back along that party's path to
	// where the two met.
	made.Points[0] = ContactPoint{Point: hit.Point.Add(otherDelta.MulS(hit.T - 1)), Depth: depth, id: pointID(0, 0)}

	// The previous tick's entry for this unordered pair, looked up here because
	// carry takes the slot rather than finding it: the discrete walk reads the
	// cached simplex out of the same lookup, and a swept Sensor has no simplex
	// to read but wants the same phase and ignore bookkeeping.
	at, wasTouching := c.prevLookup.find(made.A, made.B)
	c.carry(&made, at, wasTouching)
	c.append(made, contactAux{slotA: sensorSlot, slotB: otherSlot})
}

// startOverlap is how deep the Shape stands in a target where its path starts,
// or 0 where it does not overlap it.
func (path *bodyPath) startOverlap(target *entry, world []m.Vec2d) float64 {
	if body := path.body; body.shape.Kind == ShapeCircle {
		_, distance, _ := pointQueryWorld(path.from, target.shape, world)
		return max(0, body.shape.Radius-distance)
	}
	return max(0, path.mover.depthAt(target.shape, target.box, world))
}

// crossing is one Sensor that did not move, crossed by a fast solid Body's
// path: the entry it makes, held back until the Body's own stop is settled.
type crossing struct {
	// slot is the Body's, in the Body index's awake grid.
	slot int32
	made Contact
	aux  contactAux
}

// crossSensors keeps every Hit of a fast solid Body's path on a Sensor that
// did not move, which no walk of the Sensor's own ever tests, as an entry that
// stops nothing (continuous-collision.md § A fast Body reports the Sensors it
// crosses). The path test is already a find-all one, so the Hits are in hand:
// the first split in the Body index, the rest in the static index. A marked
// Sensor is passed over, the pair being its own walk's, along the relative
// motion.
//
// The Sensor is A, as the entry's rule says, with the Hit's T and a Depth of
// 0, or T = 0 and the overlap for one the Body started inside.
func (c *Contacts) crossSensors(bodies *BodyIndex, statics *StaticIndex, path *bodyPath, slot int32, split int) {
	body := path.body
	for at, hit := range c.probes {
		var sensor *entry
		var sensorSlot int32
		var world []m.Vec2d
		if at < split {
			sensorSlot = c.probeSlots[at]
			grid, found := bodies.entryAt(sensorSlot)
			if !found.shape.Sensor || found.path {
				continue
			}
			sensor, world = found, grid.world(found)
		} else {
			found, _, ok := statics.lookup(hit.Entity)
			if !ok || !found.shape.Sensor {
				continue
			}
			sensor, sensorSlot, world = found, -1, statics.world(found)
		}

		depth := 0.0
		if hit.T == 0 {
			depth = path.startOverlap(sensor, world)
		}
		var made Contact
		made.A, made.B = sensor.entity, body.entity
		// The Hit's Normal faces the Body, which is B, and the entry's faces A.
		made.Normal = hit.Normal.Negate()
		made.T = hit.T
		made.Count = 1
		made.Sensor = true
		made.Friction = sensor.shape.Friction * body.shape.Friction
		made.Restitution = sensor.shape.Restitution * body.shape.Restitution
		made.Points[0] = ContactPoint{Point: hit.Point, Depth: depth, id: pointID(0, 0)}
		c.crossings = append(c.crossings, crossing{
			slot: slot,
			made: made,
			aux:  contactAux{slotA: sensorSlot, slotB: slot},
		})
	}
}

// writeCrossings writes the Sensors the fast solid Bodies crossed, once every
// Body's stop is settled: a Body stopped at T crossed only the Sensors up to
// T, and one beyond it, behind the wall the Body stopped at, was never
// reached. They are written each Sensor's together and in order of T, which
// is the list's one ordering promise, the lower Body first at a tie.
func (c *Contacts) writeCrossings() {
	slices.SortFunc(c.crossings, func(x, y crossing) int {
		if byA := cmp.Compare(x.made.A, y.made.A); byA != 0 {
			return byA
		}
		if byT := cmp.Compare(x.made.T, y.made.T); byT != 0 {
			return byT
		}
		return cmp.Compare(x.made.B, y.made.B)
	})
	for i := range c.crossings {
		crossed := &c.crossings[i]
		if first := c.firstOf(crossed.slot); first != nil && crossed.made.T > first.t {
			continue
		}
		at, wasTouching := c.prevLookup.find(crossed.made.A, crossed.made.B)
		c.carry(&crossed.made, at, wasTouching)
		c.append(crossed.made, crossed.aux)
	}
}
