package internal

import (
	"testing"
)

// TestTheZeroRequestGivesTheContactBuffersBack is the Contact half: a spike of
// touching pairs grows both buffers and both pair tables, and the zero request
// cuts them to what the quiet tick after it holds.
func TestTheZeroRequestGivesTheContactBuffersBack(t *testing.T) {
	contacts, spike := spikedContacts(t, 512, 4)
	if spike == 0 {
		t.Fatal("the spiked Contact list never held an entry, so there is no spike to shrink")
	}

	released := contacts.shrink(ShrinkRequest{})
	t.Logf("a %d entry spike down to %d released %d bytes of buffer and %d of cached run",
		spike, contacts.Len(), released.Contacts, released.Cached)
	if released.Contacts == 0 {
		t.Error("the zero request released no Contact buffer bytes after a spike")
	}
	if got := contacts.Len(); got != 4 {
		t.Fatalf("the shrunk list shows %d Contacts, want the 4 the quiet tick found", got)
	}
	if cap(contacts.entries) != len(contacts.entries) {
		t.Errorf("the shrunk buffer holds %d entries at a length of %d, want no slack",
			cap(contacts.entries), len(contacts.entries))
	}
	if contacts.previous != nil {
		t.Errorf("the buffer the next tick refills from empty was kept, at %d entries", cap(contacts.previous))
	}
}

// TestTheCachedRunIsAnAreaOfItsOwn pins the opt-out the persistence window
// needs: the cached entries are past the public view, so keeping them changes
// nothing a System sees and dropping them changes nothing a System sees either
// — what it changes is whether a pair that comes back is warm started.
func TestTheCachedRunIsAnAreaOfItsOwn(t *testing.T) {
	kept, _ := spikedContacts(t, 64, 0)
	cached := len(kept.entries) - kept.visible
	if cached == 0 {
		t.Fatal("the spiked Contact list carries no cached entry, so there is no cached run to shrink")
	}

	released := kept.shrink(ShrinkRequest{KeepCached: true})
	if released.Cached != 0 {
		t.Errorf("a kept cached run released %d bytes", released.Cached)
	}
	if got := len(kept.entries) - kept.visible; got != cached {
		t.Errorf("a kept cached run carries %d entries, want the %d it carried", got, cached)
	}

	dropped, _ := spikedContacts(t, 64, 0)
	released = dropped.shrink(ShrinkRequest{KeepContacts: true})
	if released.Cached == 0 {
		t.Error("the cached run was not kept and released nothing")
	}
	if got := len(dropped.entries) - dropped.visible; got != 0 {
		t.Errorf("the shrunk cached run carries %d entries, want none", got)
	}
	if dropped.Len() != kept.Len() {
		t.Errorf("dropping the cached run changed the public view from %d entries to %d",
			kept.Len(), dropped.Len())
	}
}

// TestAShrunkContactListFindsItsPairsAgain is the hazard dropping the cached
// run has to clear: this tick's pair table names every entry by slot, and an
// entry dropped from under it would have the next tick's detection read past
// the end of the buffer.
func TestAShrunkContactListFindsItsPairsAgain(t *testing.T) {
	contacts, _ := spikedContacts(t, 64, 4)
	contacts.shrink(ShrinkRequest{})

	// The table this tick built is what the next tick looks pairs up in, so it
	// is checked as the next tick's beginTick leaves it.
	contacts.beginTick()
	for i := range contacts.previous {
		entry := &contacts.previous[i]
		slot, ok := contacts.prevLookup.find(entry.A, entry.B)
		if !ok {
			t.Fatalf("the shrunk list lost the pair %v/%v", entry.A, entry.B)
		}
		if int(slot) != i {
			t.Fatalf("the shrunk list puts the pair %v/%v at slot %d, want %d", entry.A, entry.B, slot, i)
		}
	}
	if _, ok := contacts.prevLookup.find(testEntity(9000), testEntity(9001)); ok {
		t.Fatal("the shrunk table answers for a pair it never held")
	}
}

// spikedContacts runs a Contact list through a tick of spike touching pairs and
// then two quiet ticks, and reports the list and how many entries the spike
// held. The pairs the spike found and the quiet ticks did not report Ended on
// the second tick and are carried cached on the third, which is what leaves
// entries in all three runs at once.
func spikedContacts(t testing.TB, spike, quiet int) (*Contacts, int) {
	t.Helper()
	contacts := NewContacts(1)

	contacts.beginTick()
	for i := range spike {
		contacts.append(Contact{A: testEntity(i), B: testEntity(spike + i), Phase: PhaseBegan},
			contactAux{slotA: int32(i), slotB: int32(spike + i)})
	}
	held := len(contacts.entries)
	contacts.endTick(8)

	for range 2 {
		contacts.beginTick()
		for i := range quiet {
			contacts.append(Contact{A: testEntity(i), B: testEntity(spike + i), Phase: PhaseContinuing},
				contactAux{slotA: int32(i), slotB: int32(spike + i)})
			contacts.prevAux[i].matched = true
		}
		contacts.endTick(8)
	}
	return contacts, held
}

// TestTheZeroRequestGivesTheSolverScratchBack is the fifth area: the gather
// Solve refills from nothing every tick, and the swept Sensor Probe buffer
// detection refills once a Sensor. None of it is read across a tick, so the
// zero request releases all of it, and the slot table a 100 000 Body spike
// sized is the bulk of that.
func TestTheZeroRequestGivesTheSolverScratchBack(t *testing.T) {
	const spike = 100_000
	contacts := scratchedContacts(spike)
	held := contacts.scratchBytes()
	solverStep := contacts.solver.step

	released := contacts.shrink(ShrinkRequest{})
	t.Logf("a %d slot spike released %d bytes of solver and Probe scratch", spike, released.Scratch)
	if released.Scratch < spike*4 {
		t.Errorf("the zero request released %d scratch bytes after a %d slot spike, want at least the %d its slot table needed",
			released.Scratch, spike, spike*4)
	}
	if released.Scratch != held {
		t.Errorf("the zero request released %d scratch bytes of the %d held", released.Scratch, held)
	}
	if left := contacts.scratchBytes(); left != 0 {
		t.Errorf("the shrunk scratch still holds %d bytes", left)
	}
	if contacts.solver.step != solverStep {
		t.Errorf("the shrink changed the warm start's previous step from %v to %v", solverStep, contacts.solver.step)
	}

	// The tick after regrows it, which is what the command says it does.
	contacts.maxSlot = 4
	contacts.beginSolve()
	if len(contacts.solver.slotDense) != 4 || len(contacts.solver.rows) != 1 {
		t.Errorf("the tick after a shrink gathered %d slots into %d rows, want 4 and the one immovable row",
			len(contacts.solver.slotDense), len(contacts.solver.rows))
	}
}

// TestKeepScratchOptsTheSolverScratchOut pins the fifth opt-out, and that it
// is an area of its own: keeping it leaves every buffer where it was, and
// keeping the other four still shrinks it.
func TestKeepScratchOptsTheSolverScratchOut(t *testing.T) {
	kept := scratchedContacts(4096)
	held := kept.scratchBytes()
	released := kept.shrink(ShrinkRequest{KeepScratch: true})
	if released.Scratch != 0 {
		t.Errorf("a kept scratch area released %d bytes", released.Scratch)
	}
	if got := kept.scratchBytes(); got != held {
		t.Errorf("a kept scratch area holds %d bytes, want the %d it held", got, held)
	}

	alone := scratchedContacts(4096)
	released = alone.shrink(ShrinkRequest{KeepContacts: true, KeepCached: true})
	if released.Scratch != held || released.Contacts != 0 || released.Cached != 0 {
		t.Errorf("shrinking the scratch alone released %+v, want %d scratch bytes and nothing else", released, held)
	}
}

// TestASecondContactShrinkReleasesNothingAndAllocatesNothing is the idempotence
// over every area the Contact list owns, the scratch included: the first
// request takes the slack, the second finds none and costs no allocation.
func TestASecondContactShrinkReleasesNothingAndAllocatesNothing(t *testing.T) {
	contacts, _ := spikedContacts(t, 512, 4)
	grown := scratchedContacts(4096)
	contacts.solver, contacts.probes, contacts.maxSlot = grown.solver, grown.probes, grown.maxSlot
	if contacts.scratchBytes() == 0 || len(contacts.entries) == 0 {
		t.Fatal("the Contact list holds neither entries nor scratch, so a second shrink proves nothing")
	}

	contacts.shrink(ShrinkRequest{})
	again := contacts.shrink(ShrinkRequest{})
	if again != (ShrinkResponse{}) {
		t.Errorf("a second shrink released %+v, want nothing", again)
	}
	if allocs := testing.AllocsPerRun(100, func() { contacts.shrink(ShrinkRequest{}) }); allocs != 0 && !raceEnabled {
		t.Errorf("a second shrink allocates %.0f objects, want none", allocs)
	}
}

// scratchedContacts is a Contact list whose scratch a tick of slots Bodies
// grew: the slot table and the gather through beginSolve, a Joint's row and
// Body table through the two buffers the Joint walk fills, and the Probe buffer
// through a Sensor's Hits.
func scratchedContacts(slots int) *Contacts {
	contacts := NewContacts(1)
	contacts.maxSlot = int32(slots)
	contacts.beginSolve()
	for i := range 64 {
		contacts.solver.assign(int32(i), testEntity(i))
		contacts.solver.solved = append(contacts.solver.solved, int32(i))
		contacts.solver.joints.bodies.put(testEntity(i), int32(i+1))
		contacts.probes = append(contacts.probes, Hit{Entity: testEntity(i)})
		contacts.probeSlots = append(contacts.probeSlots, int32(i))
	}
	contacts.solver.joints.rows = append(contacts.solver.joints.rows, jointRow{})
	contacts.solver.step = 1.0 / 60
	return contacts
}
