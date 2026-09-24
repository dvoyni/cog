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

// Mods is a bitmask of modifier keys held during an input event. Bit positions
// mirror gogpu/gpucontext.Modifiers so a driver maps them with a plain cast.
// Mods.Has reports whether all of a mask's bits are set.
type Mods = internal.Mods

const (
	ModShift    = internal.ModShift
	ModCtrl     = internal.ModCtrl
	ModAlt      = internal.ModAlt
	ModSuper    = internal.ModSuper
	ModCapsLock = internal.ModCapsLock
	ModNumLock  = internal.ModNumLock
)

// Pos is a pointer position in logical window coordinates (DIP).
type Pos = internal.Pos

// Change is a single input delta. Build it with the KeyChange/PointerChange/
// ScrollChange/TextChange constructors and pass it to ApplyCmd; its fields are
// unexported because drivers construct changes, they don't inspect them.
type Change = internal.Change

// ActionKind tags the variant of an Action. The seven kinds are the whole
// vocabulary of a synthetic input sequence: there is no compound click and no
// compound press, because two spellings of a click would make a caller choose
// between them.
type ActionKind = internal.ActionKind

const (
	// ActionKeyDown presses Key. Pressing a key already down is a no-op.
	ActionKeyDown = internal.ActionKeyDown
	// ActionKeyUp releases Key. Releasing a key that is not down is a silent
	// no-op, exactly as the state itself already treats it.
	ActionKeyUp = internal.ActionKeyUp
	// ActionMove puts the pointer at X, Y in window units.
	ActionMove = internal.ActionMove
	// ActionMoveBy moves the pointer by Dx, Dy from wherever it is. The
	// resolution happens under the write lock, which is the one thing the
	// caller's own arithmetic over a returned position cannot do.
	ActionMoveBy = internal.ActionMoveBy
	// ActionScroll reports a scroll delta of Dx, Dy. cog has no unit to
	// promise here: the value is whatever a driver would have passed through.
	ActionScroll = internal.ActionScroll
	// ActionText enters Text one rune at a time. It emits text changes only
	// and presses no keys, because a faithful rendering is a keyboard-layout
	// problem with no consumer in cog; a game that reads keys wants
	// ActionKeyDown and ActionKeyUp.
	ActionText = internal.ActionText
	// ActionDelay waits Ms milliseconds of wall clock. It is the only step
	// that splits a sequence: everything between two delays lands in one tick.
	ActionDelay = internal.ActionDelay
)

// Action is one step of a synthetic input sequence: Do selects the step kind,
// X and Y are move's absolute pointer position, Dx and Dy are move_by's and
// scroll's delta, Key is key_down's and key_up's key, Text is what text enters
// and Ms is how long delay waits.
//
// Its fields are exported, unlike Change's. A Change is a driver's internal
// delta, constructed and never inspected; an Action is a script, and the thing
// writing it is usually outside the process. The omitempty fields are the
// tagged union flattened, which keeps the JSON schema one object rather than a
// oneOf.
type Action = internal.Action
