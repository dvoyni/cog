package input

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// keyPattern admits the printed form of a key the name table does not cover.
// It is exactly what ParseKey accepts beyond a name, and it is published in the
// schema, so the two cannot drift: a value the schema lets through parses, and
// a value ParseKey rejects never reaches the engine.
const keyPattern = "^#-?[0-9]+$"

// keyNames is the single source of truth for both directions. String and
// MarshalText read it, ParseKey and UnmarshalText read the index built from it,
// and TextValues publishes its names as the schema's enum. A Key absent from it
// has no name and prints as "#<n>".
//
// The constant blocks have deliberate gaps and mapKey is a plain cast from
// gpucontext.Key, so a driver can legitimately produce a Key no name covers.
// That is why the printed form is accepted as input as well: accepting what you
// print is the cheap correctness.
var keyNames = buildKeyNames()

// keyByName and keyList are derived from keyNames and nothing else. keyList is
// ordered by key code rather than alphabetically, so the enum an agent reads
// keeps letters, digits, function keys and the numpad in their blocks.
var keyByName, keyList = indexKeyNames(keyNames)

// buildKeyNames writes the table. The regular runs are looped rather than
// spelled out, because a hand-written list of thirty-eight letters and digits
// is a place for a typo to hide and there is nothing to learn from reading it.
func buildKeyNames() map[Key]string {
	names := map[Key]string{
		KeyUnknown: "unknown",

		KeyEscape:    "escape",
		KeyTab:       "tab",
		KeyBackspace: "backspace",
		KeyEnter:     "enter",
		KeySpace:     "space",
		KeyInsert:    "insert",
		KeyDelete:    "delete",
		KeyHome:      "home",
		KeyEnd:       "end",
		KeyPageUp:    "page_up",
		KeyPageDown:  "page_down",
		KeyLeft:      "left",
		KeyRight:     "right",
		KeyUp:        "up",
		KeyDown:      "down",

		KeyLeftShift:    "left_shift",
		KeyRightShift:   "right_shift",
		KeyLeftControl:  "left_control",
		KeyRightControl: "right_control",
		KeyLeftAlt:      "left_alt",
		KeyRightAlt:     "right_alt",
		KeyLeftSuper:    "left_super",
		KeyRightSuper:   "right_super",

		KeyMinus:        "minus",
		KeyEqual:        "equal",
		KeyLeftBracket:  "left_bracket",
		KeyRightBracket: "right_bracket",
		KeyBackslash:    "backslash",
		KeySemicolon:    "semicolon",
		KeyApostrophe:   "apostrophe",
		KeyGrave:        "grave",
		KeyComma:        "comma",
		KeyPeriod:       "period",
		KeySlash:        "slash",

		KeyNumpadDecimal:  "numpad_decimal",
		KeyNumpadDivide:   "numpad_divide",
		KeyNumpadMultiply: "numpad_multiply",
		KeyNumpadSubtract: "numpad_subtract",
		KeyNumpadAdd:      "numpad_add",
		KeyNumpadEnter:    "numpad_enter",

		KeyCapsLock:    "caps_lock",
		KeyScrollLock:  "scroll_lock",
		KeyNumLock:     "num_lock",
		KeyPrintScreen: "print_screen",
		KeyPause:       "pause",

		KeyMouseLeft:   "mouse_left",
		KeyMouseRight:  "mouse_right",
		KeyMouseMiddle: "mouse_middle",
		KeyMouse4:      "mouse_4",
		KeyMouse5:      "mouse_5",
	}
	for i := range 26 {
		names[KeyA+Key(i)] = string(rune('a' + i))
	}
	for i := range 10 {
		names[Key0+Key(i)] = strconv.Itoa(i)
		names[KeyNumpad0+Key(i)] = "numpad_" + strconv.Itoa(i)
	}
	for i := range 12 {
		names[KeyF1+Key(i)] = "f" + strconv.Itoa(i+1)
	}
	return names
}

// indexKeyNames inverts the table and orders its names. A duplicate name would
// make the two directions disagree, so it panics rather than silently losing
// one: the table is a package constant in everything but syntax, and the panic
// can only fire on a source edit.
func indexKeyNames(names map[Key]string) (map[string]Key, []string) {
	byName := make(map[string]Key, len(names))
	codes := make([]Key, 0, len(names))
	for key, name := range names {
		if clash, taken := byName[name]; taken {
			panic(fmt.Sprintf("input: key name %q names both %d and %d", name, clash, key))
		}
		byName[name] = key
		codes = append(codes, key)
	}
	slices.Sort(codes)
	ordered := make([]string, 0, len(codes))
	for _, key := range codes {
		ordered = append(ordered, names[key])
	}
	return byName, ordered
}

// String reports the key's name, or its printed numeric form when the table
// does not name it.
//
// The number is a cog Key and nothing else. Letters are iota+1, so Key(13) is
// M — while ASCII 13, a browser keyCode 13 and USB HID usage 0x28 all say
// Enter, and KeyEnter is 84.
func (k Key) String() string {
	if name, named := keyNames[k]; named {
		return name
	}
	return "#" + strconv.Itoa(int(k))
}

// MarshalText writes the key's name, so a Key is a JSON string everywhere it
// appears rather than the integer its Go kind implies.
func (k Key) MarshalText() ([]byte, error) { return []byte(k.String()), nil }

// UnmarshalText reads a key name, or the "#<n>" form String prints for a key
// the table does not name.
func (k *Key) UnmarshalText(text []byte) error {
	parsed, err := ParseKey(string(text))
	if err != nil {
		return err
	}
	*k = parsed
	return nil
}

// ParseKey resolves a key name, or the "#<n>" printed form of an unnamed key.
// It is exported because a name for a key is something config files and debug
// tools want as much as an agent does.
func ParseKey(s string) (Key, error) {
	if key, named := keyByName[s]; named {
		return key, nil
	}
	if digits, printed := strings.CutPrefix(s, "#"); printed && numeric(digits) {
		if code, err := strconv.Atoi(digits); err == nil {
			return Key(code), nil
		}
	}
	return KeyUnknown, ErrUnknownKey{Name: s}
}

// numeric reports whether digits is the body of keyPattern: an optional minus
// and at least one digit, and nothing else. strconv.Atoi alone would accept
// "+5", which the schema's pattern does not.
func numeric(digits string) bool {
	digits = strings.TrimPrefix(digits, "-")
	if digits == "" {
		return false
	}
	for i := range len(digits) {
		if digits[i] < '0' || digits[i] > '9' {
			return false
		}
	}
	return true
}

// TextValues reports the names a Key accepts and the pattern admitting the
// printed form of one the table does not name. It is what makes the tool schema
// an anyOf of an enum and a pattern rather than the {"type": "integer"} that
// reflecting on the Go kind would infer, so a mistyped key is rejected by the
// agent's own client instead of reaching the engine.
func (Key) TextValues() (values []string, other string) {
	return slices.Clone(keyList), keyPattern
}
