package input

import "github.com/dvoyni/cog/bundles/input/internal"

// Key identifies a physical input, unifying keyboard keys and mouse buttons so a
// single Pressed/JustPressed/JustReleased path covers both. Keyboard key values
// mirror gogpu/gpucontext.Key (same order/codes) so a driver maps them with a
// plain cast; mouse buttons occupy the negative range so they never collide with
// the non-negative keyboard keys.
//
// A Key prints as its name and parses back from it (String, MarshalText,
// UnmarshalText, ParseKey), so it is a JSON string wherever it appears.
type Key = internal.Key

const KeyUnknown = internal.KeyUnknown

// Letters [1..31].
const (
	KeyA = internal.KeyA
	KeyB = internal.KeyB
	KeyC = internal.KeyC
	KeyD = internal.KeyD
	KeyE = internal.KeyE
	KeyF = internal.KeyF
	KeyG = internal.KeyG
	KeyH = internal.KeyH
	KeyI = internal.KeyI
	KeyJ = internal.KeyJ
	KeyK = internal.KeyK
	KeyL = internal.KeyL
	KeyM = internal.KeyM
	KeyN = internal.KeyN
	KeyO = internal.KeyO
	KeyP = internal.KeyP
	KeyQ = internal.KeyQ
	KeyR = internal.KeyR
	KeyS = internal.KeyS
	KeyT = internal.KeyT
	KeyU = internal.KeyU
	KeyV = internal.KeyV
	KeyW = internal.KeyW
	KeyX = internal.KeyX
	KeyY = internal.KeyY
	KeyZ = internal.KeyZ
)

// Numbers [33..47].
const (
	Key0 = internal.Key0
	Key1 = internal.Key1
	Key2 = internal.Key2
	Key3 = internal.Key3
	Key4 = internal.Key4
	Key5 = internal.Key5
	Key6 = internal.Key6
	Key7 = internal.Key7
	Key8 = internal.Key8
	Key9 = internal.Key9
)

// Function keys [49..80].
const (
	KeyF1  = internal.KeyF1
	KeyF2  = internal.KeyF2
	KeyF3  = internal.KeyF3
	KeyF4  = internal.KeyF4
	KeyF5  = internal.KeyF5
	KeyF6  = internal.KeyF6
	KeyF7  = internal.KeyF7
	KeyF8  = internal.KeyF8
	KeyF9  = internal.KeyF9
	KeyF10 = internal.KeyF10
	KeyF11 = internal.KeyF11
	KeyF12 = internal.KeyF12
)

// Navigation [81..112].
const (
	KeyEscape    = internal.KeyEscape
	KeyTab       = internal.KeyTab
	KeyBackspace = internal.KeyBackspace
	KeyEnter     = internal.KeyEnter
	KeySpace     = internal.KeySpace
	KeyInsert    = internal.KeyInsert
	KeyDelete    = internal.KeyDelete
	KeyHome      = internal.KeyHome
	KeyEnd       = internal.KeyEnd
	KeyPageUp    = internal.KeyPageUp
	KeyPageDown  = internal.KeyPageDown
	KeyLeft      = internal.KeyLeft
	KeyRight     = internal.KeyRight
	KeyUp        = internal.KeyUp
	KeyDown      = internal.KeyDown
)

// Modifiers [113..128] (as keys, not the Mods bitmask).
const (
	KeyLeftShift    = internal.KeyLeftShift
	KeyRightShift   = internal.KeyRightShift
	KeyLeftControl  = internal.KeyLeftControl
	KeyRightControl = internal.KeyRightControl
	KeyLeftAlt      = internal.KeyLeftAlt
	KeyRightAlt     = internal.KeyRightAlt
	KeyLeftSuper    = internal.KeyLeftSuper
	KeyRightSuper   = internal.KeyRightSuper
)

// Punctuation [129..160].
const (
	KeyMinus        = internal.KeyMinus
	KeyEqual        = internal.KeyEqual
	KeyLeftBracket  = internal.KeyLeftBracket
	KeyRightBracket = internal.KeyRightBracket
	KeyBackslash    = internal.KeyBackslash
	KeySemicolon    = internal.KeySemicolon
	KeyApostrophe   = internal.KeyApostrophe
	KeyGrave        = internal.KeyGrave
	KeyComma        = internal.KeyComma
	KeyPeriod       = internal.KeyPeriod
	KeySlash        = internal.KeySlash
)

// Numpad [161..192].
const (
	KeyNumpad0        = internal.KeyNumpad0
	KeyNumpad1        = internal.KeyNumpad1
	KeyNumpad2        = internal.KeyNumpad2
	KeyNumpad3        = internal.KeyNumpad3
	KeyNumpad4        = internal.KeyNumpad4
	KeyNumpad5        = internal.KeyNumpad5
	KeyNumpad6        = internal.KeyNumpad6
	KeyNumpad7        = internal.KeyNumpad7
	KeyNumpad8        = internal.KeyNumpad8
	KeyNumpad9        = internal.KeyNumpad9
	KeyNumpadDecimal  = internal.KeyNumpadDecimal
	KeyNumpadDivide   = internal.KeyNumpadDivide
	KeyNumpadMultiply = internal.KeyNumpadMultiply
	KeyNumpadSubtract = internal.KeyNumpadSubtract
	KeyNumpadAdd      = internal.KeyNumpadAdd
	KeyNumpadEnter    = internal.KeyNumpadEnter
)

// Lock and other keys [193..208].
const (
	KeyCapsLock    = internal.KeyCapsLock
	KeyScrollLock  = internal.KeyScrollLock
	KeyNumLock     = internal.KeyNumLock
	KeyPrintScreen = internal.KeyPrintScreen
	KeyPause       = internal.KeyPause
)

// Mouse buttons live in the negative Key range so they never collide with the
// (non-negative) keyboard keys. Values mirror gpucontext.MouseButton negated and
// shifted: MouseButton b maps to Key(-1 - b).
const (
	KeyMouseLeft   = internal.KeyMouseLeft
	KeyMouseRight  = internal.KeyMouseRight
	KeyMouseMiddle = internal.KeyMouseMiddle
	KeyMouse4      = internal.KeyMouse4
	KeyMouse5      = internal.KeyMouse5
)

// ParseKey resolves a key name, or the "#<n>" printed form of an unnamed key.
// It is exported because a name for a key is something config files and debug
// tools want as much as an agent does.
func ParseKey(s string) (Key, error) { return internal.ParseKey(s) }
