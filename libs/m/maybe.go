package m

import (
	"bytes"
	"encoding/json"
)

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
// It crosses JSON as the pointer did: a present value is T's own encoding, an
// absent one is null, and null reads back as absent. Tag the field omitzero to
// leave an absent one out, which is what omitempty did for the pointer; a
// missing field then reads as absent too.
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

// Present reports whether a value is held.
func (o Maybe[T]) Present() bool { return o.ok }

// Or returns the value if present and fallback otherwise.
func (o Maybe[T]) Or(fallback T) T {
	if o.ok {
		return o.value
	}
	return fallback
}

// MarshalJSON writes a present value as T's encoding and an absent one as null.
func (o Maybe[T]) MarshalJSON() ([]byte, error) {
	if !o.ok {
		return []byte("null"), nil
	}
	return json.Marshal(o.value)
}

// UnmarshalJSON reads null as absent and anything else as a present T.
func (o *Maybe[T]) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte("null")) {
		*o = Maybe[T]{}
		return nil
	}
	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*o = Some(value)
	return nil
}
