package ecs

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/kernel"
)

// Reaching another Entity. A Query reaches only what it drives over; a missile's
// target is reached through an Entity stored in a Component — a Reference — and
// followed with an accessor.

// homing is the Component that carries a Reference. An Entity is pointer-free,
// so it is an ordinary Component field and needs no vocabulary of its own.
type homing struct{ Target Entity }

// homingQuery is the real shape: the Component that names the far Entity, and
// the near Entity's own Component to write the answer into.
type homingQuery struct {
	Homing homing
	Body   *body
}

type homingSystem kernel.Subscription[app.UpdateEvent]

// TestASystemReachesAnEntityItDidNotIterateTo is the tracer bullet: a System
// follows a Reference out of a Component it iterated to, and reads the far
// Entity's Component through a Get it declared in its signature.
func TestASystemReachesAnEntityItDidNotIterateTo(t *testing.T) {
	entities, components, engine := newWorld(t, 64, func(registrar *kernel.Registrar, world *Entities) {
		registrar.Subscribe[homingSystem](ToHandler[app.UpdateEvent](world,
			func(q *Query[homingQuery], bodies *Get[body]) {
				for _, it := range q.All() {
					target, ok := bodies.Of(it.Homing.Target)
					if !ok {
						continue
					}
					it.Body.X = target.X
					it.Body.Y = target.Y
				}
			}))
	})

	target := entities.alloc()
	components.bodies.Set(target, body{X: 10, Y: 20})

	missile := entities.alloc()
	components.bodies.Set(missile, body{})
	components.homings.Set(missile, homing{Target: target})

	frame(t, engine, 1)

	if value, _ := components.bodies.Get(missile); value != (body{X: 10, Y: 20}) {
		t.Fatalf("the missile's body is %v after following its Reference, want {10 20}", value)
	}
}

type accessorSystem kernel.Subscription[app.UpdateEvent]

// accessorWorld is one System's accessors, kept so a test can call them outside
// a frame and count what they cost. Only a test in this package can hold them
// that way: a System's only route to any of them is its own signature.
type accessorWorld struct {
	bodyGet        *Get[body]
	bodySet        *Set[body]
	bodyRemove     *Remove[body]
	colliderSet    *Set[collider]
	colliderRemove *Remove[collider]
	writeable      *WriteableEntities
	entities       *Entities
	components     *componentsPlugin
}

// accessors runs one tick of a System that does nothing but keep its own
// parameters, which is the same trick handles plays for Spawn and Despawn.
func accessors(tb testing.TB, ids uint32) *accessorWorld {
	tb.Helper()
	world := &accessorWorld{}
	entities, components, engine := newWorld(tb, ids, func(registrar *kernel.Registrar, en *Entities) {
		registrar.Subscribe[accessorSystem](ToHandler[app.UpdateEvent](en,
			func(g *Get[body], s *Set[body], r *Remove[body],
				cs *Set[collider], cr *Remove[collider], we *WriteableEntities,
			) {
				world.bodyGet, world.bodySet, world.bodyRemove = g, s, r
				world.colliderSet, world.colliderRemove = cs, cr
				world.writeable = we
			}))
	})
	frame(tb, engine, 1)
	world.entities, world.components = entities, components
	return world
}

// TestSetWritesTheEntityItReaches is the write half of the same probe: Of reads
// the copy Get would, and Ref hands out the stored value itself.
func TestSetWritesTheEntityItReaches(t *testing.T) {
	entities, components, engine := newWorld(t, 64, func(registrar *kernel.Registrar, world *Entities) {
		registrar.Subscribe[accessorSystem](ToHandler[app.UpdateEvent](world,
			func(q *Query[homingQuery], bodies *Set[body]) {
				for _, it := range q.All() {
					value, ok := bodies.Of(it.Homing.Target)
					if !ok {
						continue
					}
					ref, _ := bodies.Ref(it.Homing.Target)
					ref.X = value.X + 1
				}
			}))
	})

	target := entities.alloc()
	components.bodies.Set(target, body{X: 10})
	missile := entities.alloc()
	components.bodies.Set(missile, body{})
	components.homings.Set(missile, homing{Target: target})

	frame(t, engine, 1)

	if value, _ := components.bodies.Get(target); value.X != 11 {
		t.Fatalf("the target's body is %v after a Set through the Reference, want {11 0}", value)
	}
}

// TestSetRefReportsNothingForAnEntityWithoutTheComponent: Ref is the same probe
// as Of and answers the same question, so it misses rather than handing back a
// pointer to a row that is not the Entity's.
func TestSetRefReportsNothingForAnEntityWithoutTheComponent(t *testing.T) {
	world := accessors(t, 16)
	bare := world.entities.alloc()

	if ref, ok := world.bodySet.Ref(bare); ok || ref != nil {
		t.Fatalf("Ref of an Entity without the Component = %v, %v; want nil, false", ref, ok)
	}
	if ref, ok := world.bodySet.Ref(NoEntity); ok || ref != nil {
		t.Fatalf("Ref(NoEntity) = %v, %v; want nil, false", ref, ok)
	}
}

// TestUpdateForInsertsWhenAbsent is why there is no Add handle: the write lock
// is the whole of what adding a Component takes, because nothing else records
// which Entities have what.
func TestUpdateForInsertsWhenAbsent(t *testing.T) {
	entities, components, engine := newWorld(t, 64, func(registrar *kernel.Registrar, world *Entities) {
		registrar.Subscribe[accessorSystem](ToHandler[app.UpdateEvent](world,
			func(q *Query[moveQuery], colliders *Set[collider]) {
				for e, it := range q.All() {
					colliders.UpdateFor(e, collider{Radius: it.Body.X})
				}
			}))
	})

	bare := entities.alloc()
	components.bodies.Set(bare, body{X: 3})
	components.velocities.Set(bare, velocity{})

	frame(t, engine, 1)

	value, ok := components.colliders.Get(bare)
	if !ok {
		t.Fatalf("UpdateFor left %v without the Component it was given", bare)
	}
	if value.Radius != 3 {
		t.Fatalf("the inserted collider is %v, want {3}", value)
	}

	// And the second tick replaces rather than inserting again: a Store holds at
	// most one value of a type per Entity, which is arithmetic and not policy.
	components.bodies.Set(bare, body{X: 4})
	frame(t, engine, 1)
	if components.colliders.Len() != 1 {
		t.Fatalf("two ticks of UpdateFor left %d colliders, want 1", components.colliders.Len())
	}
	if value, _ := components.colliders.Get(bare); value.Radius != 4 {
		t.Fatalf("the second UpdateFor left %v, want {4}: it replaces the value", value)
	}
}

// TestUpdateForIsSafeOnTheEntityBeingVisited covers both halves of the claim.
// Inserting into a Store the Query is not driving on cannot disturb the walk at
// all; inserting into the driver's own Store appends a row, and the backwards
// walk never reaches one — which is what makes the insertion safe rather than a
// loop that grows as it runs.
func TestUpdateForIsSafeOnTheEntityBeingVisited(t *testing.T) {
	const population = 200
	visited := 0
	bare := make([]Entity, 0, population)
	entities, components, engine := newWorld(t, 8*population, func(registrar *kernel.Registrar, world *Entities) {
		registrar.Subscribe[accessorSystem](ToHandler[app.UpdateEvent](world,
			func(q *Query[moveQuery], bodies *Set[body], colliders *Set[collider]) {
				i := 0
				for e := range q.All() {
					visited++
					// The Entity being visited gains a Component it did not have.
					colliders.UpdateFor(e, collider{Radius: 1})
					// And an Entity outside the driver is given the driver's own
					// Component, which appends a row ahead of nothing.
					bodies.UpdateFor(bare[i], body{X: 2})
					i++
				}
			}))
	})

	populate(entities, components, population)
	for range population {
		e := entities.alloc()
		components.velocities.Set(e, velocity{X: 1})
		bare = append(bare, e)
	}

	frame(t, engine, 1)

	if visited != population {
		t.Fatalf("a System inserting as it went visited %d Entities, want the %d it started with: "+
			"the walk reached a row appended during the loop", visited, population)
	}
	if components.colliders.Len() != population {
		t.Fatalf("%d Entities were visited and %d gained a collider", population, components.colliders.Len())
	}
	if components.bodies.Len() != 2*population {
		t.Fatalf("the body Store holds %d rows, want %d: every appended row must have landed",
			components.bodies.Len(), 2*population)
	}
}

// TestRemoveTakesAComponentAway is the inverse of UpdateFor, and is a handle of
// its own so that a System which only strips a Component never names a value
// type it does not supply.
func TestRemoveTakesAComponentAway(t *testing.T) {
	var reported, secondReport bool
	entities, components, engine := newWorld(t, 64, func(registrar *kernel.Registrar, world *Entities) {
		registrar.Subscribe[accessorSystem](ToHandler[app.UpdateEvent](world,
			func(q *Query[moveQuery], colliders *Remove[collider]) {
				for e := range q.All() {
					if !reported {
						reported = colliders.From(e)
						continue
					}
					secondReport = colliders.From(e)
				}
			}))
	})

	e := entities.alloc()
	components.bodies.Set(e, body{})
	components.velocities.Set(e, velocity{})
	components.colliders.Set(e, collider{Radius: 1})
	bystander := entities.alloc()
	components.colliders.Set(bystander, collider{Radius: 2})

	frame(t, engine, 1)

	if !reported {
		t.Fatalf("From reported false for an Entity that had the Component")
	}
	if components.colliders.Has(e) {
		t.Fatalf("%v still has the Component a System removed", e)
	}
	if value, ok := components.colliders.Get(bystander); !ok || value.Radius != 2 {
		t.Fatalf("the bystander's collider reads back as %v, %v; want {2}, true", value, ok)
	}
	// The Entity keeps everything else: removal names one Store, where a despawn
	// names none and empties all of them.
	if !components.bodies.Has(e) {
		t.Fatalf("removing one Component took another away from %v", e)
	}

	frame(t, engine, 1)

	if secondReport {
		t.Fatalf("From reported true the second time, want false for an Entity that no longer has one")
	}
}

type declaringSystem kernel.Subscription[app.UpdateEvent]

// TestAnAccessorDeclaresTheAuthorityItselfAndItsStore is the invariant closed by
// construction rather than by the accident that everything uses a Query. The
// handler here is an ordinary kernel subscription whose Lock calls the
// accessor's own prepare and does nothing else, so what Describe reports is what
// the accessor declared and nothing ToHandler added on its behalf.
func TestAnAccessorDeclaresTheAuthorityItselfAndItsStore(t *testing.T) {
	getter, setter, remover := &Get[body]{}, &Set[collider]{}, &Remove[solid]{}
	_, _, engine := newWorld(t, 8, func(registrar *kernel.Registrar, entities *Entities) {
		registrar.Subscribe[declaringSystem](func() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
			return func(access kernel.ResourceAccess) {
				getter.prepare(entities, access)
				setter.prepare(entities, access)
				remover.prepare(entities, access)
			}, func(kernel.Kernel, app.UpdateEvent) error { return nil }
		})
	})

	subscription := describeSubscription(t, engine, reflect.TypeFor[declaringSystem]())
	if !namesType(subscription.Reads, "*ecs.Entities") {
		t.Fatalf("three accessors alone read %v, which does not include *ecs.Entities: "+
			"the despawn traversal rests on every route to a Store declaring it", subscription.Reads)
	}
	if !namesType(subscription.Reads, "body]") {
		t.Fatalf("a Get reads %v, which does not include the body Store", subscription.Reads)
	}
	if namesType(subscription.Writes, "body]") {
		t.Fatalf("a Get write-locks %v: a read handle is not a write in disguise", subscription.Writes)
	}
	for _, written := range []string{"collider]", "solid]"} {
		if !namesType(subscription.Writes, written) {
			t.Fatalf("a Set and a Remove write %v, which does not include the %s Store",
				subscription.Writes, written)
		}
	}
}

// TestGetExposesNoPointerForm is what stops a read handle being a write in
// disguise, and it is a property of the type rather than of the documentation:
// there is no method on Get that hands out an address at all.
func TestGetExposesNoPointerForm(t *testing.T) {
	read := reflect.TypeFor[*Get[body]]()
	for i := range read.NumMethod() {
		method := read.Method(i)
		signature := method.Type
		for out := range signature.NumOut() {
			if signature.Out(out).Kind() == reflect.Pointer {
				t.Fatalf("Get.%s returns %v, a pointer: a read yields a copy, because a read yielding a "+
					"pointer is a data race against concurrent readers", method.Name, signature.Out(out))
			}
		}
	}
	// The control: the write handle does have one, so the check above is not
	// passing because the reflection found nothing to look at.
	if _, has := reflect.TypeFor[*Set[body]]().MethodByName("Ref"); !has {
		t.Fatalf("Set has no Ref, so this test no longer distinguishes the two handles")
	}
}

// TestOnlySetReachesTheAuthority is the other half of "with no liveness check of
// its own", and it is a property of the types rather than of their code. Every
// accessor declares read{*Entities}; a handle that resolves a Reference keeps no
// way to reach it, so a dangling Reference can only ever be answered by the
// Store probe. Set is the exception and has to be, because an insertion is a
// structural change and one naming an Entity that no longer exists must be
// refused — see UpdateFor.
func TestOnlySetReachesTheAuthority(t *testing.T) {
	holdsTheAuthority := func(accessor reflect.Type) bool {
		for i := range accessor.NumField() {
			if strings.Contains(accessor.Field(i).Type.String(), "Entities") {
				return true
			}
		}
		return false
	}

	for _, resolving := range []reflect.Type{
		reflect.TypeFor[Get[body]](), reflect.TypeFor[Remove[body]](),
	} {
		if holdsTheAuthority(resolving) {
			t.Fatalf("%v can reach the authority: a Reference is resolved by the Store probe alone", resolving)
		}
	}
	if inserting := reflect.TypeFor[Set[body]](); !holdsTheAuthority(inserting) {
		t.Fatalf("%v cannot reach the authority, so an insertion cannot refuse a handle to an Entity "+
			"that no longer exists", inserting)
	}
}

// TestAReferenceToADespawnedEntityResolvesToNothing is free rather than checked:
// a despawn empties every Store eagerly, so the probe that would have found the
// row finds absence, and nothing asked whether the Entity exists.
func TestAReferenceToADespawnedEntityResolvesToNothing(t *testing.T) {
	world := accessors(t, 16)
	target := world.entities.alloc()
	world.components.bodies.Set(target, body{X: 5})

	if _, ok := world.bodyGet.Of(target); !ok {
		t.Fatalf("the target has no body before the despawn, so the test proves nothing")
	}

	world.writeable.Despawn(target)

	value, ok := world.bodyGet.Of(target)
	if ok {
		t.Fatalf("a Reference to a despawned Entity resolved to %v", value)
	}
	if value != (body{}) {
		t.Fatalf("a missing Component yielded %v, want the zero value", value)
	}
	if world.entities.Alive(target) {
		t.Fatalf("%v is still Alive, so the miss above is not the one being measured", target)
	}
}

// TestARecycledIndexDoesNotAliasAStaleReference is the guarantee that lets a
// game hold an Entity across frames at all. The generation is in the sparse
// slot, so the compare that finds the row is the compare that rejects the stale
// handle — there is no second structure and no window where the old handle
// addresses the new Entity's data.
func TestARecycledIndexDoesNotAliasAStaleReference(t *testing.T) {
	world := accessors(t, 16)
	stale := world.entities.alloc()
	world.components.bodies.Set(stale, body{X: 5})
	world.writeable.Despawn(stale)

	fresh := world.entities.alloc()
	if fresh.idx() != stale.idx() {
		t.Fatalf("the new Entity %v did not recycle %v's index, so nothing aliases and the test is vacuous",
			fresh, stale)
	}
	world.bodySet.UpdateFor(fresh, body{X: 9})

	if value, ok := world.bodyGet.Of(stale); ok {
		t.Fatalf("the stale Reference resolved to %v, aliasing the Entity that took its index", value)
	}
	if value, ok := world.bodyGet.Of(fresh); !ok || value.X != 9 {
		t.Fatalf("the live Entity's Component reads back as %v, %v; want {9 0}, true", value, ok)
	}
}

// A write pointer is invalidated by a structural change to its Store. Both
// shapes are here deliberately rather than asserted in prose, because both are
// silent: the write lands somewhere, and the somewhere is not the Entity's row.

// TestAWritePointerIsInvalidatedBySwapRemove is the first shape. Removal moves
// the last row into the hole, so the Entity that owned the last row is relocated
// and a pointer taken before the removal addresses a slot that is nobody's.
func TestAWritePointerIsInvalidatedBySwapRemove(t *testing.T) {
	world := accessors(t, 16)
	first, last := world.entities.alloc(), world.entities.alloc()
	world.bodySet.UpdateFor(first, body{X: 1})
	world.bodySet.UpdateFor(last, body{X: 2})

	retained, ok := world.bodySet.Ref(last)
	if !ok {
		t.Fatalf("Ref found no row for %v", last)
	}

	// Removing the Entity in front of it relocates last's data into the hole.
	if !world.bodyRemove.From(first) {
		t.Fatalf("From reported false for an Entity that had the Component")
	}
	retained.X = 99

	value, ok := world.bodyGet.Of(last)
	if !ok {
		t.Fatalf("%v lost its Component to another Entity's removal", last)
	}
	if value.X == 99 {
		t.Fatalf("the write through the retained pointer reached %v: swap-remove relocated the row, "+
			"so a pointer held across a structural change must no longer address it", last)
	}
	if value.X != 2 {
		t.Fatalf("%v's Component is %v after the removal, want the {2 0} it was given", last, value)
	}
	// And the live pointer, taken after the change, does reach it — so the test
	// above is about the retained pointer and not about Ref being broken.
	fresh, _ := world.bodySet.Ref(last)
	fresh.X = 7
	if value, _ := world.bodyGet.Of(last); value.X != 7 {
		t.Fatalf("a pointer taken after the change wrote %v, want {7 0}", value)
	}
}

type growingSystem kernel.Subscription[app.UpdateEvent]

// TestAWritePointerIsInvalidatedByAGrowthDuringIteration is the second shape and
// the sharper one, because the pointer is the Query's own. A run captures the
// driver's owners and every probed Store's arrays once, so an UpdateFor that
// grows a Store mid-iteration leaves the whole run addressing the array the
// growth abandoned — the Query's pointer fields included.
//
// The two arms differ only in whether the insertion grows the Store: with the
// reserve hint exhausted, append hands back a new array and every write after it
// is lost; with room to spare, the same insertion writes in place and every
// write lands.
func TestAWritePointerIsInvalidatedByAGrowthDuringIteration(t *testing.T) {
	const population = 64
	// A Store reserves ids rows, so a world sized to the population has no room
	// left and one insertion reallocates; four times the population has room for
	// the same insertion to land in place.
	run := func(ids uint32) (written int, grew bool) {
		var spare []Entity
		entities, components, engine := newWorld(t, ids, func(registrar *kernel.Registrar, world *Entities) {
			registrar.Subscribe[growingSystem](ToHandler[app.UpdateEvent](world,
				func(q *Query[moveQuery], bodies *Set[body]) {
					inserted := false
					for _, it := range q.All() {
						if !inserted {
							// One insertion, before any write, into the Store the
							// Query is holding pointers into.
							bodies.UpdateFor(spare[0], body{})
							inserted = true
						}
						it.Body.X = 1
					}
				}))
		})
		populate(entities, components, population)
		spare = append(spare, entities.alloc())
		before := &components.bodies.dense[0]

		frame(t, engine, 1)

		grew = before != &components.bodies.dense[0]
		for _, owner := range components.bodies.owners {
			if value, _ := components.bodies.Get(owner); value.X == 1 {
				written++
			}
		}
		return written, grew
	}

	stale, grew := run(population)
	if !grew {
		t.Fatalf("the insertion did not move the dense array, so this arm measures nothing")
	}
	if stale != 0 {
		t.Fatalf("%d of %d writes landed after the Store grew under the iteration, want none: "+
			"the run captured the array the growth abandoned", stale, population)
	}

	live, grew := run(4 * population)
	if grew {
		t.Fatalf("the control arm grew the Store too, so the two arms do not differ by the growth")
	}
	if live != population {
		t.Fatalf("%d of %d writes landed with room to spare in the Store, want all of them: "+
			"without this the first arm's zero could be anything", live, population)
	}
}

// TestAnAccessorOverAnUnregisteredComponentFailsComposition names the Component
// and the accessor, rather than the store type the user never wrote. It is the
// same diagnostic a Query gets, and for the same reason: the ECS catches it
// before the kernel can fail finalisation on a resource nobody declared.
func TestAnAccessorOverAnUnregisteredComponentFailsComposition(t *testing.T) {
	for _, accessor := range []struct {
		name   string
		system any
	}{
		{"Get", func(g *Get[guarded]) {}},
		{"Set", func(s *Set[guarded]) {}},
		{"Remove", func(r *Remove[guarded]) {}},
	} {
		t.Run(accessor.name, func(t *testing.T) {
			entities := NewEntities(8)
			var failure error
			kernel.New(nil).
				Handler(func(err error) bool { failure = err; return true }).
				WithPlugins(
					Plugin(entities),
					&componentsPlugin{world: entities, ids: 8},
					&systemsPlugin{world: entities, subscribe: func(registrar *kernel.Registrar, world *Entities) {
						registrar.Subscribe[accessorSystem](ToHandler[app.UpdateEvent](world, accessor.system))
					}},
				)

			if failure == nil {
				t.Fatalf("composing a %s over an unregistered Component succeeded", accessor.name)
			}
			for _, want := range []string{"systems", accessor.name, "guarded"} {
				if !strings.Contains(failure.Error(), want) {
					t.Fatalf("composition failure %q does not name %q", failure.Error(), want)
				}
			}
		})
	}
}

// TestUpdateForRefusesAnEntityThatNoLongerExists is the one thing an accessor
// must not let a System do. A dangling Reference is safe to *read* because a
// despawn emptied every Store; inserting through one would put a row back, for
// an Entity that no longer exists, which no later despawn can reach — the
// Store's population would stop being exact, the driver scan would read it
// anyway, and a one-Component Query would yield a dead Entity.
//
// The System cannot defend itself here: "does this Entity still exist" is the
// question only the authority answers, and a Query holds it for read but hands
// it to nobody. So the accessor makes the check, which is affordable for exactly
// the reason it declares read{*Entities} in the first place.
func TestUpdateForRefusesAnEntityThatNoLongerExists(t *testing.T) {
	world := accessors(t, 16)
	doomed := world.entities.alloc()
	world.bodySet.UpdateFor(doomed, body{X: 1})
	world.writeable.Despawn(doomed)

	world.bodySet.UpdateFor(doomed, body{X: 2})

	if world.components.bodies.Len() != 0 {
		t.Fatalf("an UpdateFor through a Reference to a despawned Entity left %d rows, want none: "+
			"no Store may hold a dead Entity", world.components.bodies.Len())
	}
	if value, ok := world.bodyGet.Of(doomed); ok {
		t.Fatalf("the despawned Entity reads back as %v after the refused insertion", value)
	}

	// The index comes back, and it comes back clean: the Entity that recycles it
	// has no Component, and one removal empties the Store exactly.
	fresh := world.entities.alloc()
	if fresh.idx() != doomed.idx() {
		t.Fatalf("%v did not recycle %v's index, so the orphan this guards against is not reachable here",
			fresh, doomed)
	}
	if world.components.bodies.Has(fresh) {
		t.Fatalf("%v inherited a Component from the handle that held its index", fresh)
	}
	world.bodySet.UpdateFor(fresh, body{X: 3})
	if value, ok := world.bodyGet.Of(fresh); !ok || value.X != 3 {
		t.Fatalf("the live Entity's insertion reads back as %v, %v; want {3 0}, true: "+
			"the guard must refuse the dead handle and nothing else", value, ok)
	}
	world.bodyRemove.From(fresh)
	if world.components.bodies.Len() != 0 {
		t.Fatalf("%d rows survive after removing the only live Entity's Component: an orphan row is left",
			world.components.bodies.Len())
	}
}
