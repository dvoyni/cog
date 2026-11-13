package mux

// interpolator is a value type's way of easing two of its values, component
// by component: the value a fraction t of the way from a to b, by ease.
type interpolator[T any] func(a, b T, t float32, ease func(a, b, t float32) float32) T

// interpolateFloat32 is float32's interpolator: a float32 is one component,
// eased as it is.
func interpolateFloat32(a, b, t float32, ease func(a, b, t float32) float32) float32 {
	return ease(a, b, t)
}
