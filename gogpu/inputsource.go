package gogpu

import (
	"github.com/dvoyni/cog/platform"
	"github.com/gogpu/gpucontext"
)

// inputSource is the plugin's platform.InputSourceSocket: it reports the
// input the window system's windows report to it. platform calls it on the
// main thread alone.
type inputSource struct {
	// plugin holds what the next AppendChanges reports.
	plugin *Plugin
}

// AppendChanges appends to changes the input the windows reported since its
// last call, in the order it happened, and answers it.
func (s inputSource) AppendChanges(changes []platform.InputChange) []platform.InputChange {
	changes = append(changes, s.plugin.inputChanges...)
	s.plugin.inputChanges = s.plugin.inputChanges[:0]
	return changes
}

// Label answers 0: key labels are not built yet.
func (inputSource) Label(platform.Key) rune { return 0 }

// reportPointer reports event in window from the next AppendChanges. A
// cancel, and a button no Key names, report nothing.
func (s inputSource) reportPointer(window platform.Window, event gpucontext.PointerEvent) {
	change := platform.InputChange{Window: window, Position: platform.Point{X: float32(event.X), Y: float32(event.Y)}}
	switch event.Type {
	case gpucontext.PointerMove:
		change.Kind = platform.InputChangePointerMove
		change.PointerDelta = platform.Point{X: float32(event.DeltaX), Y: float32(event.DeltaY)}
	case gpucontext.PointerEnter:
		change.Kind = platform.InputChangePointerEnter
	case gpucontext.PointerLeave:
		change.Kind = platform.InputChangePointerLeave
	case gpucontext.PointerDown, gpucontext.PointerUp:
		if event.Button < 0 || int(event.Button) >= len(mouseButtonKeys) {
			return
		}
		change.Kind = platform.InputChangeKey
		change.Key = mouseButtonKeys[event.Button]
		change.Pressed = event.Type == gpucontext.PointerDown
		change.Modifiers = platform.Modifiers(event.Modifiers)
	default:
		return
	}
	s.plugin.inputChanges = append(s.plugin.inputChanges, change)
}

// reportKey reports from the next AppendChanges key pressed in window, or
// released when pressed is false, with modifiers. A press of a key already
// held is a repeat.
func (s inputSource) reportKey(window platform.Window, key gpucontext.Key, modifiers gpucontext.Modifiers, pressed bool) {
	change := platform.InputChange{Kind: platform.InputChangeKey, Window: window, Pressed: pressed, Modifiers: platform.Modifiers(modifiers)}
	if int(key) < len(keyboardKeys) {
		change.Key = keyboardKeys[key]
	}
	change.Repeat = pressed && s.plugin.heldKeys.Has(int(change.Key))
	if pressed {
		s.plugin.heldKeys.Set(int(change.Key))
	} else {
		s.plugin.heldKeys.Unset(int(change.Key))
	}
	s.plugin.inputChanges = append(s.plugin.inputChanges, change)
}

// keyboardKeys is the Key of each gogpu key cog names, indexed by gogpu's
// key; a key between them, or past the last, is KeyUnknown.
var keyboardKeys = [...]platform.Key{
	gpucontext.KeyA:              platform.KeyA,
	gpucontext.KeyB:              platform.KeyB,
	gpucontext.KeyC:              platform.KeyC,
	gpucontext.KeyD:              platform.KeyD,
	gpucontext.KeyE:              platform.KeyE,
	gpucontext.KeyF:              platform.KeyF,
	gpucontext.KeyG:              platform.KeyG,
	gpucontext.KeyH:              platform.KeyH,
	gpucontext.KeyI:              platform.KeyI,
	gpucontext.KeyJ:              platform.KeyJ,
	gpucontext.KeyK:              platform.KeyK,
	gpucontext.KeyL:              platform.KeyL,
	gpucontext.KeyM:              platform.KeyM,
	gpucontext.KeyN:              platform.KeyN,
	gpucontext.KeyO:              platform.KeyO,
	gpucontext.KeyP:              platform.KeyP,
	gpucontext.KeyQ:              platform.KeyQ,
	gpucontext.KeyR:              platform.KeyR,
	gpucontext.KeyS:              platform.KeyS,
	gpucontext.KeyT:              platform.KeyT,
	gpucontext.KeyU:              platform.KeyU,
	gpucontext.KeyV:              platform.KeyV,
	gpucontext.KeyW:              platform.KeyW,
	gpucontext.KeyX:              platform.KeyX,
	gpucontext.KeyY:              platform.KeyY,
	gpucontext.KeyZ:              platform.KeyZ,
	gpucontext.Key0:              platform.Key0,
	gpucontext.Key1:              platform.Key1,
	gpucontext.Key2:              platform.Key2,
	gpucontext.Key3:              platform.Key3,
	gpucontext.Key4:              platform.Key4,
	gpucontext.Key5:              platform.Key5,
	gpucontext.Key6:              platform.Key6,
	gpucontext.Key7:              platform.Key7,
	gpucontext.Key8:              platform.Key8,
	gpucontext.Key9:              platform.Key9,
	gpucontext.KeyF1:             platform.KeyF1,
	gpucontext.KeyF2:             platform.KeyF2,
	gpucontext.KeyF3:             platform.KeyF3,
	gpucontext.KeyF4:             platform.KeyF4,
	gpucontext.KeyF5:             platform.KeyF5,
	gpucontext.KeyF6:             platform.KeyF6,
	gpucontext.KeyF7:             platform.KeyF7,
	gpucontext.KeyF8:             platform.KeyF8,
	gpucontext.KeyF9:             platform.KeyF9,
	gpucontext.KeyF10:            platform.KeyF10,
	gpucontext.KeyF11:            platform.KeyF11,
	gpucontext.KeyF12:            platform.KeyF12,
	gpucontext.KeyF13:            platform.KeyF13,
	gpucontext.KeyF14:            platform.KeyF14,
	gpucontext.KeyF15:            platform.KeyF15,
	gpucontext.KeyF16:            platform.KeyF16,
	gpucontext.KeyF17:            platform.KeyF17,
	gpucontext.KeyF18:            platform.KeyF18,
	gpucontext.KeyF19:            platform.KeyF19,
	gpucontext.KeyF20:            platform.KeyF20,
	gpucontext.KeyF21:            platform.KeyF21,
	gpucontext.KeyF22:            platform.KeyF22,
	gpucontext.KeyF23:            platform.KeyF23,
	gpucontext.KeyF24:            platform.KeyF24,
	gpucontext.KeyEscape:         platform.KeyEscape,
	gpucontext.KeyTab:            platform.KeyTab,
	gpucontext.KeyBackspace:      platform.KeyBackspace,
	gpucontext.KeyEnter:          platform.KeyEnter,
	gpucontext.KeySpace:          platform.KeySpace,
	gpucontext.KeyInsert:         platform.KeyInsert,
	gpucontext.KeyDelete:         platform.KeyDelete,
	gpucontext.KeyHome:           platform.KeyHome,
	gpucontext.KeyEnd:            platform.KeyEnd,
	gpucontext.KeyPageUp:         platform.KeyPageUp,
	gpucontext.KeyPageDown:       platform.KeyPageDown,
	gpucontext.KeyLeft:           platform.KeyLeft,
	gpucontext.KeyRight:          platform.KeyRight,
	gpucontext.KeyUp:             platform.KeyUp,
	gpucontext.KeyDown:           platform.KeyDown,
	gpucontext.KeyLeftShift:      platform.KeyLeftShift,
	gpucontext.KeyRightShift:     platform.KeyRightShift,
	gpucontext.KeyLeftControl:    platform.KeyLeftControl,
	gpucontext.KeyRightControl:   platform.KeyRightControl,
	gpucontext.KeyLeftAlt:        platform.KeyLeftAlt,
	gpucontext.KeyRightAlt:       platform.KeyRightAlt,
	gpucontext.KeyLeftSuper:      platform.KeyLeftSuper,
	gpucontext.KeyRightSuper:     platform.KeyRightSuper,
	gpucontext.KeyMinus:          platform.KeyMinus,
	gpucontext.KeyEqual:          platform.KeyEqual,
	gpucontext.KeyLeftBracket:    platform.KeyLeftBracket,
	gpucontext.KeyRightBracket:   platform.KeyRightBracket,
	gpucontext.KeyBackslash:      platform.KeyBackslash,
	gpucontext.KeySemicolon:      platform.KeySemicolon,
	gpucontext.KeyApostrophe:     platform.KeyApostrophe,
	gpucontext.KeyGrave:          platform.KeyGrave,
	gpucontext.KeyComma:          platform.KeyComma,
	gpucontext.KeyPeriod:         platform.KeyPeriod,
	gpucontext.KeySlash:          platform.KeySlash,
	gpucontext.KeyIntlBackslash:  platform.KeyIntlBackslash,
	gpucontext.KeyIntlYen:        platform.KeyIntlYen,
	gpucontext.KeyNumpad0:        platform.KeyNumpad0,
	gpucontext.KeyNumpad1:        platform.KeyNumpad1,
	gpucontext.KeyNumpad2:        platform.KeyNumpad2,
	gpucontext.KeyNumpad3:        platform.KeyNumpad3,
	gpucontext.KeyNumpad4:        platform.KeyNumpad4,
	gpucontext.KeyNumpad5:        platform.KeyNumpad5,
	gpucontext.KeyNumpad6:        platform.KeyNumpad6,
	gpucontext.KeyNumpad7:        platform.KeyNumpad7,
	gpucontext.KeyNumpad8:        platform.KeyNumpad8,
	gpucontext.KeyNumpad9:        platform.KeyNumpad9,
	gpucontext.KeyNumpadDecimal:  platform.KeyNumpadDecimal,
	gpucontext.KeyNumpadDivide:   platform.KeyNumpadDivide,
	gpucontext.KeyNumpadMultiply: platform.KeyNumpadMultiply,
	gpucontext.KeyNumpadSubtract: platform.KeyNumpadSubtract,
	gpucontext.KeyNumpadAdd:      platform.KeyNumpadAdd,
	gpucontext.KeyNumpadEnter:    platform.KeyNumpadEnter,
	gpucontext.KeyNumpadEqual:    platform.KeyNumpadEqual,
	gpucontext.KeyCapsLock:       platform.KeyCapsLock,
	gpucontext.KeyScrollLock:     platform.KeyScrollLock,
	gpucontext.KeyNumLock:        platform.KeyNumLock,
	gpucontext.KeyPrintScreen:    platform.KeyPrintScreen,
	gpucontext.KeyPause:          platform.KeyPause,
	gpucontext.KeyContextMenu:    platform.KeyContextMenu,
}

// mouseButtonKeys is the Key of each gogpu button, indexed by gogpu's button;
// one past the last, the pen's eraser, or below the first, no button, has
// none.
var mouseButtonKeys = [...]platform.Key{
	gpucontext.ButtonLeft:   platform.KeyMouseLeft,
	gpucontext.ButtonMiddle: platform.KeyMouseMiddle,
	gpucontext.ButtonRight:  platform.KeyMouseRight,
	gpucontext.ButtonX1:     platform.KeyMouse4,
	gpucontext.ButtonX2:     platform.KeyMouse5,
}
