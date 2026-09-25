package types

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/libs/m"
)

// The path pass, and the stop it makes: continuous collision
// (continuous-collision.md § Detect: one path pass, § Solve: back to T).
//
// Index marks an entry with the one path bit when Detect is to test it along
// its path through the tick: a moving Sensor, and a solid Body that
// moved at least its own minimum extent. It lists a marked entry in the grid
// by its path box. This pass runs once over the marked entries, before the
// discrete walk, and the Sensor flag picks what a path test keeps. A Sensor
// keeps every Hit, in contacts-sensors.go. A solid Body keeps its first Hit
// that can stop it, and is stopped there: the Contact it writes carries the
// Hit's T, and Solve moves a Dynamic body back to where it stood at T before
// it solves anything.
//
// A target that is not marked is taken where the tick left it. A target that
// moved into the path during the tick counts as already there, which is the
// ghost Hit the specification names as a limit. Two marked solid Bodies meet
// along their relative motion instead, from both start poses, with one T.

// stop is one fast solid Body the path pass stopped: its stopping Contact, and
// a copy of its index entry placed where it stopped, which is what the
// discrete walk tests its other pairs against.
type stop struct {
	// at is the stopping Contact's place in the tick's entries. Both parties
	// of a meeting stopped by it share one.
	at int32
	// slot is the Body's slot in the Body index's awake grid, which is the
	// order the pass visits Bodies in and so the order of the run.
	slot int32
	// world is where the copy's world cache starts in stopWorld; its length
	// is the entry's own.
	world int32
	// t is how far through the tick the Body was when it was stopped.
	t float64
	// met records that the Body was stopped by a meeting with another marked
	// Body, along their relative motion, so the other party was where its own
	// path had it at t; otherwise by a target taken where the tick left it.
	met bool
	// body is the Body's entry, its transform, box and world cache moved back
	// along its path to t, and its path bit cleared. Its grid cells are the
	// entry's own, so it is never listed or walked, only tested.
	body entry
}

// firstStop is one fast solid Body's earliest stop while the pass is still
// finding them: the first Hit of its own path that stops it, or a meeting with
// another marked Body that comes sooner. It is written once every Body's has
// been found, because a meeting is found by one party's walk and may be the
// other's earliest.
type firstStop struct {
	slot int32
	// t is the stop's T, and above 1 while the Body has none.
	t float64
	// hit, other and otherSlot are the first stopping Hit of the Body's own
	// path, on a target that is not marked: a Static at otherSlot −1.
	hit       Hit
	other     *entry
	otherSlot int32
	// meeting is the place in the tick's meetings of the one that stops the
	// Body, or −1 when its own Hit does.
	meeting int32
}

// meeting is a pair of marked solid Bodies that meet along their relative
// motion, tested and judged once, by the walk of the lower Entity, a.
type meeting struct {
	a, b int32
	// hit is a's path relative to b's, against b where the tick left it: T is
	// the pair's one T, and Point is on b's surface translated by the rest of
	// b's path, (1 − T)·d_b.
	hit Hit
	// at is the Contact's place in the tick's entries once written, else −1.
	at int32
}

// partner is one marked solid Body a fast solid Body's path box meets, with
// what the pair's relative motion found, Probed from the Body's side.
type partner struct {
	slot int32
	hit  Hit
	ok   bool
}

// pathPass is Detect's one pass over the marked entries of the Body index: a
// moving Sensor is swept, and a fast solid Body is stopped at the first thing
// its path meets and reports the Sensors that did not move which it crossed on
// the way.
//
// It runs before the discrete walk, so what it writes sits at the front of the
// list: a moving Sensor's entries together and in order of T, then each
// stopping Contact, then the crossed Sensors' entries, each Sensor's together
// and in order of T.
//
// slop is the Slop setting, how far two Shapes may overlap and be left alone,
// which is how much deeper than a fast Body already rests a target on its
// path may reach without stopping it.
func (c *Contacts) pathPass(bodies *BodyIndex, statics *StaticIndex, jointed *JointedPairs, slop float64) {
	moving := &bodies.index
	c.firsts, c.meetings, c.crossings = c.firsts[:0], c.meetings[:0], c.crossings[:0]
	for slot := range moving.entries {
		marked := &moving.entries[slot]
		if !marked.path || !marked.live {
			continue
		}
		if marked.shape.Sensor {
			c.sweepSensor(bodies, statics, marked, slot)
			continue
		}
		c.findStop(bodies, statics, jointed, marked, int32(slot), slop)
	}
	if len(c.firsts) > 0 {
		c.writeStops(bodies, statics, jointed)
	}
	if len(c.crossings) > 0 {
		c.writeCrossings()
	}
}

// findStop is the solid half of the path pass: one fast solid Body, at slot,
// Probed along its path through both indices, and its first Hit that can stop
// it found; and the marked Bodies its path box meets, each tested along the
// pair's relative motion.
//
// The Shape is held at its end angle, so the path is its end pose moved back
// along the Position's chord: a circle's centre by Probe, anything else by the
// swept convex test over its own world cache, moved back. The Probe is a
// find-all one, because the first Hit may not be one that stops: a Hit is
// passed over when
//
//   - the target is marked itself. Where the tick left it is not where the
//     Body meets it, the error being its whole movement in the tick, which is
//     past its own extent; the pair meets along its relative motion instead;
//   - the target is a Sensor, which never stops a Body. One that did not
//     move is reported instead, as an entry that stops nothing
//     (crossSensors);
//   - it is at T = 0, a target the Body already touched where the tick began,
//     which is left to the discrete walk, so a fast ball rolling along a floor
//     is not stopped by the floor;
//   - a Joint holds the pair apart, as the discrete walk's own test rejects
//     it. The collision bits are the Probe's own filter already;
//   - the path does not enter the target: it is a seam in a surface the Body
//     slides along, which the discrete walk resolves on the right side
//     (enters). This is asked last, and only of a Hit that would stop.
//
// A Hit at T = 1 is a touch where the tick ended, which the discrete walk
// finds, and it stops nothing.
//
// A pair of marked solid Bodies is judged by the lower Entity's walk, by the
// same rules along the relative path, and a meeting that would stop it is
// recorded for both parties, whichever of them it turns out to stop.
func (c *Contacts) findStop(
	bodies *BodyIndex, statics *StaticIndex, jointed *JointedPairs, body *entry, slot int32, slop float64,
) {
	moving := &bodies.index
	world := moving.world(body)
	bits, collidesWith := body.shape.CollisionBits, body.shape.CollidesWith

	// The run the Shape is Probed in holds three movers: its own path, a
	// partner's relative path, and the one the resting depth measures with.
	used := len(world)
	if need := 6 * used; len(c.mover) < need {
		c.mover = make([]m.Vec2d, need)
	}

	c.probeSlots = c.probeSlots[:0]
	from := body.previousCentre
	var delta m.Vec2d
	var split int
	var mover shapeProbe
	if body.shape.Kind == ShapeCircle {
		to, radius := world[0], body.shape.Radius
		delta = to.Sub(from)
		c.probes = bodies.probeAllSlots(c.probes[:0], &c.probeSlots,
			from, to, radius, bits, collidesWith, body.entity)
		split = len(c.probes)
		c.probes = statics.ProbeAll(c.probes, from, to, radius, bits, collidesWith, body.entity)
	} else {
		delta = m.Vec2d{X: body.transform.TX, Y: body.transform.TY}.Sub(from)
		mover = shapeProbeBack(c.mover[:2*used], body.shape, world, body.box, delta)
		c.probes = moving.shapeAllSlots(c.probes[:0], 0, &c.probeSlots, 0,
			&mover, bits, collidesWith, body.entity)
		if bodies.sleeping > 0 {
			c.probes = bodies.sleepers.shapeAllSlots(c.probes, 0, &c.probeSlots, bodies.awakeSlots(),
				&mover, bits, collidesWith, body.entity)
		}
		split = len(c.probes)
		c.probes = statics.shapeAllSlots(c.probes, split, nil, 0,
			&mover, bits, collidesWith, body.entity)
	}
	path := bodyPath{body: body, world: world, from: from, delta: delta, mover: &mover, rests: -1, slop: slop}
	c.findPartners(moving, &path, slot)
	c.crossSensors(bodies, statics, &path, slot, split)

	// Each run is ordered by T, so the first Hit that stops in each is that
	// run's answer, and the nearer of the two is the stop.
	first := firstStop{slot: slot, t: 2, otherSlot: -1, meeting: -1}
	for at := range split {
		candidate := c.probes[at]
		within, found := bodies.entryAt(c.probeSlots[at])
		if !found.path && stops(body, candidate, found, jointed) &&
			c.enters(&path, &path, bodies, statics, split, candidate, found, within.world(found)) {
			first.t, first.hit, first.other, first.otherSlot = candidate.T, candidate, found, c.probeSlots[at]
			break
		}
	}
	for _, candidate := range c.probes[split:] {
		if candidate.T >= first.t {
			break
		}
		found, _, ok := statics.lookup(candidate.Entity)
		if ok && stops(body, candidate, found, jointed) &&
			c.enters(&path, &path, bodies, statics, split, candidate, found, statics.index.world(found)) {
			first.t, first.hit, first.other, first.otherSlot = candidate.T, candidate, found, -1
			break
		}
	}

	// The meetings this walk judges: every partner of a higher Entity whose
	// relative path would stop this Body. Each is kept whatever this Body's
	// own first Hit, since it may be the other party's earliest.
	var relative shapeProbe
	for _, p := range c.partners {
		other := &moving.entries[p.slot]
		if !p.ok || other.entity < body.entity || !stops(body, p.hit, other, jointed) {
			continue
		}
		otherWorld := moving.world(other)
		along := path.relativeTo(&relative, c.mover[2*used:4*used], other, otherWorld)
		if c.enters(&along, &path, bodies, statics, split, p.hit, other, otherWorld) {
			c.meetings = append(c.meetings, meeting{a: slot, b: p.slot, hit: p.hit, at: -1})
		}
	}
	c.firsts = append(c.firsts, first)
}

// findPartners fills the partners with the marked entries whose path boxes
// meet this one's, each tested along the pair's relative motion. They are found
// in the grid cells the entry is listed in, which are its path box's, every
// marked entry being listed by its own path box: two paths that meet share a
// cell.
//
// Which partners a walk takes is which pairs it writes (continuous-collision.md
// § Which walk writes a pair): a solid Body takes the marked solid Bodies, a
// moving Sensor meeting it being the Sensor's walk's; a Sensor takes every
// marked solid Body, and the marked Sensors of a higher Entity.
func (c *Contacts) findPartners(moving *index, path *bodyPath, slot int32) {
	c.partners = c.partners[:0]
	body := path.body
	box := body.box.Merge(body.box.Offset(path.delta.Negate()))
	used := len(path.world)
	var relative shapeProbe
	for i := body.left; i <= body.right; i++ {
		for j := body.bottom; j <= body.top; j++ {
			for at := moving.buckets[moving.bucket(i, j)]; at >= 0; at = moving.links[at].next {
				other := moving.links[at].entry
				second := &moving.entries[other]
				if other == slot || !second.path ||
					(second.shape.Sensor && (!body.shape.Sensor || second.entity < body.entity)) ||
					!firstScannedCell(second, i, j, body.left, body.bottom) {
					continue
				}
				if !collides(body.shape.CollisionBits, body.shape.CollidesWith,
					second.shape.CollisionBits, second.shape.CollidesWith) {
					continue
				}
				world := moving.world(second)
				back := pathDelta(second, world).Negate()
				// Two circles are a closed form, cheaper than the path boxes
				// that would reject them. Anything else is not.
				bothCircles := body.shape.Kind == ShapeCircle && second.shape.Kind == ShapeCircle
				if !bothCircles && !box.Intersects(second.box.Merge(second.box.Offset(back))) {
					continue
				}
				// A circle's relative path is its centre's, which needs no
				// run: it is relativeTo's, written out, so the stacked moving
				// Sensors a circle Sensor meets every tick cost it a closed
				// form each and nothing more.
				var hit Hit
				var ok bool
				if body.shape.Kind == ShapeCircle {
					delta := path.delta.Add(back)
					from := path.world[0].Sub(delta)
					hit, ok = probeWorld(from, from.Add(delta), body.shape.Radius, second.shape, world)
				} else {
					along := path.relativeTo(&relative, c.mover[2*used:4*used], second, world)
					hit, ok = along.probe(second, world)
				}
				c.partners = append(c.partners, partner{slot: other, hit: hit, ok: ok})
			}
		}
	}
}

// writeStops settles each fast solid Body's earliest stop, its own first Hit
// or a meeting that comes sooner, and writes it: the stopping Contact, the
// Body's copy where it stopped, and its other pairs there. A meeting is written
// once, by whichever of its parties it stops first in the order of the slots,
// and every party it stops shares its Contact and its T.
func (c *Contacts) writeStops(bodies *BodyIndex, statics *StaticIndex, jointed *JointedPairs) {
	moving := &bodies.index
	for i := range c.meetings {
		met := &c.meetings[i]
		for _, party := range [2]int32{met.a, met.b} {
			if first := c.firstOf(party); first != nil && met.hit.T < first.t {
				first.t, first.meeting = met.hit.T, int32(i)
			}
		}
	}
	for i := range c.firsts {
		first := &c.firsts[i]
		if first.t > 1 {
			continue
		}
		body := &moving.entries[first.slot]
		world := moving.world(body)
		delta := pathDelta(body, world)
		var at int32
		if first.meeting < 0 {
			at = c.stopAt(body, first.slot, delta, first.hit, first.other, first.otherSlot, m.Vec2d{})
		} else {
			met := &c.meetings[first.meeting]
			if met.at < 0 {
				a, b := &moving.entries[met.a], &moving.entries[met.b]
				met.at = c.stopAt(a, met.a, pathDelta(a, moving.world(a)),
					met.hit, b, met.b, pathDelta(b, moving.world(b)))
			}
			at = met.at
		}
		c.placeStop(body, first.slot, world, delta, first.t, at)
		c.stops[len(c.stops)-1].met = first.meeting >= 0
		c.stillAtStop(bodies, statics, jointed, body, world, first.slot)
	}
}

// firstOf is the earliest stop being found for the Body at slot, or nil for a
// Body the pass did not test. The run is in the order of the slots.
func (c *Contacts) firstOf(slot int32) *firstStop {
	low, high := 0, len(c.firsts)
	for low < high {
		mid := int(uint(low+high) >> 1)
		if c.firsts[mid].slot < slot {
			low = mid + 1
		} else {
			high = mid
		}
	}
	if low < len(c.firsts) && c.firsts[low].slot == slot {
		return &c.firsts[low]
	}
	return nil
}

// stillAtStop tests a Body just stopped against the statics and the sleepers
// about where it stopped, which the discrete walk would look for about its end
// pose instead. Its pairs with the awake Bodies are left to that walk, which
// meets them in the cells of the Body's path box, where it is listed, and
// tests them where it stopped too.
//
// The discrete walk reaches these pairs again through the statics' and the
// sleepers' cells about the end pose, and finds each one it touches already
// written, so a pair is still written once. Keeping the walk here keeps the discrete walk's own loop what it was
// for a world where nothing is stopped.
func (c *Contacts) stillAtStop(
	bodies *BodyIndex, statics *StaticIndex, jointed *JointedPairs,
	body *entry, world []m.Vec2d, slot int32,
) {
	box := c.stops[len(c.stops)-1].body.box
	if !finiteBB(box) {
		return
	}
	if sleeping := &bodies.sleepers; bodies.sleeping > 0 {
		awake := bodies.awakeSlots()
		left, bottom := sleeping.cell(box.L), sleeping.cell(box.B)
		right, top := sleeping.cell(box.R), sleeping.cell(box.T)
		for i := left; i <= right; i++ {
			for j := bottom; j <= top; j++ {
				for at := sleeping.buckets[sleeping.bucket(i, j)]; at >= 0; at = sleeping.links[at].next {
					other := sleeping.links[at].entry
					second := &sleeping.entries[other]
					if !firstScannedCell(second, i, j, left, bottom) {
						continue
					}
					c.pairStopped(body, world, slot, second, sleeping.world(second), awake+other, jointed)
				}
			}
		}
	}
	still := &statics.index
	left, bottom := still.cell(box.L), still.cell(box.B)
	right, top := still.cell(box.R), still.cell(box.T)
	for i := left; i <= right; i++ {
		for j := bottom; j <= top; j++ {
			for at := still.buckets[still.bucket(i, j)]; at >= 0; at = still.links[at].next {
				second := &still.entries[still.links[at].entry]
				if !firstScannedCell(second, i, j, left, bottom) {
					continue
				}
				c.pairStopped(body, world, slot, second, still.world(second), -1, jointed)
			}
		}
	}
}

// stops reports that a Hit on a target is one that can stop a fast solid
// Body: not a Sensor, strictly inside the tick, and not a pair a Joint holds
// apart.
func stops(body *entry, hit Hit, target *entry, jointed *JointedPairs) bool {
	if target.shape.Sensor || !(hit.T > 0 && hit.T < 1) {
		return false
	}
	return jointed.Len() == 0 || !jointed.Has(body.entity, target.entity)
}

// stopAt writes the stopping Contact for a Body stopped by a Hit of its path,
// d_self, on a target whose own path is d_other, zero for one that is not
// marked, and answers the Contact's place in the tick's entries.
//
// The Contact is the Probed Sensor's form: T the Hit's, one point, the Hit's,
// with a Depth of 0. A face-to-face landing gets its second point next tick
// from the discrete walk; a full manifold here would be a second narrowphase
// for every stop. A and the one normal follow the entry's rule, and r1 and r2
// are taken at the stopping poses, where each party stood at T: the Body is
// where Solve moves a Dynamic one before PreStep reads them. A Hit along a
// relative path is against the target where the tick left it, so its Point is
// moved back along the target's path to where the two met.
func (c *Contacts) stopAt(
	body *entry, slot int32, delta m.Vec2d,
	hit Hit, other *entry, otherSlot int32, otherDelta m.Vec2d,
) int32 {
	stopped := m.Vec2d{X: body.transform.TX, Y: body.transform.TY}.Add(delta.MulS(hit.T - 1))
	back := otherDelta.MulS(hit.T - 1)
	target := m.Vec2d{X: other.transform.TX, Y: other.transform.TY}.Add(back)
	met := hit.Point.Add(back)

	// A is the party that is not Static, otherwise the lower Entity; neither
	// is a Sensor. The Hit's Normal faces the Body, which is B's surface facing
	// A when the Body is A.
	bodyIsA := otherSlot < 0 || body.entity < other.entity

	var made Contact
	var aux contactAux
	point := ContactPoint{Point: met, id: pointID(0, 0)}
	if bodyIsA {
		made.A, made.B = body.entity, other.entity
		made.Normal = hit.Normal
		aux.slotA, aux.slotB = slot, otherSlot
		point.r1, point.r2 = met.Sub(stopped), met.Sub(target)
	} else {
		made.A, made.B = other.entity, body.entity
		made.Normal = hit.Normal.Negate()
		aux.slotA, aux.slotB = otherSlot, slot
		point.r1, point.r2 = met.Sub(target), met.Sub(stopped)
	}
	made.T = hit.T
	made.Count = 1
	made.Points[0] = point
	made.Friction = body.shape.Friction * other.shape.Friction
	made.Restitution = body.shape.Restitution * other.shape.Restitution

	at, wasTouching := c.prevLookup.find(made.A, made.B)
	c.carry(&made, at, wasTouching)
	written := int32(len(c.entries))
	c.append(made, aux)
	return written
}

// placeStop records a Body's stop at t, by the Contact at at, with the Body's
// copy placed there: its path, delta, taken back by the part of the tick that
// was left.
func (c *Contacts) placeStop(body *entry, slot int32, world []m.Vec2d, delta m.Vec2d, t float64, at int32) {
	offset := delta.MulS(t - 1)

	// The copy at T. Only the points of the world cache move; the normals a
	// segment and a Polygon keep after them do not, the Shape not turning.
	placed := *body
	placed.path = false
	placed.transform.TX, placed.transform.TY = placed.transform.TX+offset.X, placed.transform.TY+offset.Y
	placed.box = placed.box.Offset(offset)
	start := len(c.stopWorld)
	points := cachedPoints(body.shape.Kind, len(world))
	for i, v := range world {
		if i < points {
			v = v.Add(offset)
		}
		c.stopWorld = append(c.stopWorld, v)
	}
	c.stops = append(c.stops, stop{
		at:    at,
		slot:  slot,
		world: int32(start),
		t:     t,
		body:  placed,
	})
}

// stopOf is the stop the path pass made for the Body index entry at slot, or
// nil when it made none. The run is in the order of the awake grid's slots, so
// it is a binary search, and only an entry with the path bit is asked.
func (c *Contacts) stopOf(e *entry, slot int32) *stop {
	if !e.path || e.shape.Sensor || slot < 0 || len(c.stops) == 0 {
		return nil
	}
	low, high := 0, len(c.stops)
	for low < high {
		mid := int(uint(low+high) >> 1)
		if c.stops[mid].slot < slot {
			low = mid + 1
		} else {
			high = mid
		}
	}
	if low < len(c.stops) && c.stops[low].slot == slot {
		return &c.stops[low]
	}
	return nil
}

// stopWorldOf is a stopped Body's copy's world cache.
func (c *Contacts) stopWorldOf(s *stop) []m.Vec2d {
	return c.stopWorld[s.world : s.world+s.body.worldLen]
}

// pairStopped is the discrete walk's pair for a pair either of whose parties
// is a marked solid Body, and reports whether it dealt with it. A stopped Body
// is tested where it stopped, through its copy, and the entry the pair makes
// reports that T; the stopping pair itself is already written, since the pair
// table holds it and a pair is written once a tick. A pair with no stopped
// party is not dealt with here, and the caller tests it as it stands.
//
// The copy carries no path bit, so the pair it is handed back to tests it as
// an ordinary entry.
func (c *Contacts) pairStopped(
	first *entry, worldFirst []m.Vec2d, firstSlot int32,
	second *entry, worldSecond []m.Vec2d, secondSlot int32,
	jointed *JointedPairs,
) bool {
	firstStop, secondStop := c.stopOf(first, firstSlot), c.stopOf(second, secondSlot)
	if firstStop == nil && secondStop == nil {
		return false
	}
	if _, written := c.lookup.find(first.entity, second.entity); written {
		return true
	}
	t := 1.0
	if firstStop != nil {
		first, worldFirst, t = &firstStop.body, c.stopWorldOf(firstStop), firstStop.t
	}
	if secondStop != nil {
		second, worldSecond, t = &secondStop.body, c.stopWorldOf(secondStop), min(t, secondStop.t)
	}
	before := len(c.entries)
	c.pair(first, worldFirst, firstSlot, second, worldSecond, secondSlot, jointed)
	if len(c.entries) > before {
		c.entries[before].T = t
	}
	return true
}

// backToStops is the first thing Solve does: every Dynamic body the path pass
// stopped is moved to where it stood when it was stopped, Previous + T·d along
// its Position's chord, before anything reads its Position or its Contacts'
// offsets. Only Solve reads Dynamic, so only Solve decides which side moves.
//
// A side that is not Dynamic keeps its end pose: a Kinematic body is never
// stopped and never pushed, so a Dynamic body it meets is carried along with
// it by the movement it still has after T:
//
//   - a Dynamic body stopped by a meeting with a marked side that is not
//     Dynamic goes to Previous + T·d_self + (1 − T)·d_other, which is where it
//     touches that side where the tick left it;
//   - a Dynamic body a stopped side that is not Dynamic met on its own path is
//     carried from its end pose by (1 − T)·d_other. It was taken where the
//     tick left it, so it has no stop of its own, and it is carried by every
//     such side that met it;
//   - a Dynamic body stopped by a target it met on its own path is not
//     carried: the target was taken where the tick left it already, so its
//     movement is in T.
//
// A Kinematic body against a Static one moves neither side, and so does a stop
// whose Contact a filter dropped or ignored: a stop the app refused stops
// nothing. Each Body has at most one stop, its own earliest, so each stops at
// its own earliest T. The Angle is not moved back, the path having been tested
// at the end angle.
func (c *Contacts) backToStops(dynamics *ecs.Get[Dynamic], places *ecs.Set[Position]) {
	for i := range c.stops {
		s := &c.stops[i]
		entry := &c.entries[s.at]
		if entry.Dropped() || entry.Ignored() {
			continue
		}
		self := s.body.entity
		other, otherSlot := entry.B, c.aux[s.at].slotB
		if other == self {
			other, otherSlot = entry.A, c.aux[s.at].slotA
		}
		if _, dynamic := dynamics.Of(self); dynamic {
			place, ok := places.Ref(self)
			if !ok {
				continue
			}
			place.Current = place.Previous.Add(place.Current.Sub(place.Previous).MulS(s.t))
			if !s.met {
				continue
			}
			if _, alsoDynamic := dynamics.Of(other); alsoDynamic {
				continue
			}
			if carrier, ok := places.Of(other); ok {
				place.Current = place.Current.Add(carrier.Current.Sub(carrier.Previous).MulS(1 - s.t))
			}
			continue
		}
		// A side that is not Dynamic keeps its end pose, and carries a
		// Dynamic body its own path met. A Static target is not carried.
		if s.met || otherSlot < 0 {
			continue
		}
		if _, carried := dynamics.Of(other); !carried {
			continue
		}
		carrier, ok := places.Of(self)
		if !ok {
			continue
		}
		if target, ok := places.Ref(other); ok {
			target.Current = target.Current.Add(carrier.Current.Sub(carrier.Previous).MulS(1 - s.t))
		}
	}
}

// bodyPath is one fast solid Body's path through the tick, as the rule that
// tells a Hit its path enters from a seam reads it: its own path, or its path
// relative to another marked Body's, against that Body where the tick left it.
type bodyPath struct {
	body *entry
	// world is the Body's world cache at its end pose, from where a circle's
	// centre starts, and delta the path.
	world []m.Vec2d
	from  m.Vec2d
	delta m.Vec2d
	// mover is the Probed Shape of anything but a circle.
	mover *shapeProbe
	// rests is how deep the Body stands in what it already touches, measured
	// the first time a Hit asks for it; below zero, it has not been. Only the
	// Body's own path keeps it.
	rests float64
	slop  float64
}

// relativeTo is the Body's path relative to a marked Body's, other, whose
// world cache is at its end pose: the Body moved by d_self − d_other and ending
// where the tick left it, against other where the tick left it, which is the
// pair's relative motion from both start poses shifted by other's whole path.
// A T along it is the pair's one T. Anything but a circle is Probed in probe,
// built over runs, which must hold twice the Body's world cache.
func (path *bodyPath) relativeTo(probe *shapeProbe, runs []m.Vec2d, other *entry, world []m.Vec2d) bodyPath {
	body := path.body
	delta := path.delta.Sub(pathDelta(other, world))
	along := bodyPath{body: body, world: path.world, delta: delta, mover: probe, rests: -1, slop: path.slop}
	if body.shape.Kind == ShapeCircle {
		along.from = path.world[0].Sub(delta)
	} else {
		*probe = shapeProbeBack(runs, body.shape, path.world, body.box, delta)
	}
	return along
}

// probe is the path test of the path against one target where the tick left
// it.
func (path *bodyPath) probe(target *entry, world []m.Vec2d) (Hit, bool) {
	if body := path.body; body.shape.Kind == ShapeCircle {
		return probeWorld(path.from, path.from.Add(path.delta), body.shape.Radius, target.shape, world)
	}
	return path.mover.against(target.shape, target.box, world)
}

// enters reports that the Body's path enters the target of a Hit, which is
// what stops it, as opposed to meeting a seam in a surface it slides along,
// which the discrete walk resolves on the right side and which stops nothing
// (continuous-collision.md § A seam stops nothing).
//
// A resting Body stands a few millimetres into the surface it slides on, so
// its path meets the next tile's leading corner or face, which it did not
// touch where the tick began. Two tests tell that meeting apart, and a Hit
// stops the Body only when it passes both:
//
//   - the gate, along the Hit's normal. The Body must close on the target, in
//     the tick, by at least its own minimum extent along the normal it meets
//     it by. Closing by less, it cannot get past the surface within the tick,
//     and the discrete walk resolves it from the side it came from, which is
//     the gate's own argument, made along the normal. A Body landing on a
//     floor at a slant closes on it by its fall, not by its speed. It is one
//     product, so it goes first;
//   - the depth. The target must reach into the band the Body sweeps deeper
//     than the Body already stands in what it touches, plus the Slop: a tile
//     laid flush with the one underfoot reaches no deeper than that one does.
//     It is the Body shrunk by that depth, Probed against the target alone
//     along its path lengthened by the same depth, so that the band keeps its
//     length and a target the path meets just before it ends is still
//     reached.
//
// path is the path the Hit was found along, the Body's own or its path
// relative to a marked target's, and both tests read it; own is the Body's
// own path, which keeps the depth it rests at.
func (c *Contacts) enters(
	path, own *bodyPath, bodies *BodyIndex, statics *StaticIndex, split int,
	hit Hit, target *entry, world []m.Vec2d,
) bool {
	body := path.body
	closing := -path.delta.Dot(hit.Normal)
	if !(closing >= MinimumExtent(body.shape) && closing > 0) {
		return false
	}

	if own.rests < 0 {
		own.rests = c.restingDepth(own, bodies, statics, split)
	}
	depth := own.rests + own.slop
	past := 1 + depth/path.delta.Length()
	if body.shape.Kind == ShapeCircle {
		to := path.from.Add(path.delta.MulS(past))
		_, ok := probeWorld(path.from, to, max(0, body.shape.Radius-depth), target.shape, world)
		return ok
	}
	return path.mover.reaches(target.shape, target.box, world, depth, hit.T, past)
}

// restingDepth is how deep the Body stands, where the tick began, in the
// solid targets it already touches there: the path test's Hits at T = 0, which
// sit at the front of each run, and the marked partners its path starts
// inside, along the pair's relative motion. Each is taken with how much deeper
// the path carries the Body into that surface by the tick's end, along the
// surface's normal, so that a Body settling onto the floor it slides along is
// measured where it ends.
func (c *Contacts) restingDepth(path *bodyPath, bodies *BodyIndex, statics *StaticIndex, split int) float64 {
	deepest := 0.0
	for at := 0; at < split && c.probes[at].T == 0; at++ {
		within, found := bodies.entryAt(c.probeSlots[at])
		if found.path {
			// Marked: where the tick left it is not where the Body started
			// beside it. The partners below say that.
			continue
		}
		deepest = max(deepest, path.restsIn(c.probes[at], found, within.world(found)))
	}
	for at := split; at < len(c.probes) && c.probes[at].T == 0; at++ {
		if found, _, ok := statics.lookup(c.probes[at].Entity); ok {
			deepest = max(deepest, path.restsIn(c.probes[at], found, statics.index.world(found)))
		}
	}
	if len(c.partners) > 0 {
		moving := &bodies.index
		used := len(path.world)
		var relative shapeProbe
		for _, p := range c.partners {
			if !p.ok || p.hit.T != 0 {
				continue
			}
			other := &moving.entries[p.slot]
			world := moving.world(other)
			along := path.relativeTo(&relative, c.mover[4*used:6*used], other, world)
			deepest = max(deepest, along.restsIn(p.hit, other, world))
		}
	}
	return deepest
}

// restsIn is how deep the Body stands in one target it touches where the path
// starts, with how much further the path carries it into that surface, or 0
// for a Sensor, which holds nothing up.
func (path *bodyPath) restsIn(hit Hit, target *entry, world []m.Vec2d) float64 {
	if target.shape.Sensor {
		return 0
	}
	var depth float64
	if body := path.body; body.shape.Kind == ShapeCircle {
		_, distance, _ := pointQueryWorld(path.from, target.shape, world)
		depth = body.shape.Radius - distance
	} else {
		depth = path.mover.depthAt(target.shape, target.box, world)
	}
	return depth + max(0, -path.delta.Dot(hit.Normal))
}

// depthAt is how deeply the Probed Shape overlaps a target where its path
// starts: GJK's signed distance, which EPA measures inside an overlap.
func (probe *shapeProbe) depthAt(target Shape, box BB, world []m.Vec2d) float64 {
	probe.place(0)
	ctx := support{worldA: probe.moved, worldB: world, kindA: probe.shape.Kind, kindB: target.Kind}
	points := gjk(ctx, probe.centre, box.Centre(), m.Vec2d{Y: 1}, 0)
	return probe.shape.Radius + target.Radius - points.d
}

// reaches reports that the Probed Shape, advanced from fraction from of its
// path up to fraction past, overlaps the target by depth or more. It is the
// same Newton advance as against's, on the signed distance plus depth: the
// signed distance stays convex along the path inside an overlap, where EPA
// measures it, so the advance still never steps past the depth.
func (probe *shapeProbe) reaches(target Shape, box BB, world []m.Vec2d, depth, from, past float64) bool {
	ctx := support{worldA: probe.moved, worldB: world, kindA: probe.shape.Kind, kindB: target.Kind}
	radii := probe.shape.Radius + target.Radius
	centre := box.Centre()

	t := from
	var cached uint32
	for iteration := 0; ; iteration++ {
		offset := probe.place(t)
		points := gjk(ctx, probe.centre.Add(offset), centre, m.Vec2d{Y: 1}, cached)
		cached = points.id
		short := points.d - radii + depth
		closing := probe.delta.Dot(points.n)

		switch {
		case short <= probeShapeTolerance, iteration == maxProbeShapeIterations:
			return true
		case !(closing > 0):
			return false
		}

		t += short / closing
		if t > past {
			return false
		}
	}
}
