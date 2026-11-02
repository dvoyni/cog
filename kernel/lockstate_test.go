package kernel

import "testing"

// locksOf builds taskLocks reading and writing the given resource indexes.
func locksOf(reads, writes []int) *taskLocks {
	locks := &taskLocks{}
	grow := func(index int) {
		for len(locks.writes) <= index/64 {
			locks.reads = append(locks.reads, 0)
			locks.writes = append(locks.writes, 0)
		}
	}
	for _, index := range reads {
		grow(index)
		locks.reads[index/64] |= 1 << (index % 64)
	}
	for _, index := range writes {
		grow(index)
		locks.writes[index/64] |= 1 << (index % 64)
	}
	return locks
}

func TestReadersShareAResource(t *testing.T) {
	s := makeLockState(100)
	reader := locksOf([]int{70}, nil)
	s.lock(reader)
	if !s.canLock(reader) {
		t.Fatal("a second reader was refused")
	}
	s.lock(reader)
	writer := locksOf(nil, []int{70})
	if s.canLock(writer) {
		t.Fatal("a writer was granted a resource being read")
	}
	s.unlock(reader)
	if s.canLock(writer) {
		t.Fatal("a writer was granted a resource one reader still reads")
	}
	s.unlock(reader)
	if !s.canLock(writer) {
		t.Fatal("a writer was refused once every reader finished")
	}
}

func TestWriterExcludesEveryone(t *testing.T) {
	s := makeLockState(10)
	writer := locksOf(nil, []int{3})
	s.lock(writer)
	if s.canLock(locksOf([]int{3}, nil)) || s.canLock(writer) {
		t.Fatal("a resource being written was granted again")
	}
	if !s.canLock(locksOf([]int{4}, []int{5})) {
		t.Fatal("other resources were refused")
	}
	s.unlock(writer)
	if !s.canLock(writer) {
		t.Fatal("an unlocked resource was refused")
	}
}

func TestReservationBlocksLaterJobs(t *testing.T) {
	s := makeLockState(10)
	s.reserve(locksOf(nil, []int{2}))
	if s.canLock(locksOf([]int{2}, nil)) {
		t.Fatal("a reader was granted a resource a writer is next for")
	}
	s.clearReservations()
	if !s.canLock(locksOf([]int{2}, nil)) {
		t.Fatal("a cleared reservation still blocked")
	}
}

func TestShorterMasksLockNothingBeyond(t *testing.T) {
	s := makeLockState(200)
	s.lock(locksOf(nil, []int{150}))
	if !s.canLock(locksOf([]int{1}, []int{2})) {
		t.Fatal("a task whose masks end before word 2 was refused for a lock there")
	}
}
