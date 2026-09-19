package types

import "unsafe"

// The Contact list's half of ShrinkCmd: the cached entries dropped, and both
// entry buffers and both pair tables cut to what is used. The request and the
// response are in shrink.go.

// shrink cuts the Contact list's two areas and reports the bytes each let go.
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
func (c *Contacts) clipBuffers() {
	c.entries = clip(c.entries)
	c.aux = clip(c.aux)
	c.previous, c.prevAux = nil, nil
	c.prevLookup = pairTable{}
	c.lookup.shrink()
}

// bytes is what the Contact list's buffers and tables hold, by capacity.
func (c *Contacts) bytes() uintptr {
	return uintptr(cap(c.entries)+cap(c.previous))*unsafe.Sizeof(Contact{}) +
		uintptr(cap(c.aux)+cap(c.prevAux))*unsafe.Sizeof(contactAux{}) +
		c.lookup.bytes() + c.prevLookup.bytes()
}
