package internal

import "github.com/dvoyni/cog/libs/m"

// The manifold a collision arm fills in: at most two points, the normal they
// share, and the ids that let the next tick match a point to the one whose
// Impulse it carries. It is the stack value every arm in collide.go returns,
// and it becomes a Contact in contacts-detect.go.

type touchingPoint struct {
	p1, p2 m.Vec2d
	depth  float64
	id     uint32
}

// touching is what a closed form finds: cp's CollisionInfo as a value, with the
// two surface points cp's PushContact writes beside the normal and the overlap
// Penetration already reported.
//
// normal points from the first Shape towards the second, which is cp's own
// sense. p1 is on the first Shape's surface and p2 on the second's, both in
// world space, exactly as cp pushes them; depth is how far along the normal one
// would move to part them, and is −(p2 − p1)·normal by construction. id is the
// point's identity across ticks, two vertex indices packed into a uint32 —
// exact, where cp mixes shape pointers into a hash and admits false positives.
//
// The count is 1 for both closed forms. The array is two wide because the
// Polygon manifolds that arrive with GJK push two, and the Contact entry the
// caller fills is already laid out for them.
type touching struct {
	normal m.Vec2d
	points [2]touchingPoint
	count  int
	// gjkId is cp's collisionId: the simplex GJK converged on, which the next
	// tick's GJK starts from. It is zero for the two closed forms, which have
	// no simplex, and is always in the kind order the dispatch sorted the pair
	// into, so a caller passing its two Shapes the other way round hands back
	// an id the next tick can still use.
	gjkId uint32
}

// push adds one point to a manifold, up to cp's MAX_CONTACTS_PER_ARBITER of
// two, whatever the vertex count.
func (t *touching) push(p1, p2 m.Vec2d, depth float64, id uint32) {
	if t.count >= len(t.points) {
		return
	}
	t.points[t.count] = touchingPoint{p1: p1, p2: p2, depth: depth, id: id}
	t.count++
}

// pointID packs two vertex indices into one uint32. The closed forms have no
// vertices to name and pass 0, which is the id cp's own hash carries for them.
func pointID(first, second uint32) uint32 { return first<<16 | second }
