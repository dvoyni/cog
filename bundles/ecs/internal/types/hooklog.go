package types

import (
	"sync"
	"unsafe"
)

// hookRecord is one act in a Store's log, 24 bytes: the full Entity, generation
// included, so a despawned Entity and the one reusing its index are never
// folded together; where a removal's copy of T sits among the log's retained
// values, or -1; the System whose run end recorded a change; and the act's
// kinds.
type hookRecord struct {
	e Entity
	// value is the absolute position of a removal's retained copy, counted
	// from the first value the log ever retained, so compaction moves no index.
	value int64
	// writer is the System that recorded a change, and 0 on every other record:
	// a reader skips the changes its own System recorded. It sits in what was
	// the record's padding.
	writer uint32
	kinds  hookKind
}

// hookRecords is the half of a Store's log that knows no T: the records, and
// the readers' places in them. hookLog[T] begins with it, so the erased Store
// header reaches it through the same pointer, which is how a Changed compare
// that knows its Store only as bytes appends to it.
type hookRecords struct {
	mu      sync.Mutex
	records []hookRecord
	// base is the absolute position of records[0].
	base uint64
	// places is each reader's absolute position: the first record its next run
	// start takes.
	places []*uint64
	// runs is Validation mode's count of the runs that appended to the log,
	// each counted once at its System's run end. A release build never reads
	// or writes it. See systempace.go.
	runs uint64
}

// changed records a change a writer's run end found in e's bytes. The caller
// holds the Store's write lock, and has checked the Store watches changes.
func (r *hookRecords) changed(e Entity, writer uint32) {
	r.records = append(r.records, hookRecord{e: e, value: -1, writer: writer, kinds: kindChanged})
}

// hookLog is one watched Store's record of acts. Writers append to records and
// retained under the Store's write lock and never touch mu. Readers hold the
// Store's read lock for their whole run, so no append lands while one reads;
// mu is what two readers of the same Store, running together, share while they
// fold and compact. It is never held by a writer or across a System's body.
type hookLog[T any] struct {
	// hookRecords is first, and must stay first: see hookRecords.
	hookRecords
	// retained is the removals' copies of T, and retainedBase the absolute
	// position of retained[0].
	retained     []T
	retainedBase int64
	// trivial is T's pointer-free answer. A retained copy of a non-trivial T is
	// zeroed when compaction drops it, so a string or a List it holds is
	// released.
	trivial bool
}

// added records an addition. The caller has checked the Store watches it.
func (l *hookLog[T]) added(e Entity, kinds hookKind) {
	l.records = append(l.records, hookRecord{e: e, value: -1, kinds: kinds})
}

// removed records a removal and retains T's last value, taken at the act. The
// caller has checked the Store watches it.
func (l *hookLog[T]) removed(e Entity, row *T, kinds hookKind) {
	l.retained = append(l.retained, *row)
	l.records = append(l.records, hookRecord{
		e: e, value: l.retainedBase + int64(len(l.retained)-1), kinds: kinds,
	})
}

// compact drops every record all readers have passed, and the retained values
// only those records named. Capacity is kept until ShrinkCmd cuts it, so steady
// state never allocates. Held under mu.
func (l *hookLog[T]) compact() {
	least := l.base + uint64(len(l.records))
	for _, place := range l.places {
		least = min(least, *place)
	}
	passed := int(least - l.base)
	if passed == 0 {
		return
	}
	firstKept := l.retainedBase + int64(len(l.retained))
	for i := passed; i < len(l.records); i++ {
		if l.records[i].value >= 0 {
			firstKept = l.records[i].value
			break
		}
	}
	kept := copy(l.retained, l.retained[firstKept-l.retainedBase:])
	if !l.trivial {
		clear(l.retained[kept:])
	}
	l.retained = l.retained[:kept]
	l.retainedBase = firstKept
	kept = copy(l.records, l.records[passed:])
	l.records = l.records[:kept]
	l.base = least
}

// shrink cuts the records and the retained values to their length, and reports
// the bytes let go. What no reader has passed is the length, so every record a
// reader has yet to take survives, with its value, at the same absolute
// position; compaction has already dropped the rest. An array already at its
// length is left as it is, so a second shrink releases nothing.
//
// Only ShrinkCmd calls it, holding write{*Entities}, which excludes every writer
// that appends and every reader that folds or compacts, so mu is not taken.
func (l *hookLog[T]) shrink() uintptr {
	before := l.bytes()
	l.records = clip(l.records)
	l.retained = clip(l.retained)
	return before - l.bytes()
}

// bytes is what the log's records and retained values hold, by capacity.
func (l *hookLog[T]) bytes() uintptr {
	return uintptr(cap(l.records))*unsafe.Sizeof(hookRecord{}) +
		uintptr(cap(l.retained))*unsafe.Sizeof(*new(T))
}
