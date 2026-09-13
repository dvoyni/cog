package m

// Maybe is a value that may be absent. Its zero value is absent, so a struct
// field of this type reads as "not set" wherever a caller wrote nothing, and a
// present zero - a clear depth of 0, transparent black - stays distinct from it.
//
// It exists for the optional fields a pointer used to spell. A *T reads the
// same way but is mutable indirection: a copy of the struct shares the pointee,
// so a value holding one cannot be copied into an ECS Store, compared by value,
// or handed to another goroutine without an aliasing question. A Maybe is the
// T and one bool, held inline, and is comparable whenever T is.
//
// There is no None: the zero value already says it.
type Maybe[T any] struct {
	value T
	ok    bool
}

// Some is a present value.
func Some[T any](value T) Maybe[T] { return Maybe[T]{value: value, ok: true} }

// Get returns the value and whether it is present. An absent Maybe returns T's
// zero value and false.
func (o Maybe[T]) Get() (T, bool) { return o.value, o.ok }

// Or returns the value if present and fallback otherwise.
func (o Maybe[T]) Or(fallback T) T {
	if o.ok {
		return o.value
	}
	return fallback
}
