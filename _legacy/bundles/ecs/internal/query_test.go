package internal

import (
	"reflect"
	"slices"
	"testing"
	"unsafe"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// TestTheErasedStoreMatchesTheTypedOne is what the whole fill rests on: a Query
// reaches a Store whose Component type it knows only as a reflect.Type, by
// viewing *Store[T] as a header that is the same shape for every T. The layout
// is asserted rather than assumed, because a field added to Store in the wrong
// place would corrupt silently rather than fail to compile.
func TestTheErasedStoreMatchesTheTypedOne(t *testing.T) {
	entities := newEntities(8)
	store := NewStore[typesPosition](entities, 8)
	e := entities.alloc()
	store.Set(e, typesPosition{X: 3, Y: 4})

	erased := store.erase()
	if unsafe.Sizeof(*store) != unsafe.Sizeof(*erased) {
		t.Fatalf("Store is %d bytes and its header %d", unsafe.Sizeof(*store), unsafe.Sizeof(*erased))
	}
	if len(erased.sparse) != len(store.sparse) || len(erased.owners) != len(store.owners) {
		t.Fatalf("the header sees %d slots and %d owners, the Store %d and %d",
			len(erased.sparse), len(erased.owners), len(store.sparse), len(store.owners))
	}
	if erased.dense.len != len(store.dense) || erased.dense.cap != cap(store.dense) {
		t.Fatalf("the header sees %d/%d rows, the Store %d/%d",
			erased.dense.len, erased.dense.cap, len(store.dense), cap(store.dense))
	}
	if erased.dense.data != unsafe.Pointer(&store.dense[0]) {
		t.Fatalf("the header's rows do not start where the Store's do")
	}
	// A zero-width Component has no rows at all, and the header must still be
	// readable: a Tag's Store is its sparse index and its owners.
	tags := NewStore[disabled](entities, 8)
	tags.Set(e, disabled{})
	if erasedTag := tags.erase(); len(erasedTag.owners) != 1 {
		t.Fatalf("the header of a Tag Store sees %d owners, want 1", len(erasedTag.owners))
	}
}

// capture is how these tests reach a planned Query: a System that keeps the
// parameter it was handed. A Query is created by the handler builder and
// reached only as a System parameter, which is the point — there is no other
// route to a Store — so a test that wants one asks a System for it.
func capture(t *testing.T, ids uint32) (*Query[typesMoveQuery], *componentsPlugin, *Entities, *kernel.Engine) {
	t.Helper()
	var query *Query[typesMoveQuery]
	entities, components, engine := newWorld(t, ids, func(registrar *kernel.Registrar) {
		registrar.Subscribe[typesMoveSystem](ToHandler[app.UpdateEvent](registrar, func(q *Query[typesMoveQuery]) { query = q }))
	})
	engine.Executioner().PublishEvent(app.UpdateEvent{Dt: 1}).Wait()
	return query, components, entities, engine
}

// TestTheDriverIsTheShortestStoreAndIsChosenPerRun is the whole of why a Query
// costs what its driver is long rather than what it matches — and why the
// choice is never cached: Store lengths change on every structural change, so a
// Query that picked its driver once would walk five thousand Entities to find
// three the moment a Component became rare.
func TestTheDriverIsTheShortestStoreAndIsChosenPerRun(t *testing.T) {
	query, components, entities, _ := capture(t, 128)

	for range 8 {
		e := entities.alloc()
		components.velocities.Set(e, typesVelocity{X: 1})
	}
	bodied := entities.alloc()
	components.bodies.Set(bodied, body{})
	components.velocities.Set(bodied, typesVelocity{X: 1})

	// The body field is the Query's only pointer field, so the access mode is
	// what says which Store the scan picked.
	query.bind()
	if driver := query.fields[0].cursor; !driver.write {
		t.Fatalf("the driver is not the body Store, which is the shorter one")
	}

	// Make the other Store the shorter one and run again. Nothing was
	// re-registered and nothing was invalidated: the scan is the mechanism.
	for range 32 {
		e := entities.alloc()
		components.bodies.Set(e, body{})
	}
	query.bind()
	if driver := query.fields[0].cursor; driver.write {
		t.Fatalf("the driver is still the body Store after it became the longer one")
	}
}

// TestAllWalksItsDriverBackwards is a guarantee rather than an accident: the
// reverse walk is the whole reason "you may restructure the Entity you are on"
// is safe under swap-remove, and a forward walk over the same array silently
// skips — which store_test.go measures at 667 of 1000.
func TestAllWalksItsDriverBackwards(t *testing.T) {
	query, components, entities, _ := capture(t, 128)

	const n = 16
	order := make([]Entity, 0, n)
	for range n {
		e := entities.alloc()
		components.bodies.Set(e, body{})
		components.velocities.Set(e, typesVelocity{})
		order = append(order, e)
	}

	visited := make([]Entity, 0, n)
	for e := range query.All() {
		visited = append(visited, e)
	}
	if len(visited) != n {
		t.Fatalf("All() visited %d entities, want %d", len(visited), n)
	}
	for i, e := range visited {
		if want := order[n-1-i]; e != want {
			t.Fatalf("visit %d was %v, want %v: the walk is not backwards", i, e, want)
		}
	}
}

// TestEveryInlinedWalkYieldsWhatItsFillerYields holds the walks written out in
// All() to the fillers they were copied from: the same Entities, in the same
// backwards order over the Driver, with the same buffer at every step. The
// fillers are the reference because the validate build still runs them, so
// this is what keeps the two copies of each walk from drifting apart.
//
// Every Query wider than one loses some Entities to a probe. w1 is removed from
// every fifth Entity, which makes W1 the Driver wherever it is a field; w0 is
// removed from every seventh, which the W0 probe then rejects; and x0 is set on
// every third, which a Without[x0] rejects.
func TestEveryInlinedWalkYieldsWhatItsFillerYields(t *testing.T) {
	t.Run("width 1", func(t *testing.T) { yieldsWhatItsFillerYields[width1](t, false) })
	t.Run("width 2", func(t *testing.T) { yieldsWhatItsFillerYields[width2](t, true) })
	t.Run("width 3", func(t *testing.T) { yieldsWhatItsFillerYields[width3](t, true) })
	t.Run("width 3 with a Without", func(t *testing.T) { yieldsWhatItsFillerYields[without3](t, true) })
	t.Run("width 4", func(t *testing.T) { yieldsWhatItsFillerYields[width4](t, true) })
	t.Run("width 4 with two Withouts", func(t *testing.T) { yieldsWhatItsFillerYields[without4](t, true) })
}

func yieldsWhatItsFillerYields[Q any](t *testing.T, rejects bool) {
	t.Helper()
	const n = 60
	width, q := widthWorld[Q](t, n)
	for i, e := range slices.Clone(width.s0.owners) {
		if i%3 == 0 {
			width.x0s.Set(e, x0{})
		}
		if i%5 == 0 {
			width.s1.Remove(e)
		}
		if i%7 == 0 {
			width.s0.Remove(e)
		}
	}
	type step struct {
		e   Entity
		row Q
	}
	var viaAll, viaFiller []step
	for e, row := range q.All() {
		viaAll = append(viaAll, step{e, *row})
	}
	walked := slices.Clone(q.walk)
	q.iterate(func(e Entity, row *Q) bool {
		viaFiller = append(viaFiller, step{e, *row})
		return true
	})
	if len(viaAll) == 0 || rejects && len(viaAll) >= len(walked) {
		t.Fatalf("All() yielded %d of a %d-long walk, which tests nothing", len(viaAll), len(walked))
	}
	if !reflect.DeepEqual(viaAll, viaFiller) {
		t.Fatalf("All() and the filler disagree:\nAll()  %v\nfiller %v", viaAll, viaFiller)
	}
	// Backwards over the Driver: each yielded Entity sits lower in the walk
	// than the one before it.
	next := len(walked)
	for i, s := range viaAll {
		at := slices.Index(walked[:next], s.e)
		if at < 0 {
			t.Fatalf("visit %d, %v, is not below visit %d in the Driver's owners: the walk is not backwards", i, s.e, i-1)
		}
		next = at
	}
}

// TestAllYieldsOneBufferWhoseLifetimeIsTheStep records the two things a caller
// may not do: the pointer is the Query's own buffer, valid for the current step
// only, and a read field is a copy of the stored value rather than a view of it.
func TestAllYieldsOneBufferWhoseLifetimeIsTheStep(t *testing.T) {
	query, components, entities, _ := capture(t, 128)

	for range 3 {
		e := entities.alloc()
		components.bodies.Set(e, body{})
		components.velocities.Set(e, typesVelocity{X: 1})
	}

	var first *typesMoveQuery
	for _, it := range query.All() {
		if first == nil {
			first = it
			continue
		}
		if it != first {
			t.Fatalf("All() yielded a second buffer at %p, having yielded %p", it, first)
		}
	}

	// A read yields a copy: writing through it changes nothing, which is what
	// makes a read safe against concurrent readers.
	for _, it := range query.All() {
		it.Velocity.X = 99
	}
	for _, value := range components.velocities.dense {
		if value.X != 1 {
			t.Fatalf("a write to a read field reached the Store: %v", value)
		}
	}
}

// TestTheFillBufferIsAFieldOfTheQuery is the first of the three constraints the
// zero-allocation proof bought, and the only one of them this ticket builds. As
// a local in All() the buffer's address reaches an opaque yield and it escapes,
// at one allocation and 24 B per Query per tick — invisible in a microbenchmark,
// because there the iterator inlines at the range site and the buffer stays on
// the stack.
func TestTheFillBufferIsAFieldOfTheQuery(t *testing.T) {
	queryType := reflect.TypeFor[Query[typesMoveQuery]]()
	field, ok := queryType.FieldByName("rows")
	if !ok {
		t.Fatalf("Query has no fill buffer field")
	}
	if field.Type != reflect.TypeFor[typesMoveQuery]() {
		t.Fatalf("the fill buffer is a %s, want the Query struct itself", field.Type)
	}
}

// TestThePopulationIsStableAcrossHundredsOfTicks is the correctness half that
// has to pass before any allocation number is believed: a zero-allocation loop
// computing the wrong answer proves nothing.
func TestThePopulationIsStableAcrossHundredsOfTicks(t *testing.T) {
	const ticks = 500
	entities, components, engine := newWorld(t, 64, subscribeMove)
	moved := make([]Entity, 0, 16)
	for range 16 {
		e := entities.alloc()
		components.bodies.Set(e, body{})
		components.velocities.Set(e, typesVelocity{X: 1, Y: -2})
		moved = append(moved, e)
	}

	for range ticks {
		frame(t, engine, 1)
	}

	if components.bodies.Len() != len(moved) || components.velocities.Len() != len(moved) {
		t.Fatalf("the population is %d bodies and %d velocities after %d ticks, want %d of each",
			components.bodies.Len(), components.velocities.Len(), ticks, len(moved))
	}
	for _, e := range moved {
		value, ok := components.bodies.Get(e)
		if !ok {
			t.Fatalf("%v lost its body over %d ticks", e, ticks)
		}
		if value.X != ticks || value.Y != -2*ticks {
			t.Fatalf("body of %v is %v after %d ticks, want {%d %d}", e, value, ticks, ticks, -2*ticks)
		}
	}
}

// TestAQueryOfOneComponentNeedsNoProbe covers the shortest filler, and with it
// that a Query over a single Component drives off its only Store.
func TestAQueryOfOneComponentNeedsNoProbe(t *testing.T) {
	type bodyOnly struct{ Body *body }
	type soleSystem kernel.Subscription[app.UpdateEvent]

	visited := 0
	entities, components, engine := newWorld(t, 64, func(registrar *kernel.Registrar) {
		registrar.Subscribe[soleSystem](ToHandler[app.UpdateEvent](registrar, func(q *Query[bodyOnly]) {
			for _, it := range q.All() {
				it.Body.X++
				visited++
			}
		}))
	})
	for range 5 {
		components.bodies.Set(entities.alloc(), body{})
	}
	frame(t, engine, 1)

	if visited != 5 {
		t.Fatalf("a one-Component Query visited %d entities, want 5", visited)
	}
}

// TestAWiderQueryProbesEveryComponent exercises the three-field filler and the
// Tag path: a Component with no fields lands nothing in the Query and still
// narrows it.
func TestAWiderQueryProbesEveryComponent(t *testing.T) {
	type wideQuery struct {
		Body     *body
		Velocity typesVelocity
		Collider collider
	}
	type wideSystem kernel.Subscription[app.UpdateEvent]

	visited := 0
	entities, components, engine := newWorld(t, 64, func(registrar *kernel.Registrar) {
		registrar.Subscribe[wideSystem](ToHandler[app.UpdateEvent](registrar, func(q *Query[wideQuery]) {
			for _, it := range q.All() {
				it.Body.X += it.Velocity.X * it.Collider.Radius
				visited++
			}
		}))
	})

	all, two := entities.alloc(), entities.alloc()
	components.bodies.Set(all, body{})
	components.velocities.Set(all, typesVelocity{X: 3})
	components.colliders.Set(all, collider{Radius: 2})
	components.bodies.Set(two, body{})
	components.velocities.Set(two, typesVelocity{X: 3})

	frame(t, engine, 1)

	if visited != 1 {
		t.Fatalf("a three-Component Query visited %d entities, want 1", visited)
	}
	if value, _ := components.bodies.Get(all); value.X != 6 {
		t.Fatalf("body of the matching entity is %v, want {6 0}", value)
	}
}
