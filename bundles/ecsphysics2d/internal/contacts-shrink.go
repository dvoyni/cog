package internal

import (
	"unsafe"

	"github.com/dvoyni/cog/libs/m"
)

// The Contact list's half of ShrinkCmd: the cached entries dropped, both entry
// buffers and both pair tables cut to what is used, and the solver and Probe
// scratch released. The request and the response are in shrink.go.

// shrink cuts the Contact list's three areas and reports the bytes each let go.
//
// The cached run goes first, so the entries it was holding are counted against
// the area that was holding them rather than against the buffers.
func (c *Contacts) shrink(request ShrinkRequest) ShrinkResponse {
	var released ShrinkResponse
	if !request.KeepCached {
		before := c.bytes()
		c.dropCached()
		released.Cached = before - c.bytes()
	}
	if !request.KeepContacts {
		before := c.bytes()
		c.clipBuffers()
		released.Contacts = before - c.bytes()
	}
	if !request.KeepScratch {
		before := c.scratchBytes()
		c.releaseScratch()
		released.Scratch = before - c.scratchBytes()
	}
	return released
}

// dropCached forgets every cached entry and cuts the buffer to the entries the
// app can still see.
//
// A cached entry is past the public view and carries nothing but Impulses for a
// pair that is not touching, so dropping one changes no answer a System can
// read; what it costs is the warm start that pair would have had if it came
// back inside the persistence window. That is the trade the command is for, and
// it is the app's to make.
//
// This tick's pair table is rebuilt over what survives, because detection looks
// the previous tick's pairs up through it and a stale slot would point past the
// end of the buffer.
func (c *Contacts) dropCached() {
	if len(c.entries) == c.visible {
		return
	}
	c.entries = clip(c.entries[:c.visible])
	c.aux = clip(c.aux[:c.visible])
	c.lookup.clear()
	for i := range c.entries {
		c.lookup.insert(c.entries[i].A, c.entries[i].B, int32(i))
	}
}

// clipBuffers cuts both entry buffers and both pair tables to what they hold.
//
// Only one of the two buffers holds anything a later tick reads. The other is
// the tick before's, which endTick has already walked and which the next
// beginTick truncates before detection writes a thing into it, so it is
// released whole rather than clipped; the same goes for the pair table beside
// it. The frame after a shrink regrows them, and that frame allocates, which is
// what the command's documentation says it does.
//
// The sleeping Islands' two slabs, their members and their quiet Contacts, are
// Contact-list buffers too and are packed here: what woke is dropped, and what
// still sleeps is moved down and kept.
func (c *Contacts) clipBuffers() {
	c.entries = clip(c.entries)
	c.aux = clip(c.aux)
	c.previous, c.prevAux = nil, nil
	c.prevLookup = pairTable{}
	c.lookup.shrink()
	c.islands.pack()
}

// bytes is what the Contact list's buffers and tables hold, by capacity.
func (c *Contacts) bytes() uintptr {
	return uintptr(cap(c.entries)+cap(c.previous))*unsafe.Sizeof(Contact{}) +
		uintptr(cap(c.aux)+cap(c.prevAux))*unsafe.Sizeof(contactAux{}) +
		c.lookup.bytes() + c.prevLookup.bytes() + c.islands.bytes()
}

// releaseScratch lets go of the solver's gather and the swept Sensor Probe
// buffer, whole.
//
// None of it carries anything from one tick to the next. beginSolve truncates
// the solved list and the gather and refills the slot table with −1 before it
// reads a thing, the Joint walk resets its rows and its Body table, and
// detection truncates the Probe buffer before each Sensor's sweep. So, like the
// tick before's entry buffer, it is released rather than clipped: a clip would
// keep what a spike's last tick happened to leave in it, which the next tick
// throws away. The slot table is the one that matters — it is sized to the
// largest BodyIndex slot detection ever saw, and nothing else ever cuts it.
//
// The path pass's scratch goes with it: the stopped Bodies' copies' world
// caches, the run a fast Body's Shape is Probed in, and the earliest stops,
// meetings and partners it finds, which only detection reads. The run of stops
// is clipped rather than released, because Solve reads it after Detect within
// the same tick, and so are the run of Hits held past each stop, which the
// sleep System and Solve read, and the Sensor crossings, which keep the ones
// past each stop for Solve; the scratch Solve writes the carried ones with is
// released.
//
// The previous tick's step stays, because it is not scratch: the warm start
// scales the cached Impulses by it, and dropping it would change the next
// tick's solution.
func (c *Contacts) releaseScratch() {
	c.probes, c.probeSlots = nil, nil
	c.stopWorld, c.mover = nil, nil
	c.firsts, c.meetings, c.partners = nil, nil, nil
	c.stops, c.held, c.crossings = clip(c.stops), clip(c.held), clip(c.crossings)
	c.carried, c.carriedTail, c.carriedAux, c.givingWay = nil, nil, nil, nil
	s := &c.solver
	s.solved, s.slotDense, s.rows = nil, nil, nil
	s.joints.rows = nil
	s.joints.bodies = entityTable{}
	c.islands.releaseScratch()
}

// scratchBytes is what the solver's gather and the Probe buffers hold, by
// capacity.
func (c *Contacts) scratchBytes() uintptr {
	s := &c.solver
	return uintptr(cap(c.probes))*unsafe.Sizeof(Hit{}) +
		uintptr(cap(c.probeSlots))*unsafe.Sizeof(int32(0)) +
		uintptr(cap(c.stops))*unsafe.Sizeof(stop{}) +
		uintptr(cap(c.stopWorld)+cap(c.mover))*unsafe.Sizeof(m.Vec2d{}) +
		uintptr(cap(c.firsts))*unsafe.Sizeof(firstStop{}) +
		uintptr(cap(c.meetings))*unsafe.Sizeof(meeting{}) +
		uintptr(cap(c.partners))*unsafe.Sizeof(partner{}) +
		uintptr(cap(c.crossings))*unsafe.Sizeof(crossing{}) +
		uintptr(cap(c.held))*unsafe.Sizeof(heldHit{}) +
		uintptr(cap(c.carried)+cap(c.givingWay))*unsafe.Sizeof(int32(0)) +
		uintptr(cap(c.carriedTail))*unsafe.Sizeof(Contact{}) +
		uintptr(cap(c.carriedAux))*unsafe.Sizeof(contactAux{}) +
		uintptr(cap(s.solved)+cap(s.slotDense))*unsafe.Sizeof(int32(0)) +
		uintptr(cap(s.rows))*unsafe.Sizeof(solverBody{}) +
		uintptr(cap(s.joints.rows))*unsafe.Sizeof(jointRow{}) +
		uintptr(cap(s.joints.bodies.cells))*unsafe.Sizeof(entityCell{}) +
		c.islands.scratchBytes()
}
