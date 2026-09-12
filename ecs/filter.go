package ecs

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
// A filter can never be the Driver, so a Query needs at least one present-typed
// Component or Tag besides its filters; see the panic prepare raises. For
// Without that is forced: its owners array lists exactly the Entities to
// exclude, and nothing enumerates the complement.
type Without[T any] struct{}

// With narrows a Query to the Entities that do have T, without yielding T into
// the struct. It is the spelling for presence you want to match on but not read
// — a fat Component whose bytes the System never touches, where a value field
// would cost a copy per Entity.
//
// It contributes read{T} for the same reason Without does: the probe is a read
// of that Store's sparse array.
//
// A Tag named as an ordinary field already yields nothing and already costs no
// copy, and unlike a filter it can drive. Reach for With[T] where T is not a
// Tag; where it is, name it.
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
