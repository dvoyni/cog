package types

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/libs/m"
)

// The path pass, and the stop it makes: continuous collision
// (continuous-collision.md § Detect: one path pass, § Solve: back to T).
//
// Index marks an entry with the one path bit when Detect is to test it along
// its path through the tick: a moving circle Sensor, and a solid Body that
// moved at least its own minimum extent. This pass runs once over the marked
// entries, before the discrete walk, and the Sensor flag picks what a path
// test keeps. A Sensor keeps every Hit, in contacts-sensors.go. A solid Body
// keeps its first Hit that can stop it, and is stopped there: the Contact it
// writes carries the Hit's T, and Solve moves a Dynamic body back to where it
// stood at T before it solves anything.
//
// Every target is taken where the tick left it. A target that moved into the
// path during the tick counts as already there, which is the ghost Hit the
// specification names as a limit.

// stop is one fast solid Body the path pass stopped: its stopping Contact, and
// a copy of its index entry placed where it stopped, which is what the
// discrete walk tests its other pairs against.
type stop struct {
	// at is the stopping Contact's place in the tick's entries.
	at int32
	// slot is the Body's slot in the Body index's awake grid, which is the
	// order the pass visits Bodies in and so the order of the run.
	slot int32
	// world is where the copy's world cache starts in stopWorld; its length
	// is the entry's own.
	world int32
	// t is how far through the tick the Body was when it was stopped.
	t float64
	// body is the Body's entry, its transform, box and world cache moved back
	// along its path to t, and its path bit cleared. Its grid cells are the
	// entry's own, so it is never listed or walked, only tested.
	body entry
}

// pathPass is Detect's one pass over the marked entries of the Body index: a
// moving Sensor is swept, and a fast solid Body is stopped at the first thing
// its path meets.
//
// It runs before the discrete walk, so what it writes sits at the front of the
// list: a Sensor's entries together and in order of T, and each stopping
// Contact.
//
// slop is the Slop setting, how far two Shapes may overlap and be left alone,
// which is how much deeper than a fast Body already rests a target on its
// path may reach without stopping it.
func (c *Contacts) pathPass(bodies *BodyIndex, statics *StaticIndex, jointed *JointedPairs, slop float64) {
	moving := &bodies.index
	for slot := range moving.entries {
		marked := &moving.entries[slot]
		if !marked.path || !marked.live {
			continue
		}
		if marked.shape.Sensor {
			c.sweepSensor(bodies, statics, marked, slot)
			continue
		}
		c.stopBody(bodies, statics, jointed, marked, int32(slot), slop)
	}
}

// stopBody is the solid half of the path pass: one fast solid Body, at slot,
// Probed along its path through both indices, and stopped at its first Hit
// that can stop it.
//
// The Shape is held at its end angle, so the path is its end pose moved back
// along the Position's chord: a circle's centre by Probe, anything else by the
// swept convex test over its own world cache, moved back. The Probe is a
// find-all one, because the first Hit may not be one that stops: a Hit is
// passed over when
//
//   - the target is a Sensor, which never stops a Body;
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
func (c *Contacts) stopBody(
	bodies *BodyIndex, statics *StaticIndex, jointed *JointedPairs, body *entry, slot int32, slop float64,
) {
	moving := &bodies.index
	world := moving.world(body)
	bits, collidesWith := body.shape.CollisionBits, body.shape.CollidesWith

	c.probeSlots = c.probeSlots[:0]
	var delta m.Vec2d
	var split int
	var mover shapeProbe
	if body.shape.Kind == ShapeCircle {
		from, to, radius := body.previousCentre, world[0], body.shape.Radius
		delta = to.Sub(from)
		c.probes = bodies.probeAllSlots(c.probes[:0], &c.probeSlots,
			from, to, radius, bits, collidesWith, body.entity)
		split = len(c.probes)
		c.probes = statics.ProbeAll(c.probes, from, to, radius, bits, collidesWith, body.entity)
	} else {
		delta = m.Vec2d{X: body.transform.TX, Y: body.transform.TY}.Sub(body.previousCentre)
		if need := 2 * len(world); len(c.mover) < need {
			c.mover = make([]m.Vec2d, need)
		}
		mover = shapeProbeBack(c.mover, body.shape, world, body.box, delta)
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

	// Each run is ordered by T, so the first Hit that stops in each is that
	// run's answer, and the nearer of the two is the stop.
	var other *entry
	var otherSlot int32
	var hit Hit
	path := bodyPath{body: body, world: world, delta: delta, mover: &mover, rests: -1, slop: slop}
	for at := range split {
		candidate := c.probes[at]
		within, found := bodies.entryAt(c.probeSlots[at])
		if stops(body, candidate, found, jointed) &&
			c.enters(&path, bodies, statics, split, candidate, found, within.world(found)) {
			other, otherSlot, hit = found, c.probeSlots[at], candidate
			break
		}
	}
	for _, candidate := range c.probes[split:] {
		if other != nil && candidate.T >= hit.T {
			break
		}
		found, _, ok := statics.lookup(candidate.Entity)
		if ok && stops(body, candidate, found, jointed) &&
			c.enters(&path, bodies, statics, split, candidate, found, statics.index.world(found)) {
			other, otherSlot, hit = found, -1, candidate
			break
		}
	}
	if other == nil {
		return
	}
	c.stopAt(body, slot, world, delta, hit, other, otherSlot)
	c.stillAtStop(bodies, statics, jointed, body, world, slot)
}

// stillAtStop tests a Body just stopped against the statics and the sleepers
// about where it stopped, which the discrete walk would look for about its end
// pose instead. Its pairs with the awake Bodies are left to that walk, which
// meets them in the Body grid's listing and tests them where it stopped too.
//
// The discrete walk reaches these pairs again through the end pose's cells,
// and finds each one it touches already written, so a pair is still written
// once. Keeping the walk here keeps the discrete walk's own loop what it was
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

// stops reports that a Hit on a target is one that stops a fast solid Body.
//
// A target that is marked itself is passed over too, and the pair left to the
// discrete walk, as it was before any Body was stopped. Its end pose is not
// where the Body meets it: the error is its whole movement in the tick, which
// is past its own extent, and the two walks would each stop the other at a
// different T and write the one pair twice. Two marked parties meet along
// their relative motion, which is the next part of the build.
func stops(body *entry, hit Hit, target *entry, jointed *JointedPairs) bool {
	if target.shape.Sensor || target.path || !(hit.T > 0 && hit.T < 1) {
		return false
	}
	return jointed.Len() == 0 || !jointed.Has(body.entity, target.entity)
}

// stopAt writes the stopping Contact for a Body stopped by a Hit, and records
// the stop, with the Body's copy placed where it stopped.
//
// The Contact is the Probed Sensor's form: T the Hit's, one point, the Hit's,
// with a Depth of 0. A face-to-face landing gets its second point next tick
// from the discrete walk; a full manifold here would be a second narrowphase
// for every stop. A and the one normal follow the entry's rule, and r1 and r2
// are taken at the stopping poses: the Body where it stopped, which is where
// Solve moves a Dynamic one before PreStep reads them, and the target where the
// tick left it.
func (c *Contacts) stopAt(
	body *entry, slot int32, world []m.Vec2d, delta m.Vec2d,
	hit Hit, other *entry, otherSlot int32,
) {
	offset := delta.MulS(hit.T - 1)
	stopped := m.Vec2d{X: body.transform.TX, Y: body.transform.TY}.Add(offset)
	target := m.Vec2d{X: other.transform.TX, Y: other.transform.TY}

	// A is the party that is not Static, otherwise the lower Entity; neither
	// is a Sensor. The Hit's Normal faces the Body, which is B's surface facing
	// A when the Body is A.
	bodyIsA := otherSlot < 0 || body.entity < other.entity

	var made Contact
	var aux contactAux
	point := ContactPoint{Point: hit.Point, id: pointID(0, 0)}
	if bodyIsA {
		made.A, made.B = body.entity, other.entity
		made.Normal = hit.Normal
		aux.slotA, aux.slotB = slot, otherSlot
		point.r1, point.r2 = hit.Point.Sub(stopped), hit.Point.Sub(target)
	} else {
		made.A, made.B = other.entity, body.entity
		made.Normal = hit.Normal.Negate()
		aux.slotA, aux.slotB = otherSlot, slot
		point.r1, point.r2 = hit.Point.Sub(target), hit.Point.Sub(stopped)
	}
	made.T = hit.T
	made.Count = 1
	made.Points[0] = point
	made.Friction = body.shape.Friction * other.shape.Friction
	made.Restitution = body.shape.Restitution * other.shape.Restitution

	at, wasTouching := c.prevLookup.find(made.A, made.B)
	c.carry(&made, at, wasTouching)

	// The copy at T. Only the points of the world cache move; the normals a
	// segment and a Polygon keep after them do not, the Shape not turning.
	placed := *body
	placed.path = false
	placed.transform.TX, placed.transform.TY = stopped.X, stopped.Y
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
		at:    int32(len(c.entries)),
		slot:  slot,
		world: int32(start),
		t:     hit.T,
		body:  placed,
	})
	c.append(made, aux)
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
// offsets.
//
// A side that is not Dynamic keeps its end pose, and so does a Body whose
// stopping Contact a filter dropped or ignored: a stop the app refused stops
// nothing. Each Body has at most one stop, its own path's first Hit, so each
// stops at its own earliest T. The Angle is not moved back, the path having
// been tested at the end angle.
func (c *Contacts) backToStops(dynamics *ecs.Get[Dynamic], places *ecs.Set[Position]) {
	for i := range c.stops {
		s := &c.stops[i]
		entry := &c.entries[s.at]
		if entry.Dropped() || entry.Ignored() {
			continue
		}
		if _, ok := dynamics.Of(s.body.entity); !ok {
			continue
		}
		if place, ok := places.Ref(s.body.entity); ok {
			place.Current = place.Previous.Add(place.Current.Sub(place.Previous).MulS(s.t))
		}
	}
}

// bodyPath is one fast solid Body's path through the tick, as the rule that
// tells a Hit its path enters from a seam reads it.
type bodyPath struct {
	body *entry
	// world is the Body's world cache at its end pose, and delta its path.
	world []m.Vec2d
	delta m.Vec2d
	// mover is the Probed Shape of anything but a circle.
	mover *shapeProbe
	// rests is how deep the Body stands in what it already touches, measured
	// the first time a Hit asks for it; below zero, it has not been.
	rests float64
	slop  float64
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
func (c *Contacts) enters(
	path *bodyPath, bodies *BodyIndex, statics *StaticIndex, split int,
	hit Hit, target *entry, world []m.Vec2d,
) bool {
	body := path.body
	closing := -path.delta.Dot(hit.Normal)
	if !(closing >= MinimumExtent(body.shape) && closing > 0) {
		return false
	}

	if path.rests < 0 {
		path.rests = c.restingDepth(path, bodies, statics, split)
	}
	depth := path.rests + path.slop
	past := 1 + depth/path.delta.Length()
	if body.shape.Kind == ShapeCircle {
		from := body.previousCentre
		to := from.Add(path.world[0].Sub(from).MulS(past))
		_, ok := probeWorld(from, to, max(0, body.shape.Radius-depth), target.shape, world)
		return ok
	}
	return path.mover.reaches(target.shape, target.box, world, depth, hit.T, past)
}

// restingDepth is how deep the Body stands, where the tick began, in the
// solid targets it already touches there: the path test's Hits at T = 0, which
// sit at the front of each run. Each is taken with how much deeper the path
// carries the Body into that surface by the tick's end, along the surface's
// normal, so that a Body settling onto the floor it slides along is measured
// where it ends.
func (c *Contacts) restingDepth(path *bodyPath, bodies *BodyIndex, statics *StaticIndex, split int) float64 {
	deepest := 0.0
	for at := 0; at < split && c.probes[at].T == 0; at++ {
		within, found := bodies.entryAt(c.probeSlots[at])
		deepest = max(deepest, path.restsIn(c.probes[at], found, within.world(found)))
	}
	for at := split; at < len(c.probes) && c.probes[at].T == 0; at++ {
		if found, _, ok := statics.lookup(c.probes[at].Entity); ok {
			deepest = max(deepest, path.restsIn(c.probes[at], found, statics.index.world(found)))
		}
	}
	return deepest
}

// restsIn is how deep the Body stands in one target it touches where the tick
// began, with how much further its path carries it into that surface, or 0
// for a Sensor, which holds nothing up.
func (path *bodyPath) restsIn(hit Hit, target *entry, world []m.Vec2d) float64 {
	if target.shape.Sensor {
		return 0
	}
	var depth float64
	if body := path.body; body.shape.Kind == ShapeCircle {
		_, distance, _ := pointQueryWorld(body.previousCentre, target.shape, world)
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
