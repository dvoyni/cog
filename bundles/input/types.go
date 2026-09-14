package input

import "github.com/dvoyni/cog/bundles/input/internal/types"

// Key identifies a physical input, unifying keyboard keys and mouse buttons so a
// single Pressed/JustPressed/JustReleased path covers both. Keyboard key values
// mirror gogpu/gpucontext.Key (same order/codes) so a driver maps them with a
// plain cast; mouse buttons occupy the negative range so they never collide with
// the non-negative keyboard keys.
//
// A Key prints as its name and parses back from it (String, MarshalText,
// UnmarshalText, ParseKey), so it is a JSON string wherever it appears.
type Key = types.Key

const KeyUnknown = types.KeyUnknown

// Letters [1..31].
const (
	KeyA = types.KeyA
	KeyB = types.KeyB
	KeyC = types.KeyC
	KeyD = types.KeyD
	KeyE = types.KeyE
	KeyF = types.KeyF
	KeyG = types.KeyG
	KeyH = types.KeyH
	KeyI = types.KeyI
	KeyJ = types.KeyJ
	KeyK = types.KeyK
	KeyL = types.KeyL
	KeyM = types.KeyM
	KeyN = types.KeyN
	KeyO = types.KeyO
	KeyP = types.KeyP
	KeyQ = types.KeyQ
	KeyR = types.KeyR
	KeyS = types.KeyS
	KeyT = types.KeyT
	KeyU = types.KeyU
	KeyV = types.KeyV
	KeyW = types.KeyW
	KeyX = types.KeyX
	KeyY = types.KeyY
	KeyZ = types.KeyZ
)

// Numbers [33..47].
const (
	Key0 = types.Key0
	Key1 = types.Key1
	Key2 = types.Key2
	Key3 = types.Key3
	Key4 = types.Key4
	Key5 = types.Key5
	Key6 = types.Key6
	Key7 = types.Key7
	Key8 = types.Key8
	Key9 = types.Key9
)

// Function keys [49..80].
const (
	KeyF1  = types.KeyF1
	KeyF2  = types.KeyF2
	KeyF3  = types.KeyF3
	KeyF4  = types.KeyF4
	KeyF5  = types.KeyF5
	KeyF6  = types.KeyF6
	KeyF7  = types.KeyF7
	KeyF8  = types.KeyF8
	KeyF9  = types.KeyF9
	KeyF10 = types.KeyF10
	KeyF11 = types.KeyF11
	KeyF12 = types.KeyF12
)

// Navigation [81..112].
const (
	KeyEscape    = types.KeyEscape
	KeyTab       = types.KeyTab
	KeyBackspace = types.KeyBackspace
	KeyEnter     = types.KeyEnter
	KeySpace     = types.KeySpace
	KeyInsert    = types.KeyInsert
	KeyDelete    = types.KeyDelete
	KeyHome      = types.KeyHome
	KeyEnd       = types.KeyEnd
	KeyPageUp    = types.KeyPageUp
	KeyPageDown  = types.KeyPageDown
	KeyLeft      = types.KeyLeft
	KeyRight     = types.KeyRight
	KeyUp        = types.KeyUp
	KeyDown      = types.KeyDown
)

// Modifiers [113..128] (as keys, not the Mods bitmask).
const (
	KeyLeftShift    = types.KeyLeftShift
	KeyRightShift   = types.KeyRightShift
	KeyLeftControl  = types.KeyLeftControl
	KeyRightControl = types.KeyRightControl
	KeyLeftAlt      = types.KeyLeftAlt
	KeyRightAlt     = types.KeyRightAlt
	KeyLeftSuper    = types.KeyLeftSuper
	KeyRightSuper   = types.KeyRightSuper
)

// Punctuation [129..160].
const (
	KeyMinus        = types.KeyMinus
	KeyEqual        = types.KeyEqual
	KeyLeftBracket  = types.KeyLeftBracket
	KeyRightBracket = types.KeyRightBracket
	KeyBackslash    = types.KeyBackslash
	KeySemicolon    = types.KeySemicolon
	KeyApostrophe   = types.KeyApostrophe
	KeyGrave        = types.KeyGrave
	KeyComma        = types.KeyComma
	KeyPeriod       = types.KeyPeriod
	KeySlash        = types.KeySlash
)

// Numpad [161..192].
const (
	KeyNumpad0        = types.KeyNumpad0
	KeyNumpad1        = types.KeyNumpad1
	KeyNumpad2        = types.KeyNumpad2
	KeyNumpad3        = types.KeyNumpad3
	KeyNumpad4        = types.KeyNumpad4
	KeyNumpad5        = types.KeyNumpad5
	KeyNumpad6        = types.KeyNumpad6
	KeyNumpad7        = types.KeyNumpad7
	KeyNumpad8        = types.KeyNumpad8
	KeyNumpad9        = types.KeyNumpad9
	KeyNumpadDecimal  = types.KeyNumpadDecimal
	KeyNumpadDivide   = types.KeyNumpadDivide
	KeyNumpadMultiply = types.KeyNumpadMultiply
	KeyNumpadSubtract = types.KeyNumpadSubtract
	KeyNumpadAdd      = types.KeyNumpadAdd
	KeyNumpadEnter    = types.KeyNumpadEnter
)

// Lock and other keys [193..208].
const (
	KeyCapsLock    = types.KeyCapsLock
	KeyScrollLock  = types.KeyScrollLock
	KeyNumLock     = types.KeyNumLock
	KeyPrintScreen = types.KeyPrintScreen
	KeyPause       = types.KeyPause
)

// Mouse buttons live in the negative Key range so they never collide with the
// (non-negative) keyboard keys. Values mirror gpucontext.MouseButton negated and
// shifted: MouseButton b maps to Key(-1 - b).
const (
	KeyMouseLeft   = types.KeyMouseLeft
	KeyMouseRight  = types.KeyMouseRight
	KeyMouseMiddle = types.KeyMouseMiddle
	KeyMouse4      = types.KeyMouse4
	KeyMouse5      = types.KeyMouse5
)

// Mods is a bitmask of modifier keys held during an input event. Bit positions
// mirror gogpu/gpucontext.Modifiers so a driver maps them with a plain cast.
// Mods.Has reports whether all of a mask's bits are set.
type Mods = types.Mods

const (
	ModShift    = types.ModShift
	ModCtrl     = types.ModCtrl
	ModAlt      = types.ModAlt
	ModSuper    = types.ModSuper
	ModCapsLock = types.ModCapsLock
	ModNumLock  = types.ModNumLock
)

// Pos is a pointer position in logical window coordinates (DIP).
type Pos = types.Pos

// Change is a single input delta. Build it with the KeyChange/PointerChange/
// ScrollChange/TextChange constructors and pass it to ApplyCmd; its fields are
// unexported because drivers construct changes, they don't inspect them.
type Change = types.Change

// ActionKind tags the variant of an Action. The seven kinds are the whole
// vocabulary of a synthetic input sequence: there is no compound click and no
// compound press, because two spellings of a click would make a caller choose
// between them.
type ActionKind = types.ActionKind

const (
	// ActionKeyDown presses Key. Pressing a key already down is a no-op.
	ActionKeyDown = types.ActionKeyDown
	// ActionKeyUp releases Key. Releasing a key that is not down is a silent
	// no-op, exactly as the state itself already treats it.
	ActionKeyUp = types.ActionKeyUp
	// ActionMove puts the pointer at X, Y in window units.
	ActionMove = types.ActionMove
	// ActionMoveBy moves the pointer by Dx, Dy from wherever it is. The
	// resolution happens under the write lock, which is the one thing the
	// caller's own arithmetic over a returned position cannot do.
	ActionMoveBy = types.ActionMoveBy
	// ActionScroll reports a scroll delta of Dx, Dy. cog has no unit to
	// promise here: the value is whatever a driver would have passed through.
	ActionScroll = types.ActionScroll
	// ActionText enters Text one rune at a time. It emits text changes only
	// and presses no keys, because a faithful rendering is a keyboard-layout
	// problem with no consumer in cog; a game that reads keys wants
	// ActionKeyDown and ActionKeyUp.
	ActionText = types.ActionText
	// ActionDelay waits Ms milliseconds of wall clock. It is the only step
	// that splits a sequence: everything between two delays lands in one tick.
	ActionDelay = types.ActionDelay
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
type Action = types.Action
