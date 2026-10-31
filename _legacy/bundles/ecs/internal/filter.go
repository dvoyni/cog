package internal

import "reflect"

// Without narrows a Query to the Entities that do not have T. It is written as
// a blank field, because it yields nothing into the Query struct:
//
//	type ActiveQuery struct {
//	    Body     *Body
//	    Velocity Velocity
//	    _        ecs.Without[Disabled]
//	}
//
// It contributes read{Disabled} to the System's lock set, and that is the part
// that is easy to get wrong. Evaluating Without[T] loads Store[T]'s sparse slot
// for the Entity, and a concurrent System adding T holds write{T} and is
// mutating that exact array. A filter reads less data than a Component field —
// nothing lands in the struct — but it reads the same Store, and the lock set
// is about Stores.
//
// A Without can never be the Driver: its owners array lists exactly the
// Entities to exclude, and nothing enumerates the complement. So a Query of
// Withouts alone has nothing to walk, and needs at least one Component, Tag or
// With besides them; see the panic prepare raises.
type Without[T any] struct{}

// With narrows a Query to the Entities that do have T, without yielding T into
// the struct. It is the spelling for presence you want to match on but not read
// — a fat Component whose bytes the System never touches, where a value field
// would cost a copy per Entity.
//
// It contributes read{T} for the same reason Without does: the probe is a read
// of that Store's sparse array.
//
// It is the recommended spelling for presence a System matches on but does
// not read, whether T is a Tag or not, and it can drive: its owners array
// holds a superset of the match set, exactly as a Component field's does, so
// the Query walks it when it is the shortest Store. The Driver's remedy is
// spelled _ With[Solid].
type With[T any] struct{}

// queryFilter is what a Query field of filter type answers while the Query is
// planned: which Component's Store it reads, and which answer from the probe
// means the Entity matches.
//
// The method is unexported, so the set of filters is closed and lives here. An
// Or[…] arrives as another type in this file rather than as a branch anywhere
// else: prepare asks the field type this one question and never asks what kind
// of filter it is.
type queryFilter interface {
	filters() (component reflect.Type, present bool)
}

func (Without[T]) filters() (reflect.Type, bool) { return reflect.TypeFor[T](), false }

func (With[T]) filters() (reflect.Type, bool) { return reflect.TypeFor[T](), true }

// filterOf reports the Component a Query field filters on, or false if the
// field is an ordinary Component field. It is reflection, so it runs at
// registration and never again.
func filterOf(fieldType reflect.Type) (component reflect.Type, present bool, ok bool) {
	if fieldType.Kind() != reflect.Struct {
		return nil, false, false
	}
	filter, is := reflect.Zero(fieldType).Interface().(queryFilter)
	if !is {
		return nil, false, false
	}
	component, present = filter.filters()
	return component, present, true
}
