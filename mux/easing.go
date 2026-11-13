package mux

// Easing names a curve by its number: the value a fraction of the way from
// one number to another. A tween stores only its easing's number, so the
// timeline stays plain data. EasingLinear is the only easing yet, and a tween
// by any other panics.
type Easing uint16

// EasingLinear, the zero Easing, moves at an even pace from one value to the
// other.
const EasingLinear Easing = 0

// easeLinear is EasingLinear's curve: the value a fraction t of the way from
// a to b.
func easeLinear(a, b, t float32) float32 { return a + (b-a)*t }
