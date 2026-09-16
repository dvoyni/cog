package types

import "github.com/dvoyni/cog/libs/m"

// Collide is the whole of Detect: it walks the two indices for touching pairs,
// writes this tick's Contact list, and closes it with the Ended and cached runs
// the previous tick's entries leave behind.
//
// It is cp's ReindexQuery over SpaceCollideShapesFunc, with three departures,
// all of them the ECS layout:
//
//   - the pair goes through the index entries rather than a fresh Query, so a
//     Body spawned after Index joins detection next tick from both sides and is
//     never half-seen;
//   - the Bodies are walked against the Bodies and against the statics
//     separately, because the port has two indices whose locks stay apart, where
//     cp has one dynamic tree queried against itself and one static tree.
//     Static against Static is never tested, which is cp's own rule;
//   - there are no collision handlers to look up, so cp's per-touching-pair
//     lookup-key allocation has nothing to allocate.
//
// jointed is the set of pairs a Joint holds apart, which Index built from the
// Joint Query; persistence is cp's collisionPersistence in ticks.
func Collide(
	contacts *Contacts, bodies *BodyIndex, statics *StaticIndex,
	jointed *JointedPairs, persistence int,
) {
	contacts.beginTick()

	moving, still := &bodies.index, &statics.index
	contacts.maxSlot = int32(len(moving.entries))

	// The swept Sensors first, so that one Sensor's entries sit together and in
	// order of T at the front of the list, ahead of every solid pair — which is
	// the whole of the ordering the specification asks of the current run.
	contacts.sweepSensors(bodies, statics)

	for slot := range moving.entries {
		first := &moving.entries[slot]
		if !first.live || first.worldLen == 0 {
			continue
		}
		// A Body placed at a NaN or an infinity is in no cell of its own index,
		// which the Body walk below inherits for free: list leaves its cell
		// range empty and the loop does not run. The static walk derives its
		// range from the box instead, in the static grid's own cells, so the
		// same refusal is made again here — otherwise a half-infinite box would
		// clamp to the two ends of an int32 grid and scan it whole, looking for
		// a Shape that is listed nowhere.
		if !finiteBB(first.box) {
			continue
		}

		worldFirst := moving.world(first)

		// Body against Body. The pair is tested from the lower slot's side only,
		// and at one cell of the two Shapes' overlap, so a pair sharing several
		// cells is tested once without the index keeping a per-query stamp its
		// concurrent readers could not hold.
		for i := first.left; i <= first.right; i++ {
			for j := first.bottom; j <= first.top; j++ {
				for at := moving.buckets[moving.bucket(i, j)]; at >= 0; at = moving.links[at].next {
					other := moving.links[at].entry
					if other <= int32(slot) {
						continue
					}
					second := &moving.entries[other]
					if !firstScannedCell(second, i, j, first.left, first.bottom) {
						continue
					}
					contacts.pair(
						first, worldFirst, int32(slot),
						second, moving.world(second), other,
						jointed,
					)
				}
			}
		}

		// Body against Static, over the static grid's own cells, which may be a
		// different size from the Body grid's.
		left, bottom := still.cell(first.box.L), still.cell(first.box.B)
		right, top := still.cell(first.box.R), still.cell(first.box.T)
		for i := left; i <= right; i++ {
			for j := bottom; j <= top; j++ {
				for at := still.buckets[still.bucket(i, j)]; at >= 0; at = still.links[at].next {
					second := &still.entries[still.links[at].entry]
					if !firstScannedCell(second, i, j, left, bottom) {
						continue
					}
					contacts.pair(
						first, worldFirst, int32(slot),
						second, still.world(second), -1,
						jointed,
					)
				}
			}
		}
	}

	contacts.endTick(persistence)
}

// world is an entry's run of the index's world-cache slab.
func (idx *index) world(e *entry) []m.Vec2d { return idx.slab[e.world : e.world+e.worldLen] }

// pair is the narrowphase for one candidate pair and, when the two touch, the
// entry it becomes: which party is A, one normal, the material, and whatever the
// previous tick's entry for the same unordered pair carries forward.
//
// firstSlot and secondSlot are the parties' BodyIndex slots, and −1 says the
// party came out of the static index instead.
func (c *Contacts) pair(
	first *entry, worldFirst []m.Vec2d, firstSlot int32,
	second *entry, worldSecond []m.Vec2d, secondSlot int32,
	jointed *JointedPairs,
) {
	// A swept Sensor's entries come from its Probe, whose Hits are a superset
	// of what a discrete test at the tick's end would find, so the discrete
	// pass leaves every pair it is party to alone. Writing the pair twice is
	// also what the pair table forbids: it is inserted at most once a tick and
	// has no replacement path. Only a Body index entry is ever swept, so a
	// Static party answers false without the caller saying which index it came
	// from.
	if first.swept || second.swept {
		return
	}

	// cp's QueryReject, less the one clause the layout answers: a Shape never
	// shares an Entity with another Shape.
	if !collides(
		first.shape.CollisionBits, first.shape.CollidesWith,
		second.shape.CollisionBits, second.shape.CollidesWith,
	) {
		return
	}
	if !first.box.Intersects(second.box) {
		return
	}
	// cp's QueryRejectConstraints, in the one place it can be: the Contact is
	// never created and never reported, which is what cp's QueryReject means —
	// no arbiter, and therefore no Begin. The gate is one branch for a scene
	// with no such Joint.
	if jointed.Len() > 0 && jointed.Has(first.entity, second.entity) {
		return
	}

	// The previous tick's entry is found once, before the narrowphase rather
	// than after it: GJK warm starts from the simplex it holds, where cp reads
	// that off the arbiter it is about to update. Everything else it carries is
	// read below, out of the same lookup.
	at, wasTouching := c.prevLookup.find(first.entity, second.entity)
	var cached uint32
	if wasTouching {
		cached = c.previous[at].gjkId
	}

	touch, ok := collideWorld(
		first.shape, first.transform, worldFirst,
		second.shape, second.transform, worldSecond,
		c.nudged, cached,
	)
	if !ok {
		return
	}

	// A is the Sensor; otherwise the party that is not Static; otherwise the
	// lower Entity. The test order above was the kinds', which the nine-arm
	// switch requires, so the normal flips here once and reads one way for ever
	// after.
	firstIsA := false
	switch {
	case first.shape.Sensor != second.shape.Sensor:
		firstIsA = first.shape.Sensor
	case (firstSlot < 0) != (secondSlot < 0):
		firstIsA = firstSlot >= 0
	default:
		firstIsA = first.entity < second.entity
	}

	var made Contact
	var aux contactAux
	partyA, partyB := first, second
	if firstIsA {
		made.A, made.B = first.entity, second.entity
		made.Normal = touch.normal.Negate()
		aux.slotA, aux.slotB = firstSlot, secondSlot
	} else {
		made.A, made.B = second.entity, first.entity
		made.Normal = touch.normal
		aux.slotA, aux.slotB = secondSlot, firstSlot
		partyA, partyB = second, first
	}

	positionA := m.Vec2d{X: partyA.transform.TX, Y: partyA.transform.TY}
	positionB := m.Vec2d{X: partyB.transform.TX, Y: partyB.transform.TY}

	made.Count = uint8(touch.count)
	made.gjkId = touch.gjkId
	made.T = 1
	made.Sensor = first.shape.Sensor || second.shape.Sensor
	// cp's combination rules, both plain products, so neither depends on which
	// party ended up A. The material never carries across a tick: it is
	// recomputed from the two Shapes every tick as cp does, which is what makes
	// a filter's edit of it last exactly one tick.
	made.Friction = first.shape.Friction * second.shape.Friction
	made.Restitution = first.shape.Restitution * second.shape.Restitution

	for i := range touch.count {
		point := touch.points[i]
		// r1 is the offset on A and r2 the offset on B, so the two swap with the
		// parties. cp converts its absolute push into these offsets in Update;
		// the port does it here, detection being the only place that holds the
		// positions.
		onA, onB := point.p1, point.p2
		if !firstIsA {
			onA, onB = point.p2, point.p1
		}
		made.Points[i] = ContactPoint{
			Point: onB,
			Depth: point.depth,
			r1:    onA.Sub(positionA),
			r2:    onB.Sub(positionB),
			id:    point.id,
		}
	}

	c.carry(&made, at, wasTouching)
	c.append(made, aux)
}

// carry is cp's Arbiter.Update over the previous tick's entry for the same
// unordered pair, found at slot at: the phase, the Impulses matched point by
// point, and an ignore that has not ended yet.
//
// The simplex the entry also carries has already been read, by the narrowphase
// above, and this tick's GJK has converged on a fresher one — so nothing copies
// it forward here.
func (c *Contacts) carry(made *Contact, at int32, found bool) {
	if !found {
		made.Phase = PhaseBegan
		return
	}
	old := &c.previous[at]
	c.prevAux[at].matched = true

	if old.phased() {
		made.Phase = PhaseContinuing
	} else {
		// A pair the previous tick did not show the app touching begins again,
		// which is where the port and cp differ by one call: cp does not call
		// Begin a second time for a pair its own Begin callback refused. Only a
		// drop lands here, and only because a drop is for one tick and the
		// filter has just decided again.
		made.Phase = PhaseBegan
	}
	if old.Phase != PhaseEnded && old.Ignored() {
		// The ignore runs until the pair comes apart, and coming apart is
		// missing one tick — which is exactly an Ended entry in between.
		made.Ignore()
	}

	// cp matches points by a hash mixed from shape pointers and vertex indices,
	// and carries the comment that it could trigger false positives. A and B
	// already fix the two Shapes here, so the ids are exact and the match is
	// too. A revival out of the cached run carries its Impulses the same way,
	// which is cp's CACHED to FIRST_COLLISION.
	for i := range int(made.Count) {
		for j := range int(old.Count) {
			if made.Points[i].id == old.Points[j].id {
				made.Points[i].NormalImpulse = old.Points[j].NormalImpulse
				made.Points[i].TangentImpulse = old.Points[j].TangentImpulse
			}
		}
	}
}
