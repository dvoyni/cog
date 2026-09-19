package types

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
