package mux

import (
	"fmt"
	"math"
	"reflect"
	"unsafe"

	"github.com/dvoyni/cog/kernel"
)

// interpolator is a value type's way of easing two of its values, component
// by component: the value a fraction t of the way from a to b, by ease.
type interpolator[T any] func(a, b T, t float32, ease func(a, b, t float32) float32) T

// RegisterInterpolator registers function as T's interpolator, for every
// model of the kernel: function answers the value a fraction t of the way
// from a to b, easing each of their components by ease. It is called from
// the Init of a plugin listed after mux's, before or after the paths to T
// are made.
//
// Every float and integer kind has an interpolator built in, named types of
// those kinds included. An integer rounds to the nearest value, and stops at
// the ends of its kind's range when an easing such as Back overshoots them. A
// property needing other behaviour, such as an angle taking the shortest arc,
// gets a named type of its own, whose registered interpolator replaces the
// built-in one for that type alone.
//
// Registering before mux's plugin, after Init, or for a type registered
// before, is a composition error, and panics.
func RegisterInterpolator[T any](configurator *kernel.Configurator, function func(a, b T, t float32, ease func(a, b, t float32) float32) T) {
	valueType := reflect.TypeFor[T]()
	registry := findRegistry(configurator, "interpolator for %v is registered", valueType)
	if _, ok := registry.interpolators[valueType]; ok {
		panic(fmt.Sprintf("mux: interpolator for %v is registered twice", valueType))
	}
	registry.interpolators[valueType] = interpolator[T](function)
}

// makeBuiltInInterpolator answers the interpolator built in for T's kind, as
// an interpolator[T], or nil when T is of no float or integer kind.
func makeBuiltInInterpolator[T any]() any {
	switch reflect.TypeFor[T]().Kind() {
	case reflect.Float32:
		return interpolator[T](interpolateFloat32[T])
	case reflect.Float64:
		return interpolator[T](interpolateFloat64[T])
	case reflect.Int:
		return interpolator[T](interpolateInteger[T, int])
	case reflect.Int8:
		return interpolator[T](interpolateInteger[T, int8])
	case reflect.Int16:
		return interpolator[T](interpolateInteger[T, int16])
	case reflect.Int32:
		return interpolator[T](interpolateInteger[T, int32])
	case reflect.Int64:
		return interpolator[T](interpolateInteger[T, int64])
	case reflect.Uint:
		return interpolator[T](interpolateInteger[T, uint])
	case reflect.Uint8:
		return interpolator[T](interpolateInteger[T, uint8])
	case reflect.Uint16:
		return interpolator[T](interpolateInteger[T, uint16])
	case reflect.Uint32:
		return interpolator[T](interpolateInteger[T, uint32])
	case reflect.Uint64:
		return interpolator[T](interpolateInteger[T, uint64])
	case reflect.Uintptr:
		return interpolator[T](interpolateInteger[T, uintptr])
	}
	return nil
}

// interpolateFloat32 is the interpolator of T, whose kind is float32's: a
// float32 is one component, eased as it is.
func interpolateFloat32[T any](a, b T, t float32, ease func(a, b, t float32) float32) T {
	value := ease(*(*float32)(unsafe.Pointer(&a)), *(*float32)(unsafe.Pointer(&b)), t)
	return *(*T)(unsafe.Pointer(&value))
}

// interpolateFloat64 is the interpolator of T, whose kind is float64's. It
// eases its progress, ease(0, 1, t), and moves that far from a to b, so it
// keeps a precision a float32 has not.
func interpolateFloat64[T any](a, b T, t float32, ease func(a, b, t float32) float32) T {
	from := *(*float64)(unsafe.Pointer(&a))
	to := *(*float64)(unsafe.Pointer(&b))
	value := from + (to-from)*float64(ease(0, 1, t))
	return *(*T)(unsafe.Pointer(&value))
}

// integer is every integer kind's type.
type integer interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr
}

// interpolateInteger is the interpolator of T, whose kind is I's. It eases
// its progress, ease(0, 1, t), moves that far from a to b, and rounds to the
// nearest I. An easing that overshoots, such as Back's, stops at the end of
// I's range.
func interpolateInteger[T any, I integer](a, b T, t float32, ease func(a, b, t float32) float32) T {
	var minimum I
	maximum := ^minimum
	if maximum < minimum {
		minimum = I(1) << (8*unsafe.Sizeof(minimum) - 1)
		maximum = ^minimum
	}
	from := float64(*(*I)(unsafe.Pointer(&a)))
	to := float64(*(*I)(unsafe.Pointer(&b)))
	value := math.Round(from + (to-from)*float64(ease(0, 1, t)))
	result := maximum
	switch {
	case value <= float64(minimum):
		result = minimum
	case value < float64(maximum):
		result = I(value)
	}
	return *(*T)(unsafe.Pointer(&result))
}
