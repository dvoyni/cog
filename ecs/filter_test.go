package ecs

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/kernel"
)

// activeQuery is the shape the spec writes filters in: a blank field, yielding
// nothing into the struct and narrowing what the Query matches.
type activeQuery struct {
	Body     *body
	Velocity velocity
	_        Without[disabled]
}

type activeSystem kernel.Subscription[app.UpdateEvent]

// TestWithoutDropsExactlyTheTaggedSet verifies the set rather than assuming it:
// every entity without the Tag is visited, every entity with it is not, and the
// two halves are checked against each other rather than against a count.
func TestWithoutDropsExactlyTheTaggedSet(t *testing.T) {
	entities, components, engine := newWorld(t, 128, func(registrar *kernel.Registrar, world *Entities) {
		registrar.Subscribe[activeSystem](ToHandler[app.UpdateEvent](world,
			func(q *Query[activeQuery]) {
				for _, it := range q.All() {
					it.Body.X += it.Velocity.X
				}
			}))
	})

	const n = 20
	live, tagged := make([]Entity, 0, n), make([]Entity, 0, n)
	for i := range n {
		e := entities.alloc()
		components.bodies.Set(e, body{})
		components.velocities.Set(e, velocity{X: 1})
		// Every third entity is disabled, so the tagged set is neither a prefix
		// nor a suffix of the driver's dense array.
		if i%3 == 0 {
			components.disableds.Set(e, disabled{})
			tagged = append(tagged, e)
			continue
		}
		live = append(live, e)
	}

	frame(t, engine, 1)

	for _, e := range live {
		if value, _ := components.bodies.Get(e); value.X != 1 {
			t.Fatalf("%v carries no Tag and was not visited: %v", e, value)
		}
	}
	for _, e := range tagged {
		if value, _ := components.bodies.Get(e); value.X != 0 {
			t.Fatalf("%v carries the Tag and was visited anyway: %v", e, value)
		}
	}
	if len(tagged) == 0 || len(live) == 0 {
		t.Fatalf("the fixture excluded %d of %d, which tests nothing", len(tagged), n)
	}
}

// withQuery is the other filter: presence matched without the Component landing
// in the struct, which is what a fat Component a System never reads wants.
type withQuery struct {
	Body     *body
	Velocity velocity
	_        With[collider]
}

type withSystem kernel.Subscription[app.UpdateEvent]

// disablingQuery is the System a filter must serialise against: it holds
// write{disabled}, so it is mutating the exact sparse array a Without[disabled]
// probe loads.
type disablingQuery struct {
	Disabled *disabled
}

type disablingSystem kernel.Subscription[app.UpdateEvent]

// TestAFilterContributesAReadOfItsStore is the correction this ticket carries:
// #238 had filters contributing no access, and that was a data race rather than
// a refinement. The assertion is not that the read appears in a list — it is
// that the kernel reports the two Systems as unable to overlap, on that Store
// and on nothing else. Drop the declaration from the filter and the pair
// vanishes from the conflict report, which is the race.
func TestAFilterContributesAReadOfItsStore(t *testing.T) {
	_, _, engine := newWorld(t, 32, func(registrar *kernel.Registrar, world *Entities) {
		registrar.Subscribe[activeSystem](ToHandler[app.UpdateEvent](world,
			func(q *Query[activeQuery]) {}))
		registrar.Subscribe[withSystem](ToHandler[app.UpdateEvent](world,
			func(q *Query[withQuery]) {}))
		registrar.Subscribe[disablingSystem](ToHandler[app.UpdateEvent](world,
			func(q *Query[disablingQuery]) {}))
	})

	description := engine.Describe()
	for _, sub := range description.Subscriptions {
		switch sub.Type {
		case reflect.TypeFor[activeSystem]():
			if !namesType(sub.Reads, "disabled]") {
				t.Fatalf("a Without[disabled] System reads %v, which does not include the disabled Store", sub.Reads)
			}
			if namesType(sub.Writes, "disabled]") {
				t.Fatalf("a Without[disabled] System write-locks the Store it only probes: %v", sub.Writes)
			}
		case reflect.TypeFor[withSystem]():
			if !namesType(sub.Reads, "collider]") {
				t.Fatalf("a With[collider] System reads %v, which does not include the collider Store", sub.Reads)
			}
			if namesType(sub.Writes, "collider]") {
				t.Fatalf("a With[collider] System write-locks the Store it only probes: %v", sub.Writes)
			}
		}
	}

	// The lock set is only worth what the scheduler does with it. These two
	// Systems share no other resource for write, so the pair exists in the
	// conflict report because of the filter and for no other reason.
	found := false
	for _, conflict := range description.Contention.Handlers {
		pair := []reflect.Type{conflict.A.Type, conflict.B.Type}
		if !slices.Contains(pair, reflect.TypeFor[activeSystem]()) ||
			!slices.Contains(pair, reflect.TypeFor[disablingSystem]()) {
			continue
		}
		found = true
		if !namesType(conflict.Resources, "disabled]") {
			t.Fatalf("the two Systems conflict on %v, which is not the disabled Store", conflict.Resources)
		}
	}
	if !found {
		t.Fatalf("a System filtering on Without[disabled] may run beside one holding write{disabled}: %v",
			description.Contention.Handlers)
	}
}

// paddedQuery puts a filter in the middle of the struct and one at the end, the
// two positions Go lays out differently. A zero-size field in the middle shares
// the offset of the field after it; a trailing one forces a byte of padding and
// has no field after it at all.
type paddedQuery struct {
	Body     *body
	_        With[collider]
	Velocity velocity
	_        Without[disabled]
}

type paddedSystem kernel.Subscription[app.UpdateEvent]

// TestTheFillSkipsFilterFields is the end-to-end half: a Query carrying two
// filters narrows correctly and reads its Components correctly.
//
// It is the weaker half, and which half is weaker is worth recording. A
// mis-filled filter can never produce a wrong Component value: a zero-size field
// aliases the offset of the field declared after it, and fields are filled in
// declaration order with the driver last, so the aliased field always overwrites
// the damage. What a mis-filled filter does instead is write past the end of the
// buffer, out of the struct entirely. So the test that catches it is the
// structural one below, and this one is here for the behaviour rather than for
// the hazard.
func TestTheFillSkipsFilterFields(t *testing.T) {
	var seen velocity
	visited := 0
	entities, components, engine := newWorld(t, 32, func(registrar *kernel.Registrar, world *Entities) {
		registrar.Subscribe[paddedSystem](ToHandler[app.UpdateEvent](world,
			func(q *Query[paddedQuery]) {
				for _, it := range q.All() {
					seen = it.Velocity
					visited++
				}
			}))
	})

	e := entities.alloc()
	components.bodies.Set(e, body{})
	components.velocities.Set(e, velocity{X: 3, Y: 5})
	// A radius no velocity carries, so a mis-filled field is unmistakable
	// rather than a plausible number.
	components.colliders.Set(e, collider{Radius: 7})

	frame(t, engine, 1)

	if visited != 1 {
		t.Fatalf("the Query visited %d entities, want 1", visited)
	}
	if seen != (velocity{X: 3, Y: 5}) {
		t.Fatalf("the Query read %v, want {3 5}: the fill wrote a filtered Store into a blank field", seen)
	}
}

// TestAFilterIsPlannedWithNoWidth is the same fact one level down, and it is
// where the fill actually recognises a filter: at registration, as a row width
// of zero. By the time a filler runs there is nothing left to recognise, which
// is what keeps the recognition off the hot path.
func TestAFilterIsPlannedWithNoWidth(t *testing.T) {
	var query *Query[paddedQuery]
	_, _, engine := newWorld(t, 32, func(registrar *kernel.Registrar, world *Entities) {
		registrar.Subscribe[paddedSystem](ToHandler[app.UpdateEvent](world,
			func(q *Query[paddedQuery]) { query = q }))
	})
	frame(t, engine, 1)
	if query == nil {
		t.Fatalf("no Query was planned")
	}

	filters := 0
	for i := range query.fields {
		field := &query.fields[i]
		if !field.filter {
			continue
		}
		filters++
		if field.cursor.size != 0 {
			t.Fatalf("a filter is planned %d bytes wide, want 0", field.cursor.size)
		}
		if field.cursor.write {
			t.Fatalf("a filter is planned as a write")
		}
	}
	if filters != 2 {
		t.Fatalf("the plan holds %d filters, want 2", filters)
	}
}

// filtersOnlyQuery names two filters and nothing else, so nothing can drive it.
type filtersOnlyQuery struct {
	_ Without[disabled]
	_ With[body]
}

type filtersOnlySystem kernel.Subscription[app.UpdateEvent]

// TestAQueryOfFiltersOnlyFailsAtRegistration is the Driver's precondition made
// into a composition failure. A filter can never drive — Without[T]'s owners
// array lists exactly the Entities to exclude, and nothing enumerates the
// complement — so a Query of filters alone would have to drive off Entities,
// which would put read{*Entities} into the lock set for that reason rather than
// by design.
func TestAQueryOfFiltersOnlyFailsAtRegistration(t *testing.T) {
	entities := NewEntities(8)
	var failure error
	kernel.New(nil).
		Handler(func(err error) bool { failure = err; return true }).
		WithPlugins(
			Plugin(entities),
			&componentsPlugin{world: entities, ids: 8},
			&systemsPlugin{world: entities, subscribe: func(registrar *kernel.Registrar, world *Entities) {
				registrar.Subscribe[filtersOnlySystem](ToHandler[app.UpdateEvent](world,
					func(q *Query[filtersOnlyQuery]) {}))
			}},
		)

	if failure == nil {
		t.Fatalf("composing a Query of filters alone succeeded")
	}
	message := failure.Error()
	// The Query, the plugin, and why — a Query that names only filters is a
	// mistake about the Driver, so the diagnostic has to say what a Driver is.
	for _, want := range []string{"systems", "filtersOnlyQuery", "filter", "drive"} {
		if !strings.Contains(message, want) {
			t.Fatalf("composition failure %q does not name %q", message, want)
		}
	}
}

// TestAQueryOfNoFieldsAtAllFailsTheSameWay keeps the empty struct on the same
// diagnostic rather than on one of its own: both are a Query with nothing to
// walk, and the reader needs the same sentence.
func TestAQueryOfNoFieldsAtAllFailsTheSameWay(t *testing.T) {
	type emptyQuery struct{}
	type emptySystem kernel.Subscription[app.UpdateEvent]

	entities := NewEntities(8)
	var failure error
	kernel.New(nil).
		Handler(func(err error) bool { failure = err; return true }).
		WithPlugins(
			Plugin(entities),
			&componentsPlugin{world: entities, ids: 8},
			&systemsPlugin{world: entities, subscribe: func(registrar *kernel.Registrar, world *Entities) {
				registrar.Subscribe[emptySystem](ToHandler[app.UpdateEvent](world,
					func(q *Query[emptyQuery]) {}))
			}},
		)

	if failure == nil {
		t.Fatalf("composing a Query of no fields succeeded")
	}
	if !strings.Contains(failure.Error(), "drive") {
		t.Fatalf("composition failure %q does not say nothing can drive the Query", failure.Error())
	}
}

// TestAFilterIsNeverTheDriver is the same rule one level down, and it is the
// one a heuristic could get wrong silently: a Without over a Store shorter than
// every Component's would be the shortest Store in the scan, and picking it
// would walk exactly the Entities the Query excludes.
func TestAFilterIsNeverTheDriver(t *testing.T) {
	var query *Query[activeQuery]
	entities, components, engine := newWorld(t, 128, func(registrar *kernel.Registrar, world *Entities) {
		registrar.Subscribe[activeSystem](ToHandler[app.UpdateEvent](world,
			func(q *Query[activeQuery]) { query = q }))
	})

	for range 16 {
		e := entities.alloc()
		components.bodies.Set(e, body{})
		components.velocities.Set(e, velocity{X: 1})
	}
	// One tagged entity, so the filter's Store is far and away the shortest.
	tagged := entities.alloc()
	components.bodies.Set(tagged, body{})
	components.velocities.Set(tagged, velocity{X: 1})
	components.disableds.Set(tagged, disabled{})

	frame(t, engine, 1)
	if query == nil {
		t.Fatalf("no Query was planned")
	}

	query.bind()
	if query.fields[0].filter {
		t.Fatalf("the driver is a filter, whose Store is the shortest one")
	}
	if len(query.walk) != 17 {
		t.Fatalf("the driver walks %d entities, want the 17 the Component Stores hold", len(query.walk))
	}
	if value, _ := components.bodies.Get(tagged); value.X != 0 {
		t.Fatalf("the tagged entity moved, so the filter drove the walk: %v", value)
	}
}

// TestASparseSlotHoldsOnlyTheOwnersGenerationOrAbsence pins the invariant the
// filter probe rests on. A Without matches by looking for absentGeneration
// instead of the Entity's own, which is exact only because those are the two
// values a slot can hold for a live Entity: Set writes the one, remove writes
// the other, and a despawn is eager and total, so no slot ever keeps a third.
//
// Break it — leave a stale generation behind on a removal, say — and a Without
// would start matching Entities that do have the Component, silently.
func TestASparseSlotHoldsOnlyTheOwnersGenerationOrAbsence(t *testing.T) {
	entities := NewEntities(16)
	bodies := NewStore[body](entities, 16)
	tags := NewStore[disabled](entities, 16)

	live := make([]Entity, 0, 32)
	for i := range 24 {
		e := entities.alloc()
		live = append(live, e)
		if i%2 == 0 {
			bodies.Set(e, body{X: float32(i)})
		}
		if i%3 == 0 {
			tags.Set(e, disabled{})
		}
	}
	// Removals, despawns and recycled indices are the three ways a slot changes
	// hands, and all three have to leave it at one of the two values.
	for i, e := range live {
		switch i % 4 {
		case 1:
			bodies.Remove(e)
		case 2:
			entities.despawn(e)
		}
	}
	for range 12 {
		e := entities.alloc()
		live = append(live, e)
		bodies.Set(e, body{})
	}

	for _, e := range live {
		if !entities.Alive(e) {
			continue
		}
		for name, sparse := range map[string][]uint64{"body": bodies.sparse, "disabled": tags.sparse} {
			if int(e.idx()) >= len(sparse) {
				continue
			}
			if generation := uint32(sparse[e.idx()] >> 32); generation != e.gen() && generation != absentGeneration {
				t.Fatalf("the %s Store's slot for the live %v holds generation %d, which is neither its own nor absent",
					name, e, generation)
			}
		}
	}
}

// TestWithoutMatchesBeyondTheFilteredStoresSparseIndex covers the probe's other
// answer, which is easy to get backwards. A Store's sparse index is only as long
// as it has been made to grow, so a Store nothing was ever added to stays at its
// reserve while the Entities run past it — and an Entity beyond the end of that
// index is an Entity the Store holds nothing for, which is exactly what a
// Without wants. Reading it as "no answer" instead would drop every Entity above
// the reserve, silently and only in the app that outgrew its hint.
func TestWithoutMatchesBeyondTheFilteredStoresSparseIndex(t *testing.T) {
	const ids, n = 8, 40
	visited := 0
	entities, components, engine := newWorld(t, ids, func(registrar *kernel.Registrar, world *Entities) {
		registrar.Subscribe[activeSystem](ToHandler[app.UpdateEvent](world,
			func(q *Query[activeQuery]) {
				for _, it := range q.All() {
					it.Body.X += it.Velocity.X
					visited++
				}
			}))
	})

	for range n {
		e := entities.alloc()
		components.bodies.Set(e, body{})
		components.velocities.Set(e, velocity{X: 1})
	}
	// Nothing was ever given the Tag, so its Store's sparse index is still the
	// reserve and most of the population is past the end of it.
	if length := len(components.disableds.sparse); length != ids {
		t.Fatalf("the filtered Store's sparse index is %d long, want the untouched reserve of %d", length, ids)
	}

	frame(t, engine, 1)

	if visited != n {
		t.Fatalf("the Query visited %d entities, want all %d: an Entity past the end of the "+
			"filtered Store's index is one the Store holds nothing for", visited, n)
	}
}
