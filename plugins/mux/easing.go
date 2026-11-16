package mux

import (
	"fmt"
	"math"

	"github.com/dvoyni/cog/kernel"
)

// Easing names a curve by its number: the value a fraction of the way from
// one number to another. A tween stores only its easing's number, so the
// timeline stays plain data. Numbers below 256 are mux's, which registers
// Robert Penner's curves under the constants below. A game registers its own
// from 256, with RegisterEasing.
type Easing uint16

// mux's easings: Linear, the zero Easing, then each of Penner's curves In,
// Out and InOut. An In curve starts slowly, an Out one ends slowly, and an
// InOut one does both.
const (
	EasingLinear Easing = iota
	EasingQuadIn
	EasingQuadOut
	EasingQuadInOut
	EasingCubicIn
	EasingCubicOut
	EasingCubicInOut
	EasingQuartIn
	EasingQuartOut
	EasingQuartInOut
	EasingQuintIn
	EasingQuintOut
	EasingQuintInOut
	EasingSineIn
	EasingSineOut
	EasingSineInOut
	EasingExpoIn
	EasingExpoOut
	EasingExpoInOut
	EasingCircIn
	EasingCircOut
	EasingCircInOut
	EasingBackIn
	EasingBackOut
	EasingBackInOut
	EasingElasticIn
	EasingElasticOut
	EasingElasticInOut
	EasingBounceIn
	EasingBounceOut
	EasingBounceInOut

	// firstGameEasing is the first number a game registers an easing under:
	// those below are mux's.
	firstGameEasing Easing = 256
)

// RegisterEasing registers function as the easing numbered easing, for
// every model of the kernel: function answers the value a fraction t of the
// way from a to b. It is called from the Init of a plugin listed after mux's.
// Each easing names its own number, so the order of registration does not
// matter, and a saved timeline's easings keep their meaning.
//
// Registering before mux's plugin, after Init, under a number below 256,
// which are mux's, or under a number registered before, is a composition
// error, and panics.
func RegisterEasing(configurator *kernel.Configurator, easing Easing, function func(a, b, t float32) float32) {
	registry := findRegistry(configurator, "easing %d is registered", easing)
	if easing < firstGameEasing {
		panic(fmt.Sprintf("mux: easing %d is registered, but numbers below %d are mux's", easing, firstGameEasing))
	}
	registry.easings.register(easing, function)
}

// Penner's parameters: how far his Back curves pull back from their start,
// and the period his Elastic curves oscillate with. His InOut curves pull
// back 1.525 times as far, and oscillate half again as slowly.
const (
	backOvershoot = 1.70158
	elasticPeriod = 0.3
)

// flipCurve answers curve played backwards and upside down: an In curve's
// Out, and an Out curve's In.
func flipCurve(curve func(t float64) float64) func(t float64) float64 {
	return func(t float64) float64 { return 1 - curve(1-t) }
}

// makeInOut answers the InOut curve of in: in over the first half of the
// span, and in flipped over the second.
func makeInOut(in func(t float64) float64) func(t float64) float64 {
	return func(t float64) float64 {
		if t < 0.5 {
			return in(2*t) / 2
		}
		return 1 - in(2-2*t)/2
	}
}

// makeElasticIn answers Elastic's In curve, which oscillates with period,
// growing, before it reaches its end.
func makeElasticIn(period float64) func(t float64) float64 {
	return func(t float64) float64 {
		if t == 0 || t == 1 {
			return t
		}
		return -math.Exp2(10*t-10) * math.Sin((t-1-period/4)*2*math.Pi/period)
	}
}

// The ease functions below are the curves of Penner's families made with no
// parameter, each a shape over a span's progress from 0 to 1, as easings.net
// writes them.

func easeQuadIn(t float64) float64 { return t * t }

func easeCubicIn(t float64) float64 { return t * t * t }

func easeQuartIn(t float64) float64 { return t * t * t * t }

func easeQuintIn(t float64) float64 { return t * t * t * t * t }

func easeSineIn(t float64) float64 { return 1 - math.Cos(t*math.Pi/2) }

func easeExpoIn(t float64) float64 {
	if t == 0 {
		return 0
	}
	return math.Exp2(10*t - 10)
}

func easeCircIn(t float64) float64 { return 1 - math.Sqrt(1-t*t) }

func easeBounceOut(t float64) float64 {
	const (
		strength = 7.5625
		width    = 2.75
	)
	switch {
	case t < 1/width:
		return strength * t * t
	case t < 2/width:
		t -= 1.5 / width
		return strength*t*t + 0.75
	case t < 2.5/width:
		t -= 2.25 / width
		return strength*t*t + 0.9375
	default:
		t -= 2.625 / width
		return strength*t*t + 0.984375
	}
}
