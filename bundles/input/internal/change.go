package internal

// Mods is a bitmask of modifier keys held during an input event. Bit positions
// mirror gogpu/gpucontext.Modifiers so a driver maps them with a plain cast.
type Mods uint8

const (
	ModShift Mods = 1 << iota
	ModCtrl
	ModAlt
	ModSuper
	ModCapsLock
	ModNumLock
)

// Has reports whether all of x's bits are set in m.
func (m Mods) Has(x Mods) bool { return m&x == x }

// Pos is a pointer position in logical window coordinates (DIP).
type Pos struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// changeKind tags the variant of a Change.
type changeKind uint8

const (
	ChangeKindKey            changeKind = iota // key/button up or down
	ChangeKindPointer                          // pointer moved
	ChangeKindScroll                           // scroll delta
	ChangeKindText                             // text input (one rune)
	ChangeKindClipboardPaste                   // text pasted from the clipboard
)

// Change is a single input delta. Build it with the KeyChange/PointerChange/
// ScrollChange/TextChange constructors and pass it to ApplyCmd; its fields are
// unexported because drivers construct changes, they don't inspect them.
type Change struct {
	kind   changeKind
	key    Key
	mods   Mods
	down   bool
	pos    Pos
	dx, dy float64
	r      rune
	text   string
}

// KeyChange builds a key/button up-or-down change.
func KeyChange(k Key, mods Mods, down bool) Change {
	return Change{kind: ChangeKindKey, key: k, mods: mods, down: down}
}

// PointerChange builds a pointer-move change.
func PointerChange(p Pos) Change { return Change{kind: ChangeKindPointer, pos: p} }

// ScrollChange builds a scroll-delta change.
func ScrollChange(dx, dy float64) Change { return Change{kind: ChangeKindScroll, dx: dx, dy: dy} }

// TextChange builds a text-input change for one rune.
func TextChange(r rune) Change { return Change{kind: ChangeKindText, r: r} }

// ClipboardPasteChange builds a change for text the user pasted from the
// clipboard.
func ClipboardPasteChange(text string) Change {
	return Change{kind: ChangeKindClipboardPaste, text: text}
}
