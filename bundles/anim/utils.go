package anim

import (
	"github.com/dvoyni/cog/bundles/anim/internal"
	"github.com/dvoyni/cog/libs/m"
)

// Over returns Params for a one-shot linear track of the given duration that
// starts at the chain point.
func Over(duration float32) Params { return internal.Over(duration) }

// Linear returns progress unchanged. A track added without an easing uses it.
func Linear(progress float32) float32 { return internal.Linear(progress) }

// EaseCubicIn starts slowly and accelerates.
func EaseCubicIn(progress float32) float32 { return internal.EaseCubicIn(progress) }

// EaseCubicOut starts fast and decelerates.
func EaseCubicOut(progress float32) float32 { return internal.EaseCubicOut(progress) }

// EaseCubicInOut accelerates through the first half and decelerates through
// the second.
func EaseCubicInOut(progress float32) float32 { return internal.EaseCubicInOut(progress) }

// Hold returns an easing that stays at 0 for the first fraction of the span,
// then runs easing over the remainder. It folds a pause into a single track,
// for a value that must hold before it moves. A fraction of 0 is easing
// itself; a nil easing is Linear.
func Hold(fraction float32, easing Easing) Easing { return internal.Hold(fraction, easing) }

// Reverse returns an easing that plays easing backwards, so an ease-out
// becomes the matching ease-in. A nil easing is Linear.
func Reverse(easing Easing) Easing { return internal.Reverse(easing) }

// LerpFloat mixes two scalars linearly.
func LerpFloat(from float32, to float32) Lerp[float32] { return internal.LerpFloat(from, to) }

// LerpAngle mixes two radian angles along the shortest arc.
func LerpAngle(from float32, to float32) Lerp[float32] { return internal.LerpAngle(from, to) }

// LerpVec2 mixes two vectors componentwise.
func LerpVec2(from m.Vec2, to m.Vec2) Lerp[m.Vec2] { return internal.LerpVec2(from, to) }

// LerpVec3 mixes two vectors componentwise.
func LerpVec3(from m.Vec3, to m.Vec3) Lerp[m.Vec3] { return internal.LerpVec3(from, to) }

// LerpColor mixes two colors componentwise.
func LerpColor(from m.Color, to m.Color) Lerp[m.Color] { return internal.LerpColor(from, to) }
