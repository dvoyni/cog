package input

import "github.com/dvoyni/cog/bundles/input/internal"

// KeyEvent is published for each applied key change.
type KeyEvent = internal.KeyEvent

// PointerEvent is published for each applied pointer change.
type PointerEvent = internal.PointerEvent

// ScrollEvent is published for each applied scroll change.
type ScrollEvent = internal.ScrollEvent

// TextEvent is published for each applied text change.
type TextEvent = internal.TextEvent

// ClipboardPasteEvent is published for each applied clipboard paste.
type ClipboardPasteEvent = internal.ClipboardPasteEvent
