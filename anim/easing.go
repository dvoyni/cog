package anim

// Easing maps normalized progress in [0, 1] to eased progress. A track applies
// its easing to clamped (or, when looping, wrapped) progress before the
// sequence produces a value.
type Easing func(progress float32) float32

// Linear returns progress unchanged.
func Linear(progress float32) float32 {
	return progress
}

// EaseCubicIn starts slowly and accelerates.
func EaseCubicIn(progress float32) float32 {
	return progress * progress * progress
}

// EaseCubicOut starts fast and decelerates.
func EaseCubicOut(progress float32) float32 {
	inverse := 1 - progress
	return 1 - inverse*inverse*inverse
}

// EaseCubicInOut accelerates through the first half and decelerates through
// the second.
func EaseCubicInOut(progress float32) float32 {
	if progress < 0.5 {
		return 4 * progress * progress * progress
	}
	inverse := 2 - 2*progress
	return 1 - inverse*inverse*inverse/2
}

// Hold returns an easing that stays at 0 for the first fraction of the span,
// then runs easing over the remainder. It folds a pause into a single track,
// for a value that must hold before it moves. A fraction of 0 is easing
// itself; a nil easing is Linear.
func Hold(fraction float32, easing Easing) Easing {
	if easing == nil {
		easing = Linear
	}
	return func(progress float32) float32 {
		if progress <= fraction {
			return 0
		}
		return easing((progress - fraction) / (1 - fraction))
	}
}

// Reverse returns an easing that plays easing backwards, so an ease-out
// becomes the matching ease-in. A nil easing is Linear.
func Reverse(easing Easing) Easing {
	if easing == nil {
		easing = Linear
	}
	return func(progress float32) float32 {
		return 1 - easing(1-progress)
	}
}
