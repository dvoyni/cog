package internal

// listMode is what a stamp records about the last handle a List's backing array
// was reached through, and it is the whole of what the write check consults.
//
// The five are not a hierarchy. modeStored says the array is in the world and
// the only legal route to it is a write-locked handle; modeRead says the run
// that handed it out held a read; modeWrite says the run held a write, and is
// the one mode a write is legal under; modeSetOf says a write-locked handle
// handed out a copy rather than the row; modeHook says a Hooks reader handed it
// out in a record's value.
type listMode uint8

const (
	// modeStored is stamped when a value enters a Store. It is what closes the
	// alias a constructor leaves behind: m.ListOf copies, so the List a caller
	// builds is its own, but Set copies the header into the Store and the two
	// then share one array. Stamping on the way in makes the caller's retained
	// copy read-only from that moment, which is the rule the world needs and
	// the one a caller has no other way to learn.
	modeStored listMode = iota
	// modeRead is stamped by a read field's fill and by Get.Of.
	modeRead
	// modeWrite is stamped by a write field's fill and by Set.Ref.
	modeWrite
	// modeSetOf is stamped by Set.Of. Its copy shares the stored backing array
	// but not the row, so a Set through it would change the elements without
	// the generation in the stored Component's header, and no byte compare
	// would see the write. Set.Of stamped modeWrite until Hooks; Ref is the
	// route to a List a System means to write.
	modeSetOf
	// modeHook is stamped by a Hooks reader on every List in a record's value
	// it fills or folds. An addition's or a change's value shares the stored
	// row's arrays, and a removal's is shared by every reader's copy, so a Set
	// through either writes memory other Systems read under read{T}. It is tied
	// to no run: a Set through a value kept past the run panics the same way.
	// modeRead's message names *T as the fix, which is the wrong one for a Hook.
	modeHook
)
