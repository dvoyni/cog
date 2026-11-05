package platform

import (
	"fmt"
	"strconv"
)

// Key is anything that can be held down: a keyboard key, named by its
// position on a US keyboard whatever the layout, or a mouse button. KeyW is
// the key above KeyS on every keyboard; the character a layout prints on it
// is its label. Keyboard keys and mouse buttons share the one space, so a
// binding is a single lookup.
//
// A Key prints as its name and parses back, also as JSON or any text, so
// bindings in config files and values read "KeyW". The zero Key is
// KeyUnknown, a key the input source could not place.
type Key uint16

const (
	// KeyUnknown is a key the input source could not place.
	KeyUnknown Key = iota

	// Letters, by their position on a US keyboard.
	KeyA
	KeyB
	KeyC
	KeyD
	KeyE
	KeyF
	KeyG
	KeyH
	KeyI
	KeyJ
	KeyK
	KeyL
	KeyM
	KeyN
	KeyO
	KeyP
	KeyQ
	KeyR
	KeyS
	KeyT
	KeyU
	KeyV
	KeyW
	KeyX
	KeyY
	KeyZ

	// The digit row.
	Key0
	Key1
	Key2
	Key3
	Key4
	Key5
	Key6
	Key7
	Key8
	Key9

	// Function keys.
	KeyF1
	KeyF2
	KeyF3
	KeyF4
	KeyF5
	KeyF6
	KeyF7
	KeyF8
	KeyF9
	KeyF10
	KeyF11
	KeyF12
	KeyF13
	KeyF14
	KeyF15
	KeyF16
	KeyF17
	KeyF18
	KeyF19
	KeyF20
	KeyF21
	KeyF22
	KeyF23
	KeyF24

	// Editing and navigation.
	KeyEscape
	KeyTab
	KeyBackspace
	KeyEnter
	KeySpace
	KeyInsert
	KeyDelete
	KeyHome
	KeyEnd
	KeyPageUp
	KeyPageDown
	KeyLeft
	KeyRight
	KeyUp
	KeyDown

	// Modifier keys, left and right apart. AltGr is KeyRightAlt.
	KeyLeftShift
	KeyRightShift
	KeyLeftControl
	KeyRightControl
	KeyLeftAlt
	KeyRightAlt
	KeyLeftSuper
	KeyRightSuper

	// Punctuation, by its position on a US keyboard. KeyIntlBackslash is the
	// ISO key between left Shift and Z, KeyIntlYen the JIS key left of
	// Backspace.
	KeyMinus
	KeyEqual
	KeyLeftBracket
	KeyRightBracket
	KeyBackslash
	KeySemicolon
	KeyApostrophe
	KeyGrave
	KeyComma
	KeyPeriod
	KeySlash
	KeyIntlBackslash
	KeyIntlYen

	// The numeric keypad.
	KeyNumpad0
	KeyNumpad1
	KeyNumpad2
	KeyNumpad3
	KeyNumpad4
	KeyNumpad5
	KeyNumpad6
	KeyNumpad7
	KeyNumpad8
	KeyNumpad9
	KeyNumpadDecimal
	KeyNumpadDivide
	KeyNumpadMultiply
	KeyNumpadSubtract
	KeyNumpadAdd
	KeyNumpadEnter
	KeyNumpadEqual

	// Locks and system keys.
	KeyCapsLock
	KeyScrollLock
	KeyNumLock
	KeyPrintScreen
	KeyPause
	KeyContextMenu

	// Mouse buttons. KeyMouse4 and KeyMouse5 are the side buttons, usually
	// back and forward.
	KeyMouseLeft
	KeyMouseRight
	KeyMouseMiddle
	KeyMouse4
	KeyMouse5

	// keyCount is how many Keys there are: one past the last.
	keyCount
)

// keyNames is every Key's name, by its value.
var keyNames = [keyCount]string{
	KeyUnknown:        "KeyUnknown",
	KeyA:              "KeyA",
	KeyB:              "KeyB",
	KeyC:              "KeyC",
	KeyD:              "KeyD",
	KeyE:              "KeyE",
	KeyF:              "KeyF",
	KeyG:              "KeyG",
	KeyH:              "KeyH",
	KeyI:              "KeyI",
	KeyJ:              "KeyJ",
	KeyK:              "KeyK",
	KeyL:              "KeyL",
	KeyM:              "KeyM",
	KeyN:              "KeyN",
	KeyO:              "KeyO",
	KeyP:              "KeyP",
	KeyQ:              "KeyQ",
	KeyR:              "KeyR",
	KeyS:              "KeyS",
	KeyT:              "KeyT",
	KeyU:              "KeyU",
	KeyV:              "KeyV",
	KeyW:              "KeyW",
	KeyX:              "KeyX",
	KeyY:              "KeyY",
	KeyZ:              "KeyZ",
	Key0:              "Key0",
	Key1:              "Key1",
	Key2:              "Key2",
	Key3:              "Key3",
	Key4:              "Key4",
	Key5:              "Key5",
	Key6:              "Key6",
	Key7:              "Key7",
	Key8:              "Key8",
	Key9:              "Key9",
	KeyF1:             "KeyF1",
	KeyF2:             "KeyF2",
	KeyF3:             "KeyF3",
	KeyF4:             "KeyF4",
	KeyF5:             "KeyF5",
	KeyF6:             "KeyF6",
	KeyF7:             "KeyF7",
	KeyF8:             "KeyF8",
	KeyF9:             "KeyF9",
	KeyF10:            "KeyF10",
	KeyF11:            "KeyF11",
	KeyF12:            "KeyF12",
	KeyF13:            "KeyF13",
	KeyF14:            "KeyF14",
	KeyF15:            "KeyF15",
	KeyF16:            "KeyF16",
	KeyF17:            "KeyF17",
	KeyF18:            "KeyF18",
	KeyF19:            "KeyF19",
	KeyF20:            "KeyF20",
	KeyF21:            "KeyF21",
	KeyF22:            "KeyF22",
	KeyF23:            "KeyF23",
	KeyF24:            "KeyF24",
	KeyEscape:         "KeyEscape",
	KeyTab:            "KeyTab",
	KeyBackspace:      "KeyBackspace",
	KeyEnter:          "KeyEnter",
	KeySpace:          "KeySpace",
	KeyInsert:         "KeyInsert",
	KeyDelete:         "KeyDelete",
	KeyHome:           "KeyHome",
	KeyEnd:            "KeyEnd",
	KeyPageUp:         "KeyPageUp",
	KeyPageDown:       "KeyPageDown",
	KeyLeft:           "KeyLeft",
	KeyRight:          "KeyRight",
	KeyUp:             "KeyUp",
	KeyDown:           "KeyDown",
	KeyLeftShift:      "KeyLeftShift",
	KeyRightShift:     "KeyRightShift",
	KeyLeftControl:    "KeyLeftControl",
	KeyRightControl:   "KeyRightControl",
	KeyLeftAlt:        "KeyLeftAlt",
	KeyRightAlt:       "KeyRightAlt",
	KeyLeftSuper:      "KeyLeftSuper",
	KeyRightSuper:     "KeyRightSuper",
	KeyMinus:          "KeyMinus",
	KeyEqual:          "KeyEqual",
	KeyLeftBracket:    "KeyLeftBracket",
	KeyRightBracket:   "KeyRightBracket",
	KeyBackslash:      "KeyBackslash",
	KeySemicolon:      "KeySemicolon",
	KeyApostrophe:     "KeyApostrophe",
	KeyGrave:          "KeyGrave",
	KeyComma:          "KeyComma",
	KeyPeriod:         "KeyPeriod",
	KeySlash:          "KeySlash",
	KeyIntlBackslash:  "KeyIntlBackslash",
	KeyIntlYen:        "KeyIntlYen",
	KeyNumpad0:        "KeyNumpad0",
	KeyNumpad1:        "KeyNumpad1",
	KeyNumpad2:        "KeyNumpad2",
	KeyNumpad3:        "KeyNumpad3",
	KeyNumpad4:        "KeyNumpad4",
	KeyNumpad5:        "KeyNumpad5",
	KeyNumpad6:        "KeyNumpad6",
	KeyNumpad7:        "KeyNumpad7",
	KeyNumpad8:        "KeyNumpad8",
	KeyNumpad9:        "KeyNumpad9",
	KeyNumpadDecimal:  "KeyNumpadDecimal",
	KeyNumpadDivide:   "KeyNumpadDivide",
	KeyNumpadMultiply: "KeyNumpadMultiply",
	KeyNumpadSubtract: "KeyNumpadSubtract",
	KeyNumpadAdd:      "KeyNumpadAdd",
	KeyNumpadEnter:    "KeyNumpadEnter",
	KeyNumpadEqual:    "KeyNumpadEqual",
	KeyCapsLock:       "KeyCapsLock",
	KeyScrollLock:     "KeyScrollLock",
	KeyNumLock:        "KeyNumLock",
	KeyPrintScreen:    "KeyPrintScreen",
	KeyPause:          "KeyPause",
	KeyContextMenu:    "KeyContextMenu",
	KeyMouseLeft:      "KeyMouseLeft",
	KeyMouseRight:     "KeyMouseRight",
	KeyMouseMiddle:    "KeyMouseMiddle",
	KeyMouse4:         "KeyMouse4",
	KeyMouse5:         "KeyMouse5",
}

// keysByName is every Key by its name.
var keysByName = indexKeyNames()

// ParseKey answers the Key name names, as String prints it, and an error if
// no Key has that name.
func ParseKey(name string) (Key, error) {
	key, found := keysByName[name]
	if !found {
		return KeyUnknown, fmt.Errorf("platform: no key is named %q", name)
	}
	return key, nil
}

// String answers k's name, its constant's: "KeyW". A value past the last Key
// prints as "Key(n)", which parses as nothing.
func (k Key) String() string {
	if !k.isValid() {
		return "Key(" + strconv.Itoa(int(k)) + ")"
	}
	return keyNames[k]
}

// IsMouseButton answers whether k is a mouse button rather than a keyboard
// key.
func (k Key) IsMouseButton() bool { return k >= KeyMouseLeft && k < keyCount }

// MarshalText answers k's name, so a Key is its name in JSON and any text.
func (k Key) MarshalText() ([]byte, error) {
	if !k.isValid() {
		return nil, fmt.Errorf("platform: %v is no key", k)
	}
	return []byte(keyNames[k]), nil
}

// UnmarshalText reads a Key from its name, failing on a name no Key has.
func (k *Key) UnmarshalText(text []byte) error {
	key, err := ParseKey(string(text))
	if err != nil {
		return err
	}
	*k = key
	return nil
}

// isValid answers whether k is one of the Keys.
func (k Key) isValid() bool { return k < keyCount }

// indexKeyNames answers every Key by its name.
func indexKeyNames() map[string]Key {
	byName := make(map[string]Key, keyCount)
	for key, name := range keyNames {
		byName[name] = Key(key)
	}
	return byName
}
