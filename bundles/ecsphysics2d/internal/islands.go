package internal

import (
	"math"
	"unsafe"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/libs/m"
)

// Sleeping: cp's ProcessComponents, which finds the Islands each tick, puts to
// sleep an Island whose every Body has been idle long enough, and wakes a
// sleeping one when something disturbs it. What it keeps per Body is Rest, in
// sleep.go; the Body index's grid for sleepers is index-sleepers.go.
//
// cp builds its Islands by flood-filling each Body's intrusive lists of
// arbiters and constraints. The port has neither list, so an Island is a
// union-find over the tick's awake Dynamic bodies, joined along the Contact
// list and the Joints, with no heap allocation once its buffers have grown.

// island is one sleeping Island: its Bodies, and the Contacts that went quiet
// with it, each a run of a slab the state below keeps.
type island struct {
	members, memberCount int32
	quiet, quietCount    int32
	// seen counts the members the tick's walk over the sleepers found. One
	// missing is a member that was despawned, or lost a Component the walk
	// needs, and the Island wakes.
	seen int32
	// gone is set by Detect when a Static one of the Island's quiet Contacts
	// names has left the static index: what rested on it wakes.
	gone bool
	live bool
	// waking marks an Island already queued to wake this tick.
	waking bool
}

// islands is the sleep System's state, kept on the Contact list because the
// quiet Contacts are Contact entries and the System write-locks the list
// already. The records and the two slabs cross ticks; everything below them is
// scratch refilled every tick.
type islands struct {
	records []island
	free    []int32
	live    int

	// members and quiet are the slabs every Island's two runs are cut from, and
	// memberOwner and quietOwner name the record each position belongs to, so
	// that compacting either slab is one pass over it.
	members     []ecs.Entity
	memberOwner []int32
	quiet       []Contact
	quietOwner  []int32
	// garbage is how much of the two slabs belongs to Islands that have woken.
	garbageMembers, garbageQuiet int

	// gravity is the Constants' Gravity the System saw last, and gravitySeen
	// says it has seen one: a changed gravity wakes every Island, as cp's
	// SetGravity does.
	gravity     m.Vec2d
	gravitySeen bool
	// removals is the static index's count of Shapes that left it, as Detect
	// saw it last.
	removals uint64
	// tick counts the System's runs, and is what a Rest's stamps compare with.
	tick uint32

	// The tick's scratch.
	waking      []int32
	nodes       []ecs.Entity
	parent      []int32
	idle        []float64
	least       []float64
	counts      []int32
	rootIsland  []int32
	entryIsland []int32
	fill        []int32
	tailEntries []Contact
	tailAux     []contactAux
	ended       []Contact
	slots       entityTable
}

// The classes a party of a Contact or a Joint falls into, beside a node.
const (
	partyStatic    = -1
	partyKinematic = -2
)

// ProcessIslands is the sleep System, run between Detect and Solve where cp's
// ProcessComponents runs: it keeps each awake Dynamic body's idle time, wakes
// every sleeping Island something disturbed, builds the tick's Islands and puts
// to sleep those whose every Body has been idle for sleepTime seconds.
//
// A sleepTime of 0 is sleeping off, the zero-value spelling of cp's infinite
// SleepTimeThreshold: no Island is built, and an Island still asleep from
// before is woken. idleSpeed is cp's IdleSpeedThreshold, and 0 falls back to
// cp's estimate from gravity, |g|·h — so a world with no gravity and an
// idleSpeed of 0 never idles.
func ProcessIslands(
	contacts *Contacts,
	awake *ecs.Query[AwakeQuery],
	asleep *ecs.Query[AsleepQuery],
	joints *ecs.Query[IslandJointQuery],
	rests *ecs.Set[Rest],
	forces *ecs.Set[Force],
	velocities *ecs.Get[Velocity],
	places *ecs.Get[Position],
	tag *ecs.Set[Sleeping],
	untag *ecs.Remove[Sleeping],
	wakes *Wakes,
	idleSpeed, sleepTime float64,
	gravity m.Vec2d,
	h float64,
) {
	s := &contacts.islands
	s.tick++
	s.waking = s.waking[:0]

	if !(sleepTime > 0) || math.IsInf(sleepTime, 1) {
		// Off. Nothing is built, and nothing stays asleep: an Island asleep from
		// before sleeping was turned off wakes now, rather than never, which is
		// what cp's own flood fill would leave it.
		wakes.entities = wakes.entities[:0]
		s.gravitySeen = false
		if s.live == 0 {
			return
		}
		s.wakeAll()
		s.applyWakes(s.waking, rests, forces, untag)
		contacts.rewrite(nil, rests, places)
		s.release()
		return
	}

	s.findWakes(contacts, asleep, joints, rests, velocities, wakes, gravity)
	s.applyWakes(s.waking, rests, forces, untag)

	s.build(contacts, awake, joints, rests, velocities, idleSpeed, gravity, h)
	if len(contacts.held) > 0 {
		woken := len(s.waking)
		s.touchHeld(contacts, rests)
		s.applyWakes(s.waking[woken:], rests, forces, untag)
	}
	falling := s.fallAsleep(contacts, rests, forces, velocities, places, tag, sleepTime)

	if falling > 0 {
		contacts.rewrite(s.entryIsland, rests, places)
	} else if len(s.waking) > 0 {
		contacts.rewrite(nil, rests, places)
	}
	s.release()
}

// findWakes queues every sleeping Island something disturbed since the last
// tick, which is everything cp's setters and its arbiter loop wake a Body for:
//
//   - a changed gravity wakes every Island, as cp's SetGravity does;
//   - a Static that left the static index wakes every Island whose quiet
//     Contacts name it, as cp's RemoveShape wakes what touched a static one;
//   - a WakeCmd wakes the Island of the Entity it names;
//   - a sleeper whose Position, Velocity or Force differs from what the System
//     left in it wakes its Island. The port has no setters, so this compares
//     values: a kick, an impulse, a teleport or a new Force. A Force equal to
//     the one it carried when it fell asleep disturbs nothing, which is what
//     keeps gravity an app writes into Force every tick from waking it;
//   - a member despawned, or stripped of what the walk reads, wakes the rest;
//   - a solid Contact between an awake Body and a sleeper wakes the sleeper's
//     Island, which is cp's arbiter loop;
//   - a Joint from a sleeper to an awake Body, or between two Islands, wakes
//     them.
//
// Every sleeper not woken has its Force cleared, so that gameplay adding to it
// each tick never builds it up.
func (s *islands) findWakes(
	contacts *Contacts,
	asleep *ecs.Query[AsleepQuery],
	joints *ecs.Query[IslandJointQuery],
	rests *ecs.Set[Rest],
	velocities *ecs.Get[Velocity],
	wakes *Wakes,
	gravity m.Vec2d,
) {
	if s.gravitySeen && gravity != s.gravity && s.live > 0 {
		s.wakeAll()
	}
	s.gravity, s.gravitySeen = gravity, true

	for id := range s.records {
		if record := &s.records[id]; record.live && record.gone {
			s.wake(int32(id))
		}
	}

	for _, e := range wakes.entities {
		if rest, ok := rests.Ref(e); ok && rest.Island >= 0 {
			s.wake(rest.Island)
		}
	}
	wakes.entities = wakes.entities[:0]

	if s.live == 0 {
		return
	}

	for i := range s.records {
		s.records[i].seen = 0
	}
	for _, it := range asleep.All() {
		rest := it.Rest
		if rest.Island < 0 {
			continue
		}
		record := &s.records[rest.Island]
		record.seen++
		if record.waking {
			continue
		}
		if it.Place != rest.Position || it.Velocity != rest.Velocity || *it.Force != rest.Force {
			s.wake(rest.Island)
			continue
		}
		*it.Force = Force{}
		rest.Cleared = s.tick
	}
	for id := range s.records {
		record := &s.records[id]
		if record.live && !record.waking && record.seen < record.memberCount {
			s.wake(int32(id))
		}
	}

	// A sleeper is only ever found by Detect against an awake Body, its own
	// grid being tested against nothing else, so a party with a slot in the
	// sleepers' numbering is a sleeper something awake is touching.
	awake := contacts.awakeSlots
	for i := range contacts.current {
		entry := &contacts.entries[i]
		if entry.Sensor || entry.Dropped() || entry.Ignored() {
			continue
		}
		aux := &contacts.aux[i]
		if aux.slotA >= awake {
			s.wakeEntity(entry.A, rests)
		}
		if aux.slotB >= awake {
			s.wakeEntity(entry.B, rests)
		}
	}

	for _, it := range joints.All() {
		a, b := it.Joint.A, it.Joint.B
		islandA, movableA := s.jointSide(a, rests, velocities)
		islandB, movableB := s.jointSide(b, rests, velocities)
		switch {
		case islandA >= 0 && islandB >= 0:
			if islandA != islandB {
				s.wake(islandA)
				s.wake(islandB)
			}
		case islandA >= 0 && movableB:
			s.wake(islandA)
		case islandB >= 0 && movableA:
			s.wake(islandB)
		}
	}
}

// jointSide is the Island a Joint's party sleeps in, or −1, and whether an
// awake party is one that moves — a Dynamic or a Kinematic body, which is a
// Body with a Velocity. A Static party and a dangling Reference are neither.
func (s *islands) jointSide(e ecs.Entity, rests *ecs.Set[Rest], velocities *ecs.Get[Velocity]) (int32, bool) {
	if rest, ok := rests.Ref(e); ok && rest.Island >= 0 {
		return rest.Island, false
	}
	_, moves := velocities.Of(e)
	return -1, moves
}

// markGoneSupports is Detect's half of the rule that removing a support wakes
// what rests on it. Detect reads the static index already, so when a Shape has
// left it since the last tick, Detect marks every quiet Contact whose Static
// party is no longer there, and the Island it belongs to; the sleep System
// wakes that Island and hands the Contact back Ended. cp's RemoveShape wakes
// what touched a static one the same way.
func (s *islands) markGoneSupports(statics *StaticIndex) {
	if statics.removals == s.removals {
		return
	}
	s.removals = statics.removals
	for id := range s.records {
		record := &s.records[id]
		if !record.live || record.quiet < 0 {
			continue
		}
		for i := record.quiet; i < record.quiet+record.quietCount; i++ {
			entry := &s.quiet[i]
			if staticGone(entry, flagStaticA, entry.A, statics) || staticGone(entry, flagStaticB, entry.B, statics) {
				entry.flags |= flagGone
				record.gone = true
			}
		}
	}
}

// staticGone reports that a quiet entry's party, marked Static by flag, has
// left the static index.
func staticGone(entry *Contact, flag uint8, e ecs.Entity, statics *StaticIndex) bool {
	if entry.flags&flag == 0 {
		return false
	}
	_, held := statics.slots[e]
	return !held
}

func (s *islands) wakeEntity(e ecs.Entity, rests *ecs.Set[Rest]) {
	if rest, ok := rests.Ref(e); ok && rest.Island >= 0 {
		s.wake(rest.Island)
	}
}

// wake queues one Island to wake this tick, once.
func (s *islands) wake(id int32) {
	record := &s.records[id]
	if !record.live || record.waking {
		return
	}
	record.waking = true
	s.waking = append(s.waking, id)
}

func (s *islands) wakeAll() {
	for id := range s.records {
		s.wake(int32(id))
	}
}

// applyWakes wakes every queued Island: each member loses the Sleeping Tag,
// its idle time goes back to zero, and a Force the walk cleared this tick is
// put back, so the tick it wakes spends it. What the System left in it stays,
// for the rewrite to check its quiet Contacts against.
func (s *islands) applyWakes(ids []int32, rests *ecs.Set[Rest], forces *ecs.Set[Force], untag *ecs.Remove[Sleeping]) {
	for _, id := range ids {
		record := &s.records[id]
		for _, e := range s.members[record.members : record.members+record.memberCount] {
			rest, ok := rests.Ref(e)
			if !ok {
				continue
			}
			rest.Island, rest.Idle, rest.Woke = -1, 0, s.tick
			if rest.Cleared == s.tick {
				if force, ok := forces.Ref(e); ok {
					*force = rest.Force
				}
			}
			untag.From(e)
		}
	}
}

// build is the tick's Islands: every awake Dynamic body a node with its idle
// time brought up to date, joined along the solid Contacts and the Joints
// between two of them. A Static or Kinematic body never joins an Island and
// never bridges two; a Kinematic one keeps what it touches awake.
func (s *islands) build(
	contacts *Contacts,
	awake *ecs.Query[AwakeQuery],
	joints *ecs.Query[IslandJointQuery],
	rests *ecs.Set[Rest],
	velocities *ecs.Get[Velocity],
	idleSpeed float64,
	gravity m.Vec2d,
	h float64,
) {
	s.nodes, s.parent, s.idle = s.nodes[:0], s.parent[:0], s.idle[:0]

	// cp's threshold, compared against its KineticEnergy, which has no ½: a
	// Body is idle when v·v·m + w²·i ≤ m·dv².
	dvsq := idleSpeed * idleSpeed
	if idleSpeed == 0 {
		dvsq = gravity.Dot(gravity) * h * h
	}

	for e, it := range awake.All() {
		rest, ok := rests.Ref(e)
		if !ok {
			rests.UpdateFor(e, Rest{Island: -1, Force: it.Force})
			rest, _ = rests.Ref(e)
		}
		if rest.Stamp != s.tick-1 {
			// Not walked awake last tick — new, woken, or sleeping was off —
			// so what idle time it has is not this run's.
			rest.Idle = 0
		}
		switch {
		case rest.Woke == s.tick:
			rest.Idle = 0
		case kineticEnergy(&it.Body, it.Velocity) > idleThreshold(&it.Body, dvsq):
			rest.Idle = 0
		default:
			rest.Idle += h
		}
		// A changed Force resets the idle time, which is cp's every force
		// write waking the Body, but for a value repeated every tick: gravity
		// written into Force is the same number tick after tick and disturbs
		// nothing. That is what keeps a Body leaning on a wall under input
		// that changes every tick from stuttering between asleep and awake.
		if it.Force != rest.Force {
			rest.Idle = 0
			rest.Force = it.Force
		}
		rest.Node, rest.Stamp = int32(len(s.nodes)), s.tick
		s.nodes = append(s.nodes, e)
		s.parent = append(s.parent, rest.Node)
		s.idle = append(s.idle, rest.Idle)
	}

	for i := range contacts.current {
		entry := &contacts.entries[i]
		if entry.Sensor || entry.Dropped() || entry.Ignored() {
			continue
		}
		aux := &contacts.aux[i]
		a := s.contactParty(entry.A, aux.slotA, rests)
		b := s.contactParty(entry.B, aux.slotB, rests)
		s.join(a, b, rests)
	}

	for _, it := range joints.All() {
		a := s.jointParty(it.Joint.A, rests, velocities)
		b := s.jointParty(it.Joint.B, rests, velocities)
		s.join(a, b, rests)
	}
}

// touchHeld is what the Hits the path pass held past each fast solid Body's
// stop do to the Islands, once build has said which movers are Dynamic: a
// Dynamic mover stopped before any of them, so they touch nothing, and a
// Kinematic one is never stopped, so Solve carries each Dynamic body its path
// met (carryHeld). A carried Body is kept awake, as one touching a Kinematic
// body is, and a carried sleeper's Island wakes, as it wakes for a Contact
// naming it, so that Solve moves and solves an awake Body. Only the sleep
// System and Solve read Dynamic, so the held Hits are written as Contacts by
// Solve and are read here from where Detect held them.
func (s *islands) touchHeld(contacts *Contacts, rests *ecs.Set[Rest]) {
	awake := contacts.awakeSlots
	for i := range contacts.held {
		held := &contacts.held[i]
		moverSlot, target, targetSlot := held.parties()
		if s.contactParty(held.mover, moverSlot, rests) != partyKinematic {
			continue
		}
		if targetSlot >= awake {
			s.wakeEntity(target, rests)
			continue
		}
		if node := s.contactParty(target, targetSlot, rests); node >= 0 {
			s.keepAwake(node, rests)
		}
	}
}

// join is one edge of the tick's graph: two nodes are one Island, and a node
// touching a Kinematic body is kept awake — cp's Activate on a Body whose
// arbiter or constraint names a Kinematic one.
func (s *islands) join(a, b int32, rests *ecs.Set[Rest]) {
	switch {
	case a >= 0 && b >= 0:
		s.union(a, b)
	case a >= 0 && b == partyKinematic:
		s.keepAwake(a, rests)
	case b >= 0 && a == partyKinematic:
		s.keepAwake(b, rests)
	}
}

func (s *islands) keepAwake(node int32, rests *ecs.Set[Rest]) {
	s.idle[node] = 0
	if rest, ok := rests.Ref(s.nodes[node]); ok {
		rest.Idle = 0
	}
}

// contactParty is the node a Contact's party is this tick, or its class: a
// slot of −1 is a Static, and anything else that is not a node moves and is not
// Dynamic, which is a Kinematic body.
func (s *islands) contactParty(e ecs.Entity, slot int32, rests *ecs.Set[Rest]) int32 {
	if slot < 0 {
		return partyStatic
	}
	if rest, ok := rests.Ref(e); ok && rest.Stamp == s.tick && rest.Island < 0 {
		return rest.Node
	}
	return partyKinematic
}

// jointParty is contactParty for a Joint's party, which carries no slot: a
// party with no Velocity is Static, or a dangling Reference, and either way
// joins nothing.
func (s *islands) jointParty(e ecs.Entity, rests *ecs.Set[Rest], velocities *ecs.Get[Velocity]) int32 {
	if rest, ok := rests.Ref(e); ok && rest.Stamp == s.tick && rest.Island < 0 {
		return rest.Node
	}
	if _, moves := velocities.Of(e); moves {
		return partyKinematic
	}
	return partyStatic
}

func (s *islands) find(node int32) int32 {
	for s.parent[node] != node {
		s.parent[node] = s.parent[s.parent[node]]
		node = s.parent[node]
	}
	return node
}

func (s *islands) union(a, b int32) {
	ra, rb := s.find(a), s.find(b)
	if ra != rb {
		s.parent[rb] = ra
	}
}

// fallAsleep puts every Island whose every Body has been idle for sleepTime to
// sleep, cp's ComponentActive: each member gains the Sleeping Tag, what it
// carries is recorded, and its Force is cleared. It marks, for the rewrite,
// which of the tick's Contacts go quiet with which Island, and answers how
// many Islands fell asleep.
func (s *islands) fallAsleep(
	contacts *Contacts,
	rests *ecs.Set[Rest],
	forces *ecs.Set[Force],
	velocities *ecs.Get[Velocity],
	places *ecs.Get[Position],
	tag *ecs.Set[Sleeping],
	sleepTime float64,
) int {
	n := len(s.nodes)
	s.least = resize(s.least, n)
	s.counts = resizeInt32(s.counts, n)
	s.rootIsland = resizeInt32(s.rootIsland, n)
	for i := range n {
		s.least[i] = math.Inf(1)
		s.counts[i] = 0
		s.rootIsland[i] = -1
	}
	for i := range int32(n) {
		root := s.find(i)
		s.least[root] = math.Min(s.least[root], s.idle[i])
		s.counts[root]++
	}

	falling := 0
	for i := range int32(n) {
		if s.parent[i] != i || !(s.least[i] >= sleepTime) {
			continue
		}
		id := s.newIsland(s.counts[i])
		s.rootIsland[i] = id
		// counts is reused as each falling Island's fill cursor.
		s.counts[i] = s.records[id].members
		falling++
	}
	if falling == 0 {
		return 0
	}

	for i := range int32(n) {
		root := s.find(i)
		id := s.rootIsland[root]
		if id < 0 {
			continue
		}
		e := s.nodes[i]
		s.members[s.counts[root]] = e
		s.counts[root]++

		rest, ok := rests.Ref(e)
		if !ok {
			continue
		}
		rest.Island = id
		rest.Velocity, _ = velocities.Of(e)
		rest.Position, _ = places.Of(e)
		if force, ok := forces.Ref(e); ok {
			rest.Force = *force
			*force = Force{}
		}
		rest.Cleared = s.tick
		tag.UpdateFor(e, Sleeping{})
	}

	// The Contacts that go quiet: every solid one whose parties are all asleep
	// now or Static. A solid Contact joins its two Dynamic parties into one
	// Island, so such a Contact is always one Island's own. A Sensor Contact
	// never goes quiet — it joins nothing — and nor does a dropped or an
	// ignored one: with every party asleep none of them is tested next tick,
	// so each ends, as cp's do.
	s.entryIsland = resizeInt32(s.entryIsland, contacts.current)
	for i := range contacts.current {
		s.entryIsland[i] = -1
		entry := &contacts.entries[i]
		if entry.Sensor || entry.Dropped() || entry.Ignored() {
			continue
		}
		aux := &contacts.aux[i]
		a, okA := s.quietParty(entry.A, aux.slotA, rests)
		b, okB := s.quietParty(entry.B, aux.slotB, rests)
		if !okA || !okB || (a < 0 && b < 0) {
			continue
		}
		id := max(a, b)
		s.entryIsland[i] = id
		s.records[id].quietCount++
	}
	for id := range s.records {
		record := &s.records[id]
		if !record.live || record.waking || record.quietCount == 0 || record.quiet >= 0 {
			continue
		}
		record.quiet = int32(len(s.quiet))
		for range record.quietCount {
			s.quiet = append(s.quiet, Contact{})
			s.quietOwner = append(s.quietOwner, int32(id))
		}
	}
	return falling
}

// quietParty is the Island a Contact's party fell asleep in this tick, −1 for a
// Static, and false for anything still awake.
func (s *islands) quietParty(e ecs.Entity, slot int32, rests *ecs.Set[Rest]) (int32, bool) {
	if slot < 0 {
		return -1, true
	}
	rest, ok := rests.Ref(e)
	if !ok || rest.Stamp != s.tick || rest.Island < 0 {
		return 0, false
	}
	return rest.Island, true
}

// newIsland is a fresh record whose members run of that length is cut from the
// end of the slab. Its quiet run is cut once its Contacts are counted.
func (s *islands) newIsland(count int32) int32 {
	var id int32
	if n := len(s.free); n > 0 {
		id = s.free[n-1]
		s.free = s.free[:n-1]
	} else {
		id = int32(len(s.records))
		s.records = append(s.records, island{})
	}
	s.records[id] = island{
		members: int32(len(s.members)), memberCount: count,
		quiet: -1, live: true,
	}
	for range count {
		s.members = append(s.members, ecs.NoEntity)
		s.memberOwner = append(s.memberOwner, id)
	}
	s.live++
	return id
}

// release frees every Island that woke this tick, once the rewrite has handed
// its quiet Contacts back, and compacts a slab once more of it is dead than
// alive.
func (s *islands) release() {
	for _, id := range s.waking {
		record := &s.records[id]
		s.garbageMembers += int(record.memberCount)
		if record.quiet >= 0 {
			s.garbageQuiet += int(record.quietCount)
		}
		*record = island{quiet: -1}
		s.free = append(s.free, id)
		s.live--
	}
	s.waking = s.waking[:0]
	if s.garbageMembers > 64 && 2*s.garbageMembers > len(s.members) {
		s.compactMembers()
	}
	if s.garbageQuiet > 64 && 2*s.garbageQuiet > len(s.quiet) {
		s.compactQuiet()
	}
}

// compactMembers moves every live Island's members down over the dead ones in
// one pass, keeping each run in order. A position is live when its owner is a
// live Island and it lies inside that Island's run as it stood before the pass,
// which is what tells a dead run apart from a live one that reused its record.
func (s *islands) compactMembers() {
	s.fill = resizeInt32(s.fill, len(s.records))
	for id := range s.records {
		s.fill[id] = s.records[id].members
	}
	kept := int32(0)
	for at := range int32(len(s.members)) {
		owner := s.memberOwner[at]
		record := &s.records[owner]
		start := s.fill[owner]
		if !record.live || at < start || at >= start+record.memberCount {
			continue
		}
		if at == start {
			record.members = kept
		}
		s.members[kept], s.memberOwner[kept] = s.members[at], owner
		kept++
	}
	s.members, s.memberOwner = s.members[:kept], s.memberOwner[:kept]
	s.garbageMembers = 0
}

// compactQuiet is compactMembers for the quiet Contacts.
func (s *islands) compactQuiet() {
	s.fill = resizeInt32(s.fill, len(s.records))
	for id := range s.records {
		s.fill[id] = s.records[id].quiet
	}
	kept := int32(0)
	for at := range int32(len(s.quiet)) {
		owner := s.quietOwner[at]
		record := &s.records[owner]
		start := s.fill[owner]
		if !record.live || start < 0 || at < start || at >= start+record.quietCount {
			continue
		}
		if at == start {
			record.quiet = kept
		}
		s.quiet[kept], s.quietOwner[kept] = s.quiet[at], owner
		kept++
	}
	s.quiet, s.quietOwner = s.quiet[:kept], s.quietOwner[:kept]
	s.garbageQuiet = 0
}

// rewrite is the Contact list's half of the tick's sleeping and waking, in one
// pass over the list: the Contacts marks sends quiet leave the current run for
// their Islands' quiet runs, and every quiet Contact of an Island waking this
// tick comes back.
//
// A returning Contact comes back Continuing, with the Impulses it went quiet
// with, into the current run, so Solve warm-starts it this tick: its parties
// have not moved since it went quiet, which is what makes the geometry it
// carries still true. That fails for two, and those come back Ended instead —
// a party that has gone, despawned or taken out of the static index, and a
// party the app moved while it slept, whose Contact names a place it no longer
// is; the next tick's Detect finds whatever it touches now. cp re-detects its
// woken arbiters the same way before it solves them again.
//
// The runs keep their order: current, then Ended, then cached, with what came
// back at the end of the first two. The pair table is rebuilt over the result.
func (c *Contacts) rewrite(marks []int32, rests *ecs.Set[Rest], places *ecs.Get[Position]) {
	s := &c.islands
	returning := false
	for _, id := range s.waking {
		if record := &s.records[id]; record.quiet >= 0 && record.quietCount > 0 {
			returning = true
			break
		}
	}
	if len(marks) == 0 && !returning {
		return
	}
	s.slots.reset()
	if returning {
		c.seedReturningSlots()
	}

	current, visible := c.current, c.visible
	s.tailEntries = append(s.tailEntries[:0], c.entries[current:]...)
	s.tailAux = append(s.tailAux[:0], c.aux[current:]...)

	if len(marks) > 0 {
		s.fill = resizeInt32(s.fill, len(s.records))
		for id := range s.records {
			s.fill[id] = s.records[id].quiet
		}
	}
	kept := 0
	for i := range current {
		if len(marks) > 0 {
			if id := marks[i]; id >= 0 {
				quiet := c.entries[i]
				if c.aux[i].slotA < 0 {
					quiet.flags |= flagStaticA
				}
				if c.aux[i].slotB < 0 {
					quiet.flags |= flagStaticB
				}
				s.quiet[s.fill[id]] = quiet
				s.fill[id]++
				continue
			}
		}
		if kept != i {
			c.entries[kept], c.aux[kept] = c.entries[i], c.aux[i]
		}
		kept++
	}
	c.entries, c.aux = c.entries[:kept], c.aux[:kept]

	s.ended = s.ended[:0]
	for _, id := range s.waking {
		record := &s.records[id]
		if record.quiet < 0 {
			continue
		}
		for _, entry := range s.quiet[record.quiet : record.quiet+record.quietCount] {
			slotA, okA := c.returningParty(&entry, flagStaticA, entry.A, rests, places)
			slotB, okB := c.returningParty(&entry, flagStaticB, entry.B, rests, places)
			if okA && okB {
				entry.Phase, entry.flags = PhaseContinuing, 0
				c.entries = append(c.entries, entry)
				c.aux = append(c.aux, contactAux{slotA: slotA, slotB: slotB})
				continue
			}
			entry.Phase, entry.flags = PhaseEnded, 0
			s.ended = append(s.ended, entry)
		}
	}
	c.current = len(c.entries)

	ended := visible - current
	c.entries = append(c.entries, s.tailEntries[:ended]...)
	c.aux = append(c.aux, s.tailAux[:ended]...)
	for i := range s.ended {
		c.entries = append(c.entries, s.ended[i])
		c.aux = append(c.aux, contactAux{slotA: -1, slotB: -1, missed: 1})
	}
	c.visible = len(c.entries)
	c.entries = append(c.entries, s.tailEntries[ended:]...)
	c.aux = append(c.aux, s.tailAux[ended:]...)

	c.lookup.clear()
	for i := range c.entries {
		c.lookup.insert(c.entries[i].A, c.entries[i].B, int32(i))
	}
}

// seedReturningSlots is the slots this tick already gives a woken member
// before any returning Contact asks for one. Detect numbers a sleeper it finds
// by its place in the sleepers' grid, at or past awakeSlots, in a Contact of
// the current run — a discrete touch or a stop at the first Hit — or in a Hit
// the path pass held, which Solve writes as a Contact (carryHeld). Only a
// sleeper has such a slot, so every one of them is kept for its Entity, and a
// returning Contact naming the same Body is solved in the same row.
func (c *Contacts) seedReturningSlots() {
	s := &c.islands
	awake := c.awakeSlots
	for i := range c.current {
		aux := &c.aux[i]
		if aux.slotA >= awake {
			s.slots.put(c.entries[i].A, aux.slotA)
		}
		if aux.slotB >= awake {
			s.slots.put(c.entries[i].B, aux.slotB)
		}
	}
	for i := range c.held {
		_, target, slot := c.held[i].parties()
		if slot >= awake {
			s.slots.put(target, slot)
		}
	}
}

// returningParty is the slot a party of a returning quiet Contact is solved
// at, and false for a party that has gone or moved. A Static still in the
// static index is −1. A woken member still where it slept is solved at the
// slot this tick already gave it (seedReturningSlots), and otherwise at a slot
// past every one Detect numbered, one per Body however many Contacts name it:
// it is in the sleepers' grid until the next Index, and the sleep System reads
// no index to find it there, so it is numbered here instead and the solver's
// slot table, sized by maxSlot, takes it like any other.
func (c *Contacts) returningParty(
	entry *Contact, static uint8, e ecs.Entity, rests *ecs.Set[Rest], places *ecs.Get[Position],
) (int32, bool) {
	if entry.flags&static != 0 {
		return -1, entry.flags&flagGone == 0
	}
	rest, ok := rests.Ref(e)
	if !ok {
		return 0, false
	}
	place, _ := places.Of(e)
	if rest.Island >= 0 || place.Current != rest.Position.Current || place.Angle != rest.Position.Angle {
		return 0, false
	}
	s := &c.islands
	if slot, found := s.slots.lookup(e); found {
		return slot, true
	}
	slot := c.maxSlot
	c.maxSlot++
	s.slots.put(e, slot)
	return slot, true
}

// pack is the Islands' half of the Contact buffers' shrink: both slabs
// compacted whatever their garbage and cut to what still sleeps, and the
// records and their free list cut to their length. A record is never dropped,
// because a sleeper's Rest names its Island by position.
func (s *islands) pack() {
	s.compactMembers()
	s.compactQuiet()
	s.members, s.memberOwner = clip(s.members), clip(s.memberOwner)
	s.quiet, s.quietOwner = clip(s.quiet), clip(s.quietOwner)
	s.records, s.free = clip(s.records), clip(s.free)
}

// bytes is what the Islands' records and slabs hold, by capacity.
func (s *islands) bytes() uintptr {
	return uintptr(cap(s.records))*unsafe.Sizeof(island{}) +
		uintptr(cap(s.free)+cap(s.memberOwner)+cap(s.quietOwner))*unsafe.Sizeof(int32(0)) +
		uintptr(cap(s.members))*unsafe.Sizeof(ecs.Entity(0)) +
		uintptr(cap(s.quiet))*unsafe.Sizeof(Contact{})
}

// releaseScratch lets go of everything the tick's Island build refills from
// nothing, whole: nothing in it is read across a tick.
func (s *islands) releaseScratch() {
	s.waking, s.nodes, s.parent, s.idle, s.least = nil, nil, nil, nil, nil
	s.counts, s.rootIsland, s.entryIsland, s.fill = nil, nil, nil, nil
	s.tailEntries, s.tailAux, s.ended = nil, nil, nil
	s.slots = entityTable{}
}

// scratchBytes is what the Island build's scratch holds, by capacity.
func (s *islands) scratchBytes() uintptr {
	return uintptr(cap(s.waking)+cap(s.parent)+cap(s.counts)+cap(s.rootIsland)+
		cap(s.entryIsland)+cap(s.fill))*unsafe.Sizeof(int32(0)) +
		uintptr(cap(s.nodes))*unsafe.Sizeof(ecs.Entity(0)) +
		uintptr(cap(s.idle)+cap(s.least))*unsafe.Sizeof(float64(0)) +
		uintptr(cap(s.tailEntries)+cap(s.ended))*unsafe.Sizeof(Contact{}) +
		uintptr(cap(s.tailAux))*unsafe.Sizeof(contactAux{}) +
		s.slots.bytes()
}

// kineticEnergy is cp's Body.KineticEnergy, which has no half, with its fudge
// against a NaN: a term is only formed when its speed is not zero, so a Body
// that does not turn, whose moment is infinite, adds nothing while it is not
// turning.
func kineticEnergy(body *Dynamic, velocity Velocity) float64 {
	vsq := velocity.Linear.Dot(velocity.Linear)
	wsq := velocity.Angular * velocity.Angular
	var linear, angular float64
	if vsq != 0 {
		linear = vsq / body.InvMass
	}
	if wsq != 0 {
		angular = wsq / body.InvInertia
	}
	return linear + angular
}

// idleThreshold is cp's keThreshold: the Body's mass times the squared idle
// speed, and 0 when that speed is 0, so an infinite mass never forms a NaN.
func idleThreshold(body *Dynamic, dvsq float64) float64 {
	if dvsq == 0 {
		return 0
	}
	return dvsq / body.InvMass
}

func resize(s []float64, n int) []float64 {
	if cap(s) < n {
		return make([]float64, n, 2*n)
	}
	return s[:n]
}

func resizeInt32(s []int32, n int) []int32 {
	if cap(s) < n {
		return make([]int32, n, 2*n)
	}
	return s[:n]
}
