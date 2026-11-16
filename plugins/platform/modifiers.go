package platform

// Modifiers is the bitmask of the modifiers in force at a key change: the
// modifier keys held, and the Caps Lock and Num Lock states.
type Modifiers uint8

const (
	// ModifierShift is either Shift key held.
	ModifierShift Modifiers = 1 << iota
	// ModifierControl is either Control key held.
	ModifierControl
	// ModifierAlt is either Alt key held, Option on macOS. AltGr is
	// KeyRightAlt, held with no ModifierControl.
	ModifierAlt
	// ModifierSuper is either Super key held: Command on macOS, the Windows
	// key elsewhere.
	ModifierSuper
	// ModifierCapsLock is Caps Lock on.
	ModifierCapsLock
	// ModifierNumLock is Num Lock on.
	ModifierNumLock
)
