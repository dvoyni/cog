package types

import (
	"math"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/libs/m"
)

// Phase is where a Contact is in its life: it Began this tick, it is
// Continuing from the last one, or it Ended and this is the only tick that says
// so.
//
// Phases are always on, and are compared against what survived the previous
// tick's filters, so a reacting System always sees a pair begin, continue and
// end in that order however a filter changes its mind between ticks.
type Phase uint8

const (
	// PhaseBegan is a pair that was not touching, as the list reported it, on
	// the previous tick.
	PhaseBegan Phase = iota
	// PhaseContinuing is a pair that was reported touching on the previous tick.
	PhaseContinuing
	// PhaseEnded is a pair that is no longer touching, reported for one tick.
	// The entry keeps the previous tick's geometry and carries no reason —
	// separation, a despawned party, a removed Shape and a filter's drop all
	// read the same — so it may name an Entity a Store no longer holds.
	//
	// This departs from cp, whose Count() returns 0 once an arbiter is CACHED,
	// so its Separate callback sees no points at all.
	PhaseEnded
)

// The two marks a filter may leave on an entry. Nothing is ever deleted or
// moved: a reacting System would otherwise see a Contact begin twice without
// ending, and every deletion would shift the slice under the other filters.
const (
	// flagDropped takes the pair out of this tick's solution. A dropped
	// Continuing entry becomes Ended, so reacting Systems see the end; a dropped
	// Began entry is one nobody saw begin, and comes back as Began next tick,
	// where cp does not call Begin again.
	flagDropped uint8 = 1 << iota
	// flagIgnored is cp's arb.Ignore: the pair is skipped until it comes apart,
	// which is what a one-way platform needs. It crosses ticks, so while the
	// pair keeps touching its entry arrives already marked, and it ends when the
	// pair misses one tick.
	flagIgnored
)

// ContactPoint is one point of a Contact, 120 bytes: cp's Contact struct with
// the two fields the ECS layout forces added to it.
//
// cp derives the public contact point as r1 + bodyA.p, because its arbiter
// holds a *Body. This entry holds an ecs.Entity, so a method on it cannot reach
// a position: Point and Depth are stored, and r1 and r2 sit beside them. That
// is 64 B a pair of cp's own redundancy, kept deliberately — re-deriving r1 and
// r2 would cost two vector subtractions a point inside the impulse loop, ten
// iterations a tick.
//
// NormalImpulse and TangentImpulse are cp's jnAcc and jtAcc and are the only
// numbers here that cross a tick; everything from nMass down is scratch PreStep
// rewrites every tick.
type ContactPoint struct {
	// Point is where the pair touches, on B's surface, in world space.
	Point m.Vec2d
	// Depth is how deeply the two overlap at this point, in metres. PreStep
	// reads it rather than recomputing cp's dist: detection ran at the same Body
	// positions, the step integrating positions before it detects, so the two
	// are equal by construction.
	Depth float64
	// NormalImpulse is cp's jnAcc, the accumulated impulse along the Normal. It
	// carries across ticks, which is what warm starting spends. Before Solve it
	// is the previous tick's and after Solve it is this tick's.
	NormalImpulse float64
	// TangentImpulse is cp's jtAcc, the accumulated friction impulse.
	TangentImpulse float64

	// r1 and r2 are the point's offsets from A's and B's centres of gravity,
	// written at detection as cp's Update writes them.
	r1, r2 m.Vec2d

	nMass, tMass float64
	bounce, bias float64
	jBias        float64

	// id says which point of the pair this is across ticks: two vertex indices
	// packed into a uint32, exact where cp mixes shape pointers into a hash and
	// carries the comment that it could trigger false positives. A and B already
	// fix the two Shapes, so the id only has to tell one pair's at most two
	// points apart.
	id uint32
	_  [4]byte
}

// Contact is one pair of touching Shapes, 320 bytes: cp's arbiter as one
// struct, scratch included.
//
// A split into an app-facing entry and a parallel solver-private array — drawn
// along cp's own "survives the tick / rewritten by PreStep" line — was weighed
// and rejected. The scratch lives here, which is also what makes TotalKE a
// method rather than something needing a back-pointer.
//
// Solid Contacts and Sensor Hits share the type; Solve skips Sensor entries.
type Contact struct {
	// A and B are the two parties. A is the Sensor; otherwise the party that is
	// not Static; otherwise the lower Entity. Pairs are matched across ticks as
	// unordered pairs, so a change of A never breaks the phase.
	A, B ecs.Entity
	// Normal is B's surface at the Contact, facing A: the direction A is pushed
	// along to part the two. There is one normal and one convention, flipped
	// once at the boundary of detection; cp's swapped flag is not ported, and
	// neither is the sign wart in TotalImpulse that it caused.
	Normal m.Vec2d
	// SurfaceVelocity is cp's surface_vr, the relative velocity of the two
	// surfaces with its normal component removed. It is zero until Shapes carry
	// a surface velocity of their own.
	//
	// When they do, it is A's less B's, and the sign is worth stating because it
	// is easy to land backwards. cp computes b.surfaceV − a.surfaceV, and this
	// port's A plays the part cp's b plays — the Normal faces A where cp's faces
	// its second Body — so cp's expression is A's less B's here. The
	// specification's prose spells it the other way round, quoting cp's own a
	// and b; it changes no number today, because nothing writes this field.
	SurfaceVelocity m.Vec2d
	// Points are the Contact's points, of which Count are meaningful.
	Points [2]ContactPoint
	// T is the fraction of the tick at which the pair met. Everything found
	// where the tick ended reports 1.
	T float64
	// Friction is cp's u for this pair, the product of the two Shapes', filled
	// by Detect every tick and writable by a filter for one tick.
	Friction float64
	// Restitution is cp's e for this pair, the product of the two Shapes'.
	Restitution float64

	// gjkId is cp's collisionId, the cached simplex the next tick's GJK warm
	// starts from. It crosses ticks and is zero for the closed forms.
	gjkId uint32

	// Count is how many of Points mean anything.
	Count uint8
	// Phase is where this Contact is in its life.
	Phase Phase
	// Sensor reports that at least one of the two Shapes is a Sensor, which is
	// what keeps the pair out of the solution.
	Sensor bool

	flags uint8
}

// Dropped reports that a filter has taken this pair out of this tick's
// solution.
func (c *Contact) Dropped() bool { return c.flags&flagDropped != 0 }

// Ignored reports that a filter has taken this pair out of the solution until
// it comes apart.
func (c *Contact) Ignored() bool { return c.flags&flagIgnored != 0 }

// Drop takes the pair out of this tick's solution. An Ended entry cannot be
// dropped — there is nothing left to take out of.
func (c *Contact) Drop() {
	if c.Phase != PhaseEnded {
		c.flags |= flagDropped
	}
}

// Ignore is cp's arb.Ignore: it takes the pair out of the solution until the
// two come apart. While they keep touching the entry arrives already marked.
func (c *Contact) Ignore() { c.flags |= flagIgnored }

// Other is the party of the pair that is not e, and NoEntity when e is neither.
func (c *Contact) Other(e ecs.Entity) ecs.Entity {
	switch e {
	case c.A:
		return c.B
	case c.B:
		return c.A
	}
	return ecs.NoEntity
}

// NormalFor is the unit normal along which e is pushed out of the pair, which
// is Normal for A and its negation for B. It is one party's view of the one
// normal, so a reacting System never flips a sign by hand.
func (c *Contact) NormalFor(e ecs.Entity) m.Vec2d {
	if e == c.B {
		return c.Normal.Negate()
	}
	return c.Normal
}

// TotalImpulse is cp's Arbiter.TotalImpulse: the impulse this Contact applied to
// A over the tick, normal and friction together.
//
// cp returns the sum negated unless its swapped flag is set, a wart of the
// per-pair handlers this port does not have. Deleting swapped deletes the wart:
// the sum is the impulse applied to A, which is the party the Normal faces, and
// B's is its negation.
func (c *Contact) TotalImpulse() m.Vec2d {
	var sum m.Vec2d
	for i := range int(c.Count) {
		point := c.Points[i]
		sum = sum.Add(c.Normal.Rotate(m.Vec2d{X: point.NormalImpulse, Y: point.TangentImpulse}))
	}
	return sum
}

// TotalKE is Chipmunk's cpArbiterTotalKE: the kinetic energy the Contact
// removed over the tick, which is what a collision sound or a damage number is
// scaled by.
//
// It has no counterpart in jakecoffman/cp, whose only kinetic-energy
// computation is inline in DebugInfo, so this is ported from the C.
//
// One guard is added: a point PreStep never reached has nMass and tMass of
// zero, and C never meets one because its Count() reports 0 for a cached
// arbiter. This port's Ended entries keep their points, which is a departure
// already stated, so the divide has to be guarded or that departure would hand
// back an infinity.
func (c *Contact) TotalKE() float64 {
	eCoef := (1 - c.Restitution) / (1 + c.Restitution)
	var sum float64
	for i := range int(c.Count) {
		point := c.Points[i]
		if point.nMass != 0 {
			sum += eCoef * point.NormalImpulse * point.NormalImpulse / point.nMass
		}
		if point.tMass != 0 {
			sum += point.TangentImpulse * point.TangentImpulse / point.tMass
		}
	}
	return sum
}

// zeroImpulses forgets the accumulated solution, which is what a tick the
// solver skipped the pair on leaves behind.
func (c *Contact) zeroImpulses() {
	for i := range c.Points {
		c.Points[i].NormalImpulse = 0
		c.Points[i].TangentImpulse = 0
	}
}

// phased reports that the entry was a current one the previous tick that no
// filter dropped, which is what the next tick's phase is compared against.
//
// An ignored entry counts. The two marks differ in exactly this: a drop is for
// one tick, so the filter re-decides every tick and a pair it dropped begins
// again — the one accepted difference from cp, which does not call Begin twice.
// An ignore runs until the pair comes apart, so there is no re-decision to
// report and the pair Continues, which is cp's own IGNORE state persisting.
func (c *Contact) phased() bool {
	return c.Phase != PhaseEnded && c.flags&flagDropped == 0
}

// survived reports that the entry was one the app saw touching and no filter
// marked at all, which is what an Ended entry is produced for: a dropped pair
// was never shown beginning and an ignored one is never seen at all, so neither
// has an end to report.
func (c *Contact) survived() bool {
	return c.phased() && c.flags&flagIgnored == 0
}

// contactAux is the run of solver- and detector-private numbers beside each
// entry, kept out of the entry itself so that the 320 bytes the specification
// lays out stay exactly what they are.
//
// slotA and slotB are the two parties' BodyIndex slots, which detection has in
// hand as it walks, and −1 for a Static party, which is in the other index and
// is one immovable row in the solver's gather however many of them there are.
type contactAux struct {
	slotA, slotB int32
	// missed is how many consecutive ticks the pair has not touched: 0 while it
	// touches, 1 on the tick its Ended entry is reported, and upwards while it
	// is carried cached.
	missed int32
	// matched records that this tick's detection found the pair again, so the
	// pass that builds the Ended and cached runs can skip it.
	matched bool
}

// Contacts is the tick's Contact list: one entry per pair, rebuilt each tick
// reusing its buffers.
//
// It is a Resource rather than Entities, because spawning takes the frame-wide
// write on the id authority. Its contents persist until the next Detect, so a
// System ordered before Integrate legally reads the previous tick's.
//
// The buffer carries three runs and the app sees two:
//
//	[ current entries | ended entries | cached entries ]
//	                                   ^ the public view stops here
//
// A cached entry has already reported Ended and is carried forward unreported,
// purely as a carrier of Impulses, until it expires — cp's CACHED state, which
// is what lets a pair that flickers apart and back within the persistence
// window keep its Impulses.
type Contacts struct {
	// entries is this tick's three runs, and previous is the tick before's. The
	// two buffers swap, which is what lets detection look the previous tick up
	// while it appends this tick's.
	entries  []Contact
	previous []Contact
	aux      []contactAux
	prevAux  []contactAux

	// current is the length of the current run and visible the length of the
	// public view, current plus ended.
	current int
	visible int

	// lookup maps this tick's unordered Entity pairs to their slot in entries,
	// and prevLookup the previous tick's. Detection inserts into the first as it
	// appends and looks the pair up in the second — no extra pass, no sort.
	lookup     pairTable
	prevLookup pairTable

	// probes is the swept Sensor scratch: the Hits along one Sensor's path,
	// refilled once a Sensor and kept across ticks, so a steady scene allocates
	// nothing for the Probe half of detection either.
	probes []Hit

	// maxSlot is one past the largest BodyIndex slot detection saw, which sizes
	// the solver's slot table without Solve reading an index of its own.
	maxSlot int32

	// nudge is the seeded source the one coin flip in the whole package draws
	// from, and nudged is the direction it drew for this tick: the way two
	// exactly coincident circles are parted.
	//
	// It is drawn once a tick rather than once a pair, because a pair that has
	// to ask is vanishingly rare and asking costs a sine and a cosine, which
	// every candidate pair would otherwise pay before it knew whether it
	// touched. One draw a tick still breaks the symmetry cp's fixed (1, 0)
	// never breaks, and two coincident pairs within one tick are different
	// pairs, not a symmetry.
	nudge  uint64
	nudged m.Vec2d

	// solver is the gather the impulse solver runs over. It lives here because
	// Solve write-locks this Resource already and nothing else may see it.
	solver solver
}

// NewContacts is an empty Contact list, its coincidence nudge seeded from the
// caller's seed.
//
// The seed is mixed rather than taken raw because the generator behind it has
// zero as a fixed point, and zero is an ordinary seed a caller may name — the
// settings take it as given rather than as a request for an arbitrary one. The
// mixing is a multiply by an odd constant, so it is a permutation and exactly
// one seed still lands on zero; that one is moved off it, because the state
// staying zero would hand back cp's own fixed direction for ever.
func NewContacts(seed uint64) *Contacts {
	state := seed*0x9E3779B97F4A7C15 + 0x9E3779B97F4A7C15
	if state == 0 {
		state = 0x9E3779B97F4A7C15
	}
	return &Contacts{nudge: state}
}

// All is the tick's Contacts: every current entry and then every Ended one.
// Cached entries are invisible and sit past the end of it.
//
// The slice is the list's own, so a filter System writes an entry's marks and
// its material through it and a reacting System reads this tick's Impulses from
// it. It is valid until the next Detect.
func (c *Contacts) All() []Contact { return c.entries[:c.visible] }

// Len is how many Contacts the app can see.
func (c *Contacts) Len() int { return c.visible }

// beginTick swaps the two buffers and the two maps, so that what detection is
// about to write goes into the buffer the previous tick read from.
func (c *Contacts) beginTick() {
	c.entries, c.previous = c.previous, c.entries
	c.aux, c.prevAux = c.prevAux, c.aux
	c.lookup, c.prevLookup = c.prevLookup, c.lookup

	c.entries = c.entries[:0]
	c.aux = c.aux[:0]
	c.lookup.clear()
	c.current, c.visible = 0, 0
	c.maxSlot = 0
	c.nudged = c.nudgeNormal()
}

// nudgeNormal draws this tick's coincidence direction. cp invents a fixed
// (1, 0) there, which never breaks the symmetry it is there to break.
//
// It is an xorshift64 over the seeded state, which allocates nothing and needs
// no source object.
func (c *Contacts) nudgeNormal() m.Vec2d {
	c.nudge ^= c.nudge << 13
	c.nudge ^= c.nudge >> 7
	c.nudge ^= c.nudge << 17
	return m.ForAngle(float64(c.nudge>>11) * (2 * math.Pi / (1 << 53)))
}

// append puts one entry and its private run on the end of the buffer and into
// this tick's map.
func (c *Contacts) append(entry Contact, aux contactAux) {
	c.lookup.insert(entry.A, entry.B, int32(len(c.entries)))
	c.entries = append(c.entries, entry)
	c.aux = append(c.aux, aux)
}

// endTick closes the current run and writes the Ended and cached ones after it,
// from the previous tick's entries this tick's detection did not find again.
//
// persistence is cp's collisionPersistence in ticks: an entry reports Ended on
// its first missed tick and is carried cached until it has missed that many.
func (c *Contacts) endTick(persistence int) {
	c.current = len(c.entries)

	// The Ended run first, so that every entry the app sees ordered after every
	// current one, and the cached run after that, out of the public view.
	for i := range c.previous {
		old := &c.previous[i]
		if c.prevAux[i].matched || old.Phase == PhaseEnded {
			continue
		}
		if !old.survived() {
			// A dropped or ignored entry is one the app was never shown
			// touching, so there is no end to report and nothing worth carrying:
			// a dropped tick has already had its Impulses zeroed, and an ignored
			// pair was never solved. The pair starts again at Began if it comes
			// back.
			continue
		}
		ended := *old
		ended.Phase = PhaseEnded
		// The ignore ends when the pair misses one tick, so a re-touch inside
		// the persistence window arrives unmarked.
		ended.flags = 0
		c.append(ended, contactAux{slotA: -1, slotB: -1, missed: 1})
	}
	c.visible = len(c.entries)

	for i := range c.previous {
		old := &c.previous[i]
		if c.prevAux[i].matched || old.Phase != PhaseEnded {
			continue
		}
		if old.Dropped() {
			// A Continuing entry a filter dropped is rewritten Ended so that
			// reacting Systems see the end, but it never survived and its
			// Impulses were zeroed, so there is nothing to carry forward.
			continue
		}
		missed := c.prevAux[i].missed + 1
		if int(missed) >= persistence {
			continue
		}
		c.append(*old, contactAux{slotA: -1, slotB: -1, missed: missed})
	}
}

// pairTable is the open-addressed map from an unordered Entity pair to a slot
// in the buffer beside it. There are two of them and they swap with the
// buffers, so detection inserts into one as it appends and looks the pair up in
// the other — about two probes an entry a tick, and no sort.
//
// It is a table of its own rather than cp's persistent arbiter table at stable
// slots: that would give persistence for free but turn the app's two walks a
// tick into an indirect walk over a table with holes, and the contiguous walk is
// what those two passes are paying for.
type pairTable struct {
	keys []pairKey
	used int
}

// pairKey is one cell. A cell with A of NoEntity is empty, which an Entity
// never is: generations start at 1.
type pairKey struct {
	a, b ecs.Entity
	slot int32
}

// pairTableMin is where a fresh table starts. It doubles whenever it is half
// full, and never shrinks on its own.
const pairTableMin = 64

// clear empties the table, keeping the memory it has grown.
func (t *pairTable) clear() {
	for i := range t.keys {
		t.keys[i] = pairKey{}
	}
	t.used = 0
}

// order is the pair as the table keys it, lower Entity first, so that a change
// of which party is A never loses the pair.
func order(a, b ecs.Entity) (ecs.Entity, ecs.Entity) {
	if b < a {
		return b, a
	}
	return a, b
}

// hashPair mixes the ordered pair into a bucket. cp's own HashPair multiplies
// and adds two pointers, which needs no mixing because a pointer is already
// spread; Entity ids are small dense integers and would cluster, so this is
// splitmix's finaliser over the two.
func hashPair(a, b ecs.Entity) uint64 {
	x := uint64(a)*0x9E3779B97F4A7C15 ^ uint64(b)*0xC2B2AE3D27D4EB4F
	x ^= x >> 29
	x *= 0xBF58476D1CE4E5B9
	x ^= x >> 32
	return x
}

// find is the slot the pair is at, or false. The full pair is verified rather
// than trusted, the entry carrying it anyway.
func (t *pairTable) find(a, b ecs.Entity) (int32, bool) {
	if len(t.keys) == 0 {
		return 0, false
	}
	low, high := order(a, b)
	mask := uint64(len(t.keys) - 1)
	for at := hashPair(low, high) & mask; ; at = (at + 1) & mask {
		cell := &t.keys[at]
		switch {
		case cell.a == ecs.NoEntity:
			return 0, false
		case cell.a == low && cell.b == high:
			return cell.slot, true
		}
	}
}

// insert records where the pair's entry is. A pair is inserted at most once a
// tick, so there is no replacement path and no tombstone.
func (t *pairTable) insert(a, b ecs.Entity, slot int32) {
	if t.used*2 >= len(t.keys) {
		t.grow()
	}
	low, high := order(a, b)
	mask := uint64(len(t.keys) - 1)
	for at := hashPair(low, high) & mask; ; at = (at + 1) & mask {
		if t.keys[at].a == ecs.NoEntity {
			t.keys[at] = pairKey{a: low, b: high, slot: slot}
			t.used++
			return
		}
	}
}

// grow doubles the table and re-inserts what it held.
func (t *pairTable) grow() {
	size := len(t.keys) * 2
	if size < pairTableMin {
		size = pairTableMin
	}
	old := t.keys
	t.keys = make([]pairKey, size)
	t.used = 0
	for i := range old {
		if old[i].a != ecs.NoEntity {
			t.insert(old[i].a, old[i].b, old[i].slot)
		}
	}
}
