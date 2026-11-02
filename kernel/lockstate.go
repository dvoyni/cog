package kernel

import "math/bits"

// lockState is which of a kernel's resources are locked right now, one bit
// per resource as in a task's masks. Only the lock combiner touches it, so it
// needs no atomics.
type lockState struct {
	// writeHeld is every resource a running job writes.
	writeHeld []uint64
	// readHeld is every resource at least one running job reads, and readers
	// how many do, per resource, so a read lock is held until its last reader
	// finishes.
	readHeld []uint64
	readers  []int32
	// reserved is every resource a waiting writer is next for. A job behind
	// that writer may not lock it, so a stream of readers cannot starve the
	// writer. It is rebuilt on every pass over the waiting jobs.
	reserved []uint64
}

// makeLockState returns a lockState for resources resources, none locked.
func makeLockState(resources int) lockState {
	words := (resources + 63) / 64
	return lockState{
		writeHeld: make([]uint64, words),
		readHeld:  make([]uint64, words),
		readers:   make([]int32, resources),
		reserved:  make([]uint64, words),
	}
}

// canLock reports whether every lock of locks is free: nothing it touches is
// written or reserved, and nothing it writes is read.
func (s *lockState) canLock(locks *taskLocks) bool {
	var conflict uint64
	for i := range locks.writes {
		conflict |= (s.writeHeld[i] | s.reserved[i]) & (locks.reads[i] | locks.writes[i])
		conflict |= s.readHeld[i] & locks.writes[i]
	}
	return conflict == 0
}

// lock locks every resource of locks. canLock must have answered true.
func (s *lockState) lock(locks *taskLocks) {
	for i := range locks.writes {
		s.writeHeld[i] |= locks.writes[i]
		s.readHeld[i] |= locks.reads[i]
		for word := locks.reads[i]; word != 0; word &= word - 1 {
			s.readers[i*64+bits.TrailingZeros64(word)]++
		}
	}
}

// unlock frees every resource of locks, as a job that locked them finishes.
func (s *lockState) unlock(locks *taskLocks) {
	for i := range locks.writes {
		s.writeHeld[i] &^= locks.writes[i]
		for word := locks.reads[i]; word != 0; word &= word - 1 {
			bit := bits.TrailingZeros64(word)
			index := i*64 + bit
			s.readers[index]--
			if s.readers[index] == 0 {
				s.readHeld[i] &^= 1 << bit
			}
		}
	}
}

// reserve keeps every resource locks writes for it, against the jobs waiting
// behind it.
func (s *lockState) reserve(locks *taskLocks) {
	for i := range locks.writes {
		s.reserved[i] |= locks.writes[i]
	}
}

// clearReservations drops every reservation, before a new pass over the
// waiting jobs.
func (s *lockState) clearReservations() {
	clear(s.reserved)
}
