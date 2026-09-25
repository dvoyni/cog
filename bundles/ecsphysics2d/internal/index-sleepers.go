package internal

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/libs/m"
)

// The Body index's second grid, the one its Sleeping bodies are kept in, and
// the three queries answering over both grids. Why a sleeper has a grid of its
// own and not a place in the static index is on BodyIndex; what falls asleep
// and when is islands.go.

// Probe is the nearest Hit over both grids, the awake Bodies' and the Sleeping
// ones', so its answer does not depend on what sleeps.
func (idx *BodyIndex) Probe(
	from, to m.Vec2d, radius float64,
	bits, collidesWith uint32, exclude ecs.Entity,
) (Hit, bool) {
	hit, ok := idx.index.Probe(from, to, radius, bits, collidesWith, exclude)
	if idx.sleeping == 0 {
		return hit, ok
	}
	sleeper, found := idx.sleepers.Probe(from, to, radius, bits, collidesWith, exclude)
	if found && (!ok || sleeper.T < hit.T) {
		return sleeper, true
	}
	return hit, ok
}

// ProbeAll appends every Hit along the Probe to dst, ordered by T, over both
// grids as one ordered run.
func (idx *BodyIndex) ProbeAll(
	dst []Hit, from, to m.Vec2d, radius float64,
	bits, collidesWith uint32, exclude ecs.Entity,
) []Hit {
	start := len(dst)
	dst, _, _ = idx.probeWalk(dst, start, nil, 0, false, from, to, radius, bits, collidesWith, exclude, nil)
	if idx.sleeping == 0 {
		return dst
	}
	dst, _, _ = idx.sleepers.probeWalk(dst, start, nil, 0, false, from, to, radius, bits, collidesWith, exclude, nil)
	return dst
}

// ProbeWith is the nearest Hit of a moving Shape over both grids, so its
// answer does not depend on what sleeps.
func (idx *BodyIndex) ProbeWith(
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
	if idx.sleeping == 0 {
		return hit, ok
	}
	_, sleeper, found := idx.sleepers.shapeWalk(nil, 0, true, &mover, bits, collidesWith, exclude)
	if found && (!ok || sleeper.T < hit.T) {
		return sleeper, true
	}
	return hit, ok
}

// ProbeAllWith appends every Hit of a moving Shape to dst, ordered by T, over
// both grids as one ordered run.
func (idx *BodyIndex) ProbeAllWith(
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
	start := len(dst)
	dst, _, _ = idx.shapeWalk(dst, start, false, &mover, bits, collidesWith, exclude)
	if idx.sleeping == 0 {
		return dst
	}
	dst, _, _ = idx.sleepers.shapeWalk(dst, start, false, &mover, bits, collidesWith, exclude)
	return dst
}

// Overlap appends every Entity the placed Shape touches to dst, unordered, over
// both grids.
func (idx *BodyIndex) Overlap(
	dst []ecs.Entity, shape Shape, at m.Vec2d, angle float64, verts []m.Vec2d,
	bits, collidesWith uint32, exclude ecs.Entity,
) []ecs.Entity {
	dst = idx.index.Overlap(dst, shape, at, angle, verts, bits, collidesWith, exclude)
	if idx.sleeping == 0 {
		return dst
	}
	return idx.sleepers.Overlap(dst, shape, at, angle, verts, bits, collidesWith, exclude)
}

// probeAllSlots is ProbeAll over both grids with each Hit's solver slot kept
// beside it, a sleeper's numbered after the awake grid's. It is the swept
// Sensor's Probe of the Body index.
func (idx *BodyIndex) probeAllSlots(
	dst []Hit, slots *[]int32, from, to m.Vec2d, radius float64,
	bits, collidesWith uint32, exclude ecs.Entity,
) []Hit {
	start := len(dst)
	dst, _, _ = idx.probeWalk(dst, start, slots, 0, false, from, to, radius, bits, collidesWith, exclude, nil)
	if idx.sleeping == 0 {
		return dst
	}
	dst, _, _ = idx.sleepers.probeWalk(dst, start, slots, idx.awakeSlots(), false,
		from, to, radius, bits, collidesWith, exclude, nil)
	return dst
}

// awakeSlots is how many slots the awake grid numbers, which is where the
// sleepers' grid's numbering starts in the solver's slot table.
func (idx *BodyIndex) awakeSlots() int32 { return int32(len(idx.entries)) }

// entryAt is the entry behind a solver slot, in whichever grid numbers it.
func (idx *BodyIndex) entryAt(slot int32) (*index, *entry) {
	if awake := idx.awakeSlots(); slot >= awake {
		return &idx.sleepers, &idx.sleepers.entries[slot-awake]
	}
	return &idx.index, &idx.entries[slot]
}

// dropSleeper takes one entry out of the sleepers' grid and frees its slot.
func (idx *BodyIndex) dropSleeper(slot int32) {
	idx.sleepers.drop(slot)
	idx.sleepers.freeEntries = append(idx.sleepers.freeEntries, slot)
	idx.sleeping--
}

// InsertSleeper puts a Body whose Island has fallen asleep into the Body
// index's sleepers' grid, placed where it sleeps. Index calls it once, on the
// tick after the Body gained the Sleeping Tag, and the Body stays there until
// its Island wakes: the grid is not rebuilt every tick.
//
// It is a function of this package rather than a method, so that it is not on
// the Resource an app reads: an app asks the index, it does not keep it.
func InsertSleeper(idx *BodyIndex, entity ecs.Entity, shape Shape, at m.Vec2d, angle float64, verts []m.Vec2d) {
	if entity == ecs.NoEntity {
		return
	}
	slot := idx.sleepers.allocEntry()
	idx.sleepers.place(slot, entity, shape, at, angle, verts)
	idx.sleeping++
}

// RemoveSleepers takes every Body named in woken out of the sleepers' grid in
// one pass over it, whatever their number, and does nothing for an Entity the
// grid does not hold. Index calls it with the Bodies whose Sleeping Tag went
// this tick, the ones that woke and the ones despawned while asleep.
func RemoveSleepers(idx *BodyIndex, woken []ecs.Entity) {
	if len(woken) == 0 || idx.sleeping == 0 {
		return
	}
	idx.woken.reset()
	for _, e := range woken {
		idx.woken.put(e, 0)
	}
	for slot := range idx.sleepers.entries {
		e := &idx.sleepers.entries[slot]
		if !e.live {
			continue
		}
		if _, ok := idx.woken.lookup(e.entity); ok {
			idx.dropSleeper(int32(slot))
		}
	}
}

// Sleepers is how many Shapes the sleepers' grid holds.
func Sleepers(idx *BodyIndex) int { return idx.sleeping }

// shrink cuts both grids' areas, the awake grid's and the sleepers', and
// reports the bytes each let go, summed.
func (idx *BodyIndex) shrink(request ShrinkRequest) ShrinkResponse {
	released := idx.index.shrink(request)
	released.add(idx.sleepers.shrink(request))
	if !request.KeepScratch {
		before := idx.woken.bytes()
		idx.woken = entityTable{}
		released.Scratch += before
	}
	return released
}
