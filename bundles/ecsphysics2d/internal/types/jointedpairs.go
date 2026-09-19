package types

import "github.com/dvoyni/cog/bundles/ecs"

// JointedPairs is the set of Entity pairs whose Joint says the two Bodies do
// not collide. Index builds it from the Joint Query and Detect checks it, after
// the bit filter and the bounding-box test, so the Contact is never created and
// never reported — which is cp's semantics, its QueryReject meaning no arbiter
// and therefore no Begin.
//
// cp walks the Body's intrusive constraint list per candidate pair. The port
// has no such list and will not grow one: Index already drains the static hooks
// and rebuilds BodyIndex, so it also walks the Joints and fills this, using the
// open-addressed pair map the Contact list already needs. Detect's check is
// gated on the set being empty, so a scene with no such Joint pays one branch.
//
// Handing it to the app's relationship filter was put and rejected: a ragdoll
// is unusable without it, so making every app that spawns a pin Joint write a
// filter System and its own pair-lookup structure is a real regression against
// the destination's "out of the box". The flag is also not a game rule about a
// particular pair; it is a property of the Joint, which is plugin data.
type JointedPairs struct{ pairs pairTable }

// NewJointedPairs is an empty set.
func NewJointedPairs() *JointedPairs { return &JointedPairs{} }

// Len is how many pairs the set holds, and is what gates the check in Detect.
func (j *JointedPairs) Len() int { return j.pairs.used }

// Clear empties the set, keeping the memory it has grown so that refilling it
// each tick allocates nothing.
func (j *JointedPairs) Clear() { j.pairs.clear() }

// Add records that the two Entities do not collide. Adding a pair twice is
// harmless and is what two Joints between the same two Bodies do, so the pair
// is looked up before it is inserted: the table beside it has no replacement
// path, a Contact pair being inserted at most once a tick.
func (j *JointedPairs) Add(a, b ecs.Entity) {
	if _, found := j.pairs.find(a, b); found {
		return
	}
	j.pairs.insert(a, b, 0)
}

// Has reports that the two Entities are held apart by a Joint.
func (j *JointedPairs) Has(a, b ecs.Entity) bool {
	_, found := j.pairs.find(a, b)
	return found
}
