package types

import (
	"fmt"
	"reflect"
	"unsafe"
)

// Changed is a difference in bytes, not a write: hooks.md § Changed is a
// difference in bytes, which this file is judged against.
//
// On a Store watched for Changed, a System handed rows with write access copies
// them before it can write them, and compares the copy with the Store when its
// run ends. A *T Query field copies the whole Store when the Query binds, which
// is exact for a System holding write{T}: a row it was not handed cannot
// change. Set.Ref and a replacing Set.UpdateFor copy the one row, once per run.
// At the run end, each copied row whose bytes differ appends one Changed record,
// under the write{T} the System already holds, naming the System so its own
// Hooks readers skip it.
//
// The copy is bytes, whatever T holds. It is only ever compared, never read back
// as a T, so a string header or a List header in it keeps nothing alive and
// needs no write barrier.

// rowCopy is one System's copy of the rows it was handed with write access on
// one Store, and every handle the System holds on that Store shares it: a row
// reached through a *T field, Ref and UpdateFor in one run is compared once, and
// records at most one Changed.
//
// Its buffers are allocated on the System's first watched run and kept, so
// steady state allocates nothing.
type rowCopy struct {
	// gate is the System's check, at each run start, of whether the Store is
	// watched for Changed. A Component with no fields never records Changed,
	// so its mask is empty and it is never on.
	gate  hookGate
	store *storeHeader
	size  uintptr
	// writer is the System, named on every Changed record this copy appends.
	writer uint32
	// run is this run's stamp: a row whose index is stamped with it has been
	// copied this run already.
	run uint32
	// taken says a row was copied this run, and whole that the whole Store was.
	taken bool
	whole bool
	// owners and rows are the copies, one Entity and one row's bytes each, in
	// the order they were taken; for a whole copy, in the Store's row order.
	owners []Entity
	rows   []byte
	stamps []uint32
}

// newRowCopy is a handle's own row copy of class's Store, made when the handle
// registers. Its System replaces it with the one it shares on that Store.
func newRowCopy(class *componentClass) *rowCopy {
	mask := kindChanged
	if class.size == 0 {
		mask = 0
	}
	return &rowCopy{
		gate:  hookGate{watch: &class.header.watch, mask: mask},
		store: class.header,
		size:  class.size,
		run:   1,
	}
}

// rowCopier is a System parameter handed rows with write access. It shows its
// System each row copy it holds, one per Store, so the System can make every
// handle on one Store share one.
type rowCopier interface {
	rowCopies(each func(slot **rowCopy))
}

// take copies e's row before the caller writes it, once per run. The caller has
// checked the gate is on, and holds the Store's write lock.
//
// A row whose index was already copied this run is not copied again, even if
// the Entity at that index is a new one: an Entity that reused an index during
// the run gained T through a Spawn or an UpdateFor this run, and that addition
// already carries Changed.
func (c *rowCopy) take(e Entity, row uint32) {
	if c.whole {
		return
	}
	index := e.idx()
	if int(index) >= len(c.stamps) {
		c.stamps = append(c.stamps, make([]uint32, max(int(index)+1, len(c.store.sparse))-len(c.stamps))...)
	}
	if c.stamps[index] == c.run {
		return
	}
	c.stamps[index] = c.run
	c.taken = true
	c.owners = append(c.owners, e)
	c.rows = append(c.rows, unsafe.Slice((*byte)(unsafe.Add(c.store.dense.data, uintptr(row)*c.size)), c.size)...)
}

// takeWhole copies the whole Store, once per run, when a Query holding a *T
// field binds. A row already copied this run keeps the bytes it was copied
// with, because the System may have written it since.
func (c *rowCopy) takeWhole() {
	if c.whole {
		return
	}
	store, size := c.store, c.size
	earlier := len(c.owners)
	c.owners = append(c.owners, store.owners...)
	c.rows = append(c.rows, unsafe.Slice((*byte)(store.dense.data), uintptr(len(store.owners))*size)...)
	for i := range earlier {
		if row, ok := store.probe(c.owners[i]); ok {
			at := uintptr(earlier+int(row)) * size
			copy(c.rows[at:at+size], c.rows[uintptr(i)*size:uintptr(i+1)*size])
		}
	}
	n := copy(c.owners, c.owners[earlier:])
	c.owners = c.owners[:n]
	copy(c.rows, c.rows[uintptr(earlier)*size:])
	c.rows = c.rows[:uintptr(n)*size]
	c.taken, c.whole = true, true
}

// compare is the System's run end on this Store: each copied row whose Entity
// still holds T and whose bytes differ appends one Changed record. A row whose
// Entity lost T during the run compares against nothing and records nothing,
// so a change followed by a removal records only the removal.
//
// It then empties the copy for the next run, keeping its capacity.
func (c *rowCopy) compare() {
	if !c.taken {
		return
	}
	store, size := c.store, c.size
	log := store.hooks
	live := unsafe.Slice((*byte)(store.dense.data), uintptr(len(store.owners))*size)
	if c.whole && sameEntities(store.owners, c.owners) {
		// Nothing was added, removed or moved, so the copy lines up with the
		// Store row for row, and a run of equal rows is passed a block at a
		// time.
		const block = 64
		for start := 0; start < len(c.owners); start += block {
			end := min(start+block, len(c.owners))
			lo, hi := uintptr(start)*size, uintptr(end)*size
			if string(live[lo:hi]) == string(c.rows[lo:hi]) {
				continue
			}
			for row := start; row < end; row++ {
				lo, hi := uintptr(row)*size, uintptr(row+1)*size
				if string(live[lo:hi]) != string(c.rows[lo:hi]) {
					log.changed(c.owners[row], c.writer)
				}
			}
		}
	} else {
		for i, e := range c.owners {
			row, ok := store.probe(e)
			if !ok {
				continue
			}
			at := uintptr(row) * size
			if string(live[at:at+size]) != string(c.rows[uintptr(i)*size:uintptr(i+1)*size]) {
				log.changed(e, c.writer)
			}
		}
	}
	if validate && len(store.lists) > 0 {
		// Each compared row's Lists are registered to it: ecs.md § Validation
		// mode, the second owner.
		holdLists(store, size, c.owners)
	}
	c.owners, c.rows = c.owners[:0], c.rows[:0]
	c.taken, c.whole = false, false
	c.run++
	if c.run == 0 {
		clear(c.stamps)
		c.run = 1
	}
}

// implicitPadding reports the first implicit padding in t's layout, in memory
// order: where it is, as "after field F" or "at the end", and how many bytes it
// is. It walks nested structs and arrays; an array has no padding between its
// elements, so its element is walked once. A List's elements live outside the
// row, so a List is only its header here. It reports 0 bytes for a layout with
// none.
//
// It is the Validation check behind hooks.md's padding rule: a Component some
// reader watches for Changed has no implicit padding, because a Changed compare
// reads every byte of a row and Go leaves padding holding whatever was there.
// Explicit _ [N]byte fields carry no garbage, measured on every write route.
// Only a validating build calls it, when a reader watching Changed registers.
func implicitPadding(t reflect.Type) (where string, bytes uintptr) {
	return paddingIn(t, "")
}

func paddingIn(t reflect.Type, path string) (string, uintptr) {
	switch t.Kind() {
	case reflect.Array:
		if t.Len() == 0 {
			return "", 0
		}
		return paddingIn(t.Elem(), path+"[_]")
	case reflect.Struct:
		end, last := uintptr(0), ""
		for i := range t.NumField() {
			field := t.Field(i)
			name := field.Name
			if name == "_" {
				name = fmt.Sprintf("_ (at byte %d)", field.Offset)
			}
			if path != "" {
				name = path + "." + name
			}
			if field.Offset > end {
				return "after field " + last, field.Offset - end
			}
			if where, bytes := paddingIn(field.Type, name); bytes > 0 {
				return where, bytes
			}
			end, last = field.Offset+field.Type.Size(), name
		}
		// A trailing field of zero size leaves tail padding too: Go pads the
		// struct so that field's address stays inside it.
		if t.Size() > end {
			if path == "" {
				return "at the end", t.Size() - end
			}
			return "at the end of " + path, t.Size() - end
		}
	}
	return "", 0
}

// sameEntities reports whether two owners arrays hold the same Entities in the
// same order.
func sameEntities(a, b []Entity) bool {
	if len(a) != len(b) {
		return false
	}
	n := uintptr(len(a)) * unsafe.Sizeof(Entity(0))
	return string(unsafe.Slice((*byte)(unsafe.Pointer(unsafe.SliceData(a))), n)) ==
		string(unsafe.Slice((*byte)(unsafe.Pointer(unsafe.SliceData(b))), n))
}
