package internal

// KeyEvent is published for each applied key change.
type KeyEvent struct {
	Key  Key
	Mods Mods
	Down bool
}

// PointerEvent is published for each applied pointer change.
type PointerEvent struct{ Pos Pos }

// ScrollEvent is published for each applied scroll change.
type ScrollEvent struct{ Dx, Dy float64 }

// TextEvent is published for each applied text change.
type TextEvent struct{ Rune rune }

// ClipboardPasteEvent is published for each applied clipboard paste.
type ClipboardPasteEvent struct{ Text string }
