package types

import (
	"math"

	"github.com/dvoyni/cog/libs/m"
)

// The tick's Contact list, and the buffer swap that is its life. What one
// entry is, the pair and its points and the marks a filter leaves on it, is
// contact.go; the map from a pair to its slot is pairtable.go.
//
// The list is too large for one file and each pass over it has its own
// subject: contacts-detect.go finds the tick's pairs, contacts-sensors.go
// sweeps the moving Sensors ahead of them, contacts-solve.go and
// contacts-joints.go are the two halves of the impulse solver, and
// contacts-shrink.go gives the buffers back.

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
	// exactly coincident Shapes are parted, of whatever kinds.
	//
	// It reaches every collision arm and not only the circles'. The closed
	// circle-against-circle form reads it when the two centres coincide; the
	// four GJK arms hand it to gjk, whose cold-start axis is the difference of
	// the two bounding-box centres and therefore nothing at the same placement.
	// One seed, one mechanism, read at one condition.
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

// nudgeNormal draws this tick's coincidence direction: one draw, which every
// pair the tick tests then shares. cp invents a fixed (1, 0) there, which never
// breaks the symmetry it is there to break.
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
