package mux

import "fmt"

// easingTable is a kernel's easings: each easing's function under its
// number, nil where none is registered. It is filled while plugins
// initialize, and only read once the kernel runs.
type easingTable struct {
	functions []func(a, b, t float32) float32
}

// newEasingTable returns a table holding mux's easings: Linear, and Penner's
// curves. Each curve is a shape over a span's progress from 0 to 1, which
// its easing follows from one value to the other.
func newEasingTable() *easingTable {
	backIn := func(t float64) float64 { return (backOvershoot+1)*t*t*t - backOvershoot*t*t }
	backInOutIn := func(t float64) float64 {
		const overshoot = backOvershoot * 1.525
		return (overshoot+1)*t*t*t - overshoot*t*t
	}
	elasticIn := makeElasticIn(elasticPeriod)
	bounceIn := flipCurve(easeBounceOut)
	curves := [...]func(t float64) float64{
		EasingLinear:       func(t float64) float64 { return t },
		EasingQuadIn:       easeQuadIn,
		EasingQuadOut:      flipCurve(easeQuadIn),
		EasingQuadInOut:    makeInOut(easeQuadIn),
		EasingCubicIn:      easeCubicIn,
		EasingCubicOut:     flipCurve(easeCubicIn),
		EasingCubicInOut:   makeInOut(easeCubicIn),
		EasingQuartIn:      easeQuartIn,
		EasingQuartOut:     flipCurve(easeQuartIn),
		EasingQuartInOut:   makeInOut(easeQuartIn),
		EasingQuintIn:      easeQuintIn,
		EasingQuintOut:     flipCurve(easeQuintIn),
		EasingQuintInOut:   makeInOut(easeQuintIn),
		EasingSineIn:       easeSineIn,
		EasingSineOut:      flipCurve(easeSineIn),
		EasingSineInOut:    makeInOut(easeSineIn),
		EasingExpoIn:       easeExpoIn,
		EasingExpoOut:      flipCurve(easeExpoIn),
		EasingExpoInOut:    makeInOut(easeExpoIn),
		EasingCircIn:       easeCircIn,
		EasingCircOut:      flipCurve(easeCircIn),
		EasingCircInOut:    makeInOut(easeCircIn),
		EasingBackIn:       backIn,
		EasingBackOut:      flipCurve(backIn),
		EasingBackInOut:    makeInOut(backInOutIn),
		EasingElasticIn:    elasticIn,
		EasingElasticOut:   flipCurve(elasticIn),
		EasingElasticInOut: makeInOut(makeElasticIn(elasticPeriod * 1.5)),
		EasingBounceIn:     bounceIn,
		EasingBounceOut:    easeBounceOut,
		EasingBounceInOut:  makeInOut(bounceIn),
	}
	table := &easingTable{}
	for easing, curve := range curves {
		table.register(Easing(easing), func(a, b, t float32) float32 { return a + (b-a)*float32(curve(float64(t))) })
	}
	return table
}

// function answers easing's function, nil when none is registered.
func (e *easingTable) function(easing Easing) func(a, b, t float32) float32 {
	if int(easing) >= len(e.functions) {
		return nil
	}
	return e.functions[easing]
}

// register registers function under easing. A number registered before
// panics.
func (e *easingTable) register(easing Easing, function func(a, b, t float32) float32) {
	if e.function(easing) != nil {
		panic(fmt.Sprintf("mux: easing %d is registered twice", easing))
	}
	for len(e.functions) <= int(easing) {
		e.functions = append(e.functions, nil)
	}
	e.functions[easing] = function
}
