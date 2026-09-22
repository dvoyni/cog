package types

import (
	"fmt"
	"reflect"
	"unsafe"

	"github.com/dvoyni/cog/kernel"
)

// spawnField is one Component of a Component set, as registration left it.
type spawnField struct {
	// get reads this Component's Store out of its locked cell, erased. It is
	// the Query's own shape: Spawn.resolve calls it once an invocation and
	// leaves the answer in store, so no write pays the cell's type assertion.
	get func() *storeHeader
	// store is get's answer for the current invocation. See resolver.
	store *storeHeader
	// set writes one Component from the staging buffer into store. A generic
	// cannot be instantiated from a reflect.Type, so the typed call is baked at
	// registration where C is a compile-time type, and reached here through an
	// unsafe.Pointer into staging and the erased Store it converts back.
	set func(store *storeHeader, e Entity, value unsafe.Pointer)
	// offset is where this Component sits in the Component set's struct, which
	// is where it sits in staging: they are the same type.
	offset uintptr
	// recorded is set that also records the Spawn in the Store's Hook log, and
	// hooks whether this run takes it. Neither is read on a Spawn that records
	// nowhere.
	recorded func(store *storeHeader, e Entity, value unsafe.Pointer)
	hooks    hookGate
}

// Spawn creates Entities carrying a complete Component set, named as a struct
// type whose field types are the Components, the way a Query is, and whose value
// carries the Components themselves.
//
//	type Projectile struct {
//	    Body     Body
//	    Velocity Velocity
//	    Collider Collider
//	}
//
//	func fire(sp *ecs.Spawn[Projectile]) {
//	    sp.New(Projectile{Body: Body{X: 1}, Velocity: Velocity{X: 10}})
//	}
//
// A field of the struct simply *is* a Component field: there is no conversion
// step and none is needed, because everything a Component may hold can be
// written where the struct is declared, so a declarative spawn naming a model
// needs nothing from the ECS.
// The struct names the Component set a new Entity starts with and nothing more:
// the Entity may gain and lose Components afterwards, and from then on the
// struct type means nothing. It is not a structure the engine keeps, and nothing
// groups Entities by it.
//
// It is a handle the System already holds, and that is the whole of the design:
// creating an Entity is a direct call on a value in the signature, not a
// Command. A Uses dispatch measures 1113 ns against 0.49 ns for the direct call
// — a factor of 2270, all of it the scheduler round-trip to the coordinator —
// and it would buy nothing, because a dispatch runs on the calling goroutine
// with no locks of its own and the Uses fold is static at finalisation. The
// exclusion a structural change needs was therefore arranged before the frame
// started, by the lock set this handle declares.
//
// It declares write{*Entities}, which supersedes the read every System takes and
// is a total barrier: Entities holds a reference to every Store, so one entry in
// the lock set excludes every System in the frame. That is also why a spawn
// whose Components are chosen at runtime has exactly the lock set of one spelled
// in Go — there is nothing to name statically that is not already named.
//
// Spawn and WriteableEntities are two handles rather than one, because folding
// Despawn onto Spawn[S] would force a Component set type on Systems that never
// spawn.
type Spawn[S any] struct {
	// staging is the Component set the current New is writing, and it is a field
	// of the Spawn rather than the parameter's own address on purpose. The
	// obvious spelling — taking &components of the parameter and handing it to
	// the cached per-field closures — hands the address of a parameter to an
	// opaque func value, so the value escapes: 48 B and one allocation per spawn,
	// 28.4 ns against 15.7. Copying into a buffer bound at registration removes
	// it, and is sound because the handler holds write{*Entities} and therefore
	// runs alone.
	staging S
	// entities is the write-locked authority. It is the handle rather than the
	// registration-time value because a handle is what the kernel guards: the
	// cell it reads is the one the lock covers.
	entities kernel.Write[*Entities]
	// resolved is entities' value for the current invocation. See resolver.
	resolved *Entities
	// fields is the Component set's field table as registration left it: an
	// offset into staging, the Store's getter and this invocation's Store, and
	// the setters baked for that Component's Store.
	// Reflection runs exactly once, here, and never again — the mirror of the
	// Query's fill.
	fields []spawnField
	// hooks is whether this run's spawns are recorded on any Store the Component
	// set carries: on when a Hooks reader watches one of them for Spawns,
	// additions or changes. Checked when the System's first run starts. See
	// hooks.go.
	hooks spawnGate
}

// prepare plans the Component set against the world and declares the locks. It
// runs once, inside the single registration-time call of the handler's Lock.
//
// It panics when a field names a Component no plugin registered, naming the
// Component and the Component set; the plugin boundary turns that into a
// composition failure naming the plugin.
func (s *Spawn[S]) prepare(en *Entities, access kernel.ResourceAccess) {
	setType := reflect.TypeFor[S]()
	if setType.Kind() != reflect.Struct {
		panic(fmt.Sprintf("ecs: Component set %s is a %s; a Component set is a struct whose field types are the Components",
			kernel.TypeName(setType), setType.Kind()))
	}
	s.entities = access.GetWrite[*Entities]()
	s.fields = make([]spawnField, 0, setType.NumField())
	for i := range setType.NumField() {
		field := setType.Field(i)
		if field.Type.Kind() == reflect.Pointer {
			panic(fmt.Sprintf(
				"ecs: Component set %s field %s is a %s; a Component set field is a Component value, because a spawn supplies the value rather than reaching one that already exists",
				kernel.TypeName(setType), field.Name, kernel.TypeName(field.Type)))
		}
		class := en.classOf(field.Type)
		if class == nil {
			panic(fmt.Sprintf("ecs: Component set %s names unregistered Component %s", kernel.TypeName(setType), kernel.TypeName(field.Type)))
		}
		planned := class.declareSet(access)
		planned.offset = field.Offset
		s.fields = append(s.fields, planned)
	}
	s.hooks = spawnGate{fields: s.fields}
}

func (s *Spawn[S]) spawnGate() *spawnGate { return &s.hooks }

// resolve reads the authority and every field's Store out of their cells for
// this invocation. spawnGate shares the field array, so the Stores it writes
// through when a Hooks reader watches are the ones resolved here.
func (s *Spawn[S]) resolve() {
	s.resolved = s.entities.Get()
	for i := range s.fields {
		field := &s.fields[i]
		field.store = field.get()
	}
}

// New creates an Entity carrying every Component the Component set names and
// returns its handle. The Components are written in field order, and the Entity
// is complete when New returns: there is no command buffer and nothing is
// deferred, because a type-erased one costs an allocation per queued command and
// the handler already holds the barrier that would make deferral safe.
//
// Calling it while iterating a Query is safe for the Query being iterated, since
// All() walks its driver backwards and never reaches a row appended during the
// loop. What it is not is cheap in lock duration: the barrier is held for the
// System's whole run, so a System that spawns should be a small System.
func (s *Spawn[S]) New(components S) Entity {
	// The Components land in the Spawn's own buffer before any closure sees an
	// address, which is what keeps them off the heap. See staging.
	s.staging = components
	e := s.resolved.alloc()
	buffer := unsafe.Pointer(&s.staging)
	if s.hooks.on {
		s.hooks.spawn(e, buffer)
		return e
	}
	for i := range s.fields {
		field := &s.fields[i]
		field.set(field.store, e, unsafe.Add(buffer, field.offset))
	}
	return e
}
