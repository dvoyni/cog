package sfx

// FlushChanges is the changes one flush hands the backend, in one Emit. It is
// owned by sfx and valid only for the duration of that call; a backend that
// needs it after the call copies it.
//
// A backend applies the stops first, then the starts, then the updates. The
// fields are not commutative on one slot, and a steal states both about it in
// one flush's changes: the victim's stop, and the start that takes the slot
// over.
//
// Destroys is the only way sfx releases a clip. A destroy is guaranteed to
// arrive after the stops that precede it, so a backend frees a clip once its
// mixer has passed these changes, and never while it still mixes it.
type FlushChanges struct {
	Starts   []VoiceStart
	Updates  []VoiceUpdate
	Stops    []VoiceSlot // applied first, before the starts that reuse their slots
	Destroys []ClipID    // ordered after the stops that precede them
}

// reset empties the changes for the next flush, keeping their capacity, so a
// warm flush builds them with no allocation.
func (c *FlushChanges) reset() {
	c.Starts = c.Starts[:0]
	c.Updates = c.Updates[:0]
	c.Stops = c.Stops[:0]
	c.Destroys = c.Destroys[:0]
}
