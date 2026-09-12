package ecs

import (
	"fmt"
	"iter"
	"reflect"
	"unsafe"

	"github.com/dvoyni/cog/kernel"
)

// Query is a System's means of iterating the Entities that have a set of
// Components. The set is the field list of Q, a struct type whose field types
// are the Components and whose field pointer-ness is the access mode:
//
//	type MoveQuery struct {
//	    Body     *Body     // write — yields the stored value itself
//	    Velocity Velocity  // read  — yields a copy
//	}
//
// Nothing else declares that lock set. A read yields a copy because a read
// yielding a pointer would be a data race against concurrent readers and Go has
// no pointer-to-const; a fat read Component therefore costs a copy per Entity,
// which is evidence the Component is too fat rather than a reason for a hatch.
//
// Fields are named rather than embedded: two Components with the same base name
// from different packages cannot both be embedded, and two Components each
// having a Kind field would make it.Kind an ambiguous selector at the use site,
// far from the cause. The ECS reads field types and never names, so the names
// are free to read well.
//
// A Query is planned once, at registration, from the Component classes the
// world holds. It is created by the handler builder and reached only as a
// parameter of a System, which is what makes under-declaration unrepresentable:
// the only route to a Store is this, and the field types are the declaration.
type Query[Q any] struct {
	// rows is the fill buffer, and it is a field of the Query rather than a
	// local in All() on purpose. As a local its address reaches an opaque yield
	// and it escapes — one allocation, 24 B, per Query per tick, invisible in a
	// microbenchmark because the iterator inlines at the range site there and
	// visible the moment a real System is called across a package boundary.
	// Nothing changes semantically: the same buffer was always reused for every
	// Entity, and the pointer All() yields is valid only for the current step.
	rows Q
	// fields is the field table, planned at registration: an offset into rows,
	// a row width, an access mode, and the baked getter for the Store. bind
	// puts the driver at index 0 and leaves the rest to be probed.
	fields []queryField
	// walk is the owners array of the driver, captured per run. Capturing it is
	// what makes restructuring the Entity being visited safe under swap-remove:
	// the array is shared, so a removal is seen, and an append is not.
	walk []Entity
	// shape is the filler chosen once, at registration, by field count. A
	// per-field loop costs 2.4x a hand-written one and per-field binder closures
	// about 5x — and those allocate.
	//
	// It is an enum reached through a switch rather than the iterator itself
	// held as a func field, and that is not a stylistic choice: an indirect call
	// is opaque to escape analysis, so the yield closure the range statement
	// builds would escape and the frame would cost two allocations a tick — one
	// for the closure and one for the loop state it captures. The switch keeps
	// every call in the chain a known callee, which is what lets the closure
	// stay on the caller's stack. Measured in situ; invisible in a
	// microbenchmark, where the whole iterator inlines at the range site.
	shape uint8
}

// queryField is one Component of a Query, as registration left it.
type queryField struct {
	// get resolves the Store from the handle the Lock bound. It is called once
	// per field per run, never per Entity.
	get func() *storeHeader
	// cursor is what the run needs and nothing else. It is separate from get
	// because the fillers copy it into locals: reached through a pointer it is
	// memory the compiler has to reload after every fill, since a write through
	// an unsafe.Pointer may alias anything.
	cursor queryCursor
}

// queryCursor is one Component's Store as one run sees it: where the membership
// slots are, where the rows start, how wide a row is, what access mode the
// field has, and where in the fill buffer it lands.
//
// The arrays are captured once per run rather than re-read per Entity, which is
// the capture the driver's owners array already gets and has the same
// consequence: a removal is seen, because the array is shared, and an append is
// not, because a growth leaves the old array behind. That is the contract
// either way — no dense row may be held across a mutation, and a pointer into a
// Store is invalidated by a structural change to it.
type queryCursor struct {
	sparse []uint64
	rows   unsafe.Pointer
	offset uintptr
	size   uintptr
	write  bool
}

// wideShape is the field count at and above which the per-field loop is used
// instead of an unrolled filler.
const wideShape = 5

// prepare plans the Query against the world and declares its locks. It runs
// once, inside the single registration-time call of the handler's Lock.
//
// It panics when a field names a Component no plugin registered, naming the
// Component and the Query rather than the store type the user never wrote, and
// the plugin boundary turns that into a composition failure naming the plugin.
func (q *Query[Q]) prepare(en *Entities, access kernel.ResourceAccess) {
	queryType := reflect.TypeFor[Q]()
	if queryType.Kind() != reflect.Struct {
		panic(fmt.Sprintf("ecs: Query %s is a %s; a Query is a struct whose field types are the Components",
			queryType, queryType.Kind()))
	}
	q.fields = make([]queryField, 0, queryType.NumField())
	for i := range queryType.NumField() {
		field := queryType.Field(i)
		componentType, write := field.Type, false
		if componentType.Kind() == reflect.Pointer {
			componentType, write = componentType.Elem(), true
		}
		class := en.classOf(componentType)
		if class == nil {
			panic(fmt.Sprintf("ecs: Query %s names unregistered Component %s", queryType, componentType))
		}
		planned := queryField{cursor: queryCursor{offset: field.Offset, size: class.size, write: write}}
		if write {
			planned.get = class.declareWrite(access)
		} else {
			planned.get = class.declareRead(access)
		}
		q.fields = append(q.fields, planned)
	}
	if len(q.fields) == 0 {
		panic(fmt.Sprintf("ecs: Query %s names no Component, so there is nothing to drive it", queryType))
	}
	// Unrolled by field count, chosen here and never again.
	q.shape = uint8(min(len(q.fields), wideShape))
}

// All iterates the Entities having every Component the Query names, yielding
// the Entity and the filled buffer.
//
// The Entity comes first and always, because the driver's owners array is
// loaded anyway and the alternative is a second iteration shape. The pointer is
// the Query's own buffer and is valid only for the current step — the same
// lifetime rule a kernel handle carries.
//
// The walk is backwards, and that is a guarantee rather than an accident:
// you may restructure the Entity you are currently visiting. Under swap-remove
// a forward walk silently skips, and a forward walk reaches Entities appended
// during the loop, so a System spawning one Entity per visited Entity would not
// terminate. Changing whether some other Entity is in the driver Store is
// undefined.
func (q *Query[Q]) All() iter.Seq2[Entity, *Q] {
	return func(yield func(Entity, *Q) bool) { q.iterate(yield) }
}

// iterate picks the filler the field count chose at registration. Every call
// here is a direct one, which is what keeps the range statement's yield closure
// on the caller's stack.
func (q *Query[Q]) iterate(yield func(Entity, *Q) bool) {
	switch q.shape {
	case 1:
		q.iterate1(yield)
	case 2:
		q.iterate2(yield)
	case 3:
		q.iterate3(yield)
	case 4:
		q.iterate4(yield)
	default:
		q.iterateWide(yield)
	}
}

// bind resolves every Store and picks the driver: the shortest one, because a
// Query costs what its driver is long and not what it matches.
//
// It runs per run and the choice is never cached. Store lengths change on every
// structural change, so a choice made at registration would walk five thousand
// Entities to find three the moment a Component became rare — and a cache would
// be a table every structural change has to write, which is the global index
// this storage model exists to avoid, bought for the few nanoseconds comparing
// the lengths costs.
//
// The driver is moved to index 0 so the fillers need no per-Entity branch to
// find it. Field order is not observable: an offset travels with its field.
func (q *Query[Q]) bind() {
	driver, shortest := 0, -1
	for i := range q.fields {
		field := &q.fields[i]
		store := field.get()
		field.cursor.sparse = store.sparse
		field.cursor.rows = store.dense.data
		if n := len(store.owners); shortest < 0 || n < shortest {
			driver, shortest = i, n
			q.walk = store.owners
		}
	}
	if driver != 0 {
		q.fields[0], q.fields[driver] = q.fields[driver], q.fields[0]
	}
}

// row reports the dense row e's value is in, and whether e has this Component
// at all. It is one load of the sparse slot and one compare of the generation
// half: the compare that finds the row is the compare that rejects a stale
// handle, so liveness is not an extra cost, it is the probe.
//
// It is kept apart from fill, and both are kept small, because the whole point
// of the unrolled fillers is that the loop contains no call: a probe that
// swallowed the fill grew past the inlining budget and cost 8% a frame.
func (c queryCursor) row(e Entity) (uintptr, bool) {
	index := e.idx()
	if int(index) >= len(c.sparse) {
		return 0, false
	}
	slot := c.sparse[index]
	return uintptr(uint32(slot)), uint32(slot>>32) == e.gen()
}

// fill writes one Component into the buffer: the address of the stored row for
// a pointer field, a copy of it for a value field.
//
// The pointer write goes through unsafe.Pointer and emits no GC write barrier.
// That is sound here for one reason, which is an invariant rather than an
// observation: a Query's buffer may only ever hold pointers into a live Store,
// and a Store is a kernel resource cell held for the engine lifetime, so the
// pointee is independently reachable whether a barrier fires or not. The safe
// alternative is a typed setter closure per pointer field, which is the shape
// that costs about 5x and allocates.
func (c queryCursor) fill(row uintptr, buffer unsafe.Pointer) {
	source := unsafe.Add(c.rows, row*c.size)
	target := unsafe.Add(buffer, c.offset)
	if c.write {
		*(*unsafe.Pointer)(target) = source
		return
	}
	// A sized store for the common widths, because a memmove call for eight
	// bytes costs several times what the copy does. None of this needs a write
	// barrier: a Component is pointer-free.
	switch c.size {
	case 8:
		*(*uint64)(target) = *(*uint64)(source)
	case 4:
		*(*uint32)(target) = *(*uint32)(source)
	case 16:
		*(*[2]uint64)(target) = *(*[2]uint64)(source)
	case 0:
		// A Tag carries nothing, and nothing lands in the Query.
	default:
		copyRow(target, source, c.size)
	}
}

// copyRow is the general-width copy, kept out of fill so that fill stays within
// the inlining budget.
func copyRow(target, source unsafe.Pointer, size uintptr) {
	copy(unsafe.Slice((*byte)(target), size), unsafe.Slice((*byte)(source), size))
}

// The fillers below are the unrolled fill, one per field count, chosen at
// registration. Each copies its cursors into locals first: reached through the
// field table they are memory, and a write through an unsafe.Pointer may alias
// anything, so the compiler would reload them after every fill.
//
// The driver's field needs no probe. Its rows are exactly what is being walked,
// and swap-remove keeps owners[row] and dense[row] describing one Entity.

func (q *Query[Q]) iterate1(yield func(Entity, *Q) bool) {
	q.bind()
	buffer := unsafe.Pointer(&q.rows)
	driver := q.fields[0].cursor
	walk := q.walk
	for row := len(walk) - 1; row >= 0; row-- {
		driver.fill(uintptr(row), buffer)
		if !yield(walk[row], &q.rows) {
			return
		}
	}
}

func (q *Query[Q]) iterate2(yield func(Entity, *Q) bool) {
	q.bind()
	buffer := unsafe.Pointer(&q.rows)
	driver, second := q.fields[0].cursor, q.fields[1].cursor
	walk := q.walk
	for row := len(walk) - 1; row >= 0; row-- {
		e := walk[row]
		secondRow, ok := second.row(e)
		if !ok {
			continue
		}
		second.fill(secondRow, buffer)
		driver.fill(uintptr(row), buffer)
		if !yield(e, &q.rows) {
			return
		}
	}
}

func (q *Query[Q]) iterate3(yield func(Entity, *Q) bool) {
	q.bind()
	buffer := unsafe.Pointer(&q.rows)
	driver, second, third := q.fields[0].cursor, q.fields[1].cursor, q.fields[2].cursor
	walk := q.walk
	for row := len(walk) - 1; row >= 0; row-- {
		e := walk[row]
		secondRow, ok := second.row(e)
		if !ok {
			continue
		}
		thirdRow, ok := third.row(e)
		if !ok {
			continue
		}
		second.fill(secondRow, buffer)
		third.fill(thirdRow, buffer)
		driver.fill(uintptr(row), buffer)
		if !yield(e, &q.rows) {
			return
		}
	}
}

func (q *Query[Q]) iterate4(yield func(Entity, *Q) bool) {
	q.bind()
	buffer := unsafe.Pointer(&q.rows)
	driver, second := q.fields[0].cursor, q.fields[1].cursor
	third, fourth := q.fields[2].cursor, q.fields[3].cursor
	walk := q.walk
	for row := len(walk) - 1; row >= 0; row-- {
		e := walk[row]
		secondRow, ok := second.row(e)
		if !ok {
			continue
		}
		thirdRow, ok := third.row(e)
		if !ok {
			continue
		}
		fourthRow, ok := fourth.row(e)
		if !ok {
			continue
		}
		second.fill(secondRow, buffer)
		third.fill(thirdRow, buffer)
		fourth.fill(fourthRow, buffer)
		driver.fill(uintptr(row), buffer)
		if !yield(e, &q.rows) {
			return
		}
	}
}

// iterateWide is the per-field loop, and it is here only for Queries wider than
// the unrolled fillers. It is the shape the unrolled ones exist to avoid: a
// per-field loop costs about 2.4x a hand-written walk where an unrolled fill
// costs about 1.4x.
func (q *Query[Q]) iterateWide(yield func(Entity, *Q) bool) {
	q.bind()
	buffer := unsafe.Pointer(&q.rows)
	driver := q.fields[0].cursor
	probed := q.fields[1:]
	walk := q.walk
	for row := len(walk) - 1; row >= 0; row-- {
		e := walk[row]
		matched := true
		for i := range probed {
			cursor := probed[i].cursor
			probedRow, ok := cursor.row(e)
			if !ok {
				matched = false
				break
			}
			cursor.fill(probedRow, buffer)
		}
		if !matched {
			continue
		}
		driver.fill(uintptr(row), buffer)
		if !yield(e, &q.rows) {
			return
		}
	}
}
