package kernel

// lockSet is a handler's lock set compiled for the scheduler: the dense track
// index of every key it reads and every key it writes, each slice sorted.
// ResourceAccess keeps the reflect.Type maps for Describe and the contention
// report; the scheduler sees only this, so a dispatch never hashes a type.
//
// Finalization fills it in place, and every holder points at the one inside its
// ResourceAccess, so a pointer taken before compilation reads the compiled set.
type lockSet struct {
	read, write []int32
	// span is one past the highest index in either slice: how many tracks the
	// scheduler must have before this set can be submitted.
	span int32
}

// noLockSet is the empty set of a declared nested dispatch, which runs under its
// caller's locks and asks for none of its own.
var noLockSet = &lockSet{}

func (l *lockSet) empty() bool { return len(l.read) == 0 && len(l.write) == 0 }
