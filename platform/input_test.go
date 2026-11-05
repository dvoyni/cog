package platform_test

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strings"
	"testing"

	"github.com/dvoyni/cog/platform"
)

// openFocused opens a window, pumps it open and focused, and drops what that
// published.
func openFocused(t *testing.T, h *harness) platform.Window {
	t.Helper()
	window := h.open(platform.WindowEntry{Title: "main"})
	h.pump()
	h.fake.inputChanges = append(h.fake.inputChanges, platform.InputChange{Kind: platform.InputChangeFocus, Window: window, Focused: true})
	h.pump()
	want := []any{platform.WindowOpenedEvent{Window: window}, platform.FocusEvent{Window: window, Focused: true}}
	if got := h.takePublished(); !slices.Equal(got, want) {
		t.Fatalf("opening and focusing published %v, want %v", got, want)
	}
	if !h.manager.IsFocused(window) {
		t.Fatal("the window is not focused after the focus change")
	}
	return window
}

// keyChange is a key change of window.
func keyChange(window platform.Window, key platform.Key, pressed, repeat bool, modifiers platform.Modifiers) platform.InputChange {
	return platform.InputChange{Kind: platform.InputChangeKey, Window: window, Key: key, Pressed: pressed, Repeat: repeat, Modifiers: modifiers}
}

func TestKeysAndTextArriveInOrderAndTheStateHoldsLevels(t *testing.T) {
	h := spawn(t, newFake())
	window := openFocused(t, h)

	mousePress := keyChange(window, platform.KeyMouseLeft, true, false, platform.ModifierShift)
	mousePress.Position = platform.Point{X: 10.5, Y: 20}
	h.fake.inputChanges = append(h.fake.inputChanges,
		keyChange(window, platform.KeyW, true, false, 0),
		platform.InputChange{Kind: platform.InputChangeText, Window: window, Char: 'w'},
		keyChange(window, platform.KeyW, true, true, 0),
		platform.InputChange{Kind: platform.InputChangeText, Window: window, Char: 'w'},
		keyChange(window, platform.KeyLeftShift, true, false, platform.ModifierShift),
		platform.InputChange{Kind: platform.InputChangeText, Window: window, Char: 'W'},
		keyChange(window, platform.KeyW, false, false, platform.ModifierShift),
		mousePress,
	)
	h.pump()

	want := []any{
		platform.KeyEvent{Window: window, Key: platform.KeyW, Pressed: true},
		platform.TextEvent{Window: window, Char: 'w'},
		platform.KeyEvent{Window: window, Key: platform.KeyW, Pressed: true, Repeat: true},
		platform.TextEvent{Window: window, Char: 'w'},
		platform.KeyEvent{Window: window, Key: platform.KeyLeftShift, Pressed: true, Modifiers: platform.ModifierShift},
		platform.TextEvent{Window: window, Char: 'W'},
		platform.KeyEvent{Window: window, Key: platform.KeyW, Modifiers: platform.ModifierShift},
		platform.KeyEvent{Window: window, Key: platform.KeyMouseLeft, Pressed: true, Modifiers: platform.ModifierShift, Position: platform.Point{X: 10.5, Y: 20}},
	}
	if got := h.takePublished(); !slices.Equal(got, want) {
		t.Fatalf("the pump published\n%v\nwant\n%v", got, want)
	}
	for key, held := range map[platform.Key]bool{
		platform.KeyW:          false,
		platform.KeyLeftShift:  true,
		platform.KeyRightShift: false,
		platform.KeyMouseLeft:  true,
	} {
		if got := h.input.IsHeld(key); got != held {
			t.Errorf("%v held is %v, want %v", key, got, held)
		}
	}
}

func TestLosingFocusReleasesEveryHeldKey(t *testing.T) {
	h := spawn(t, newFake())
	window := openFocused(t, h)

	h.fake.inputChanges = append(h.fake.inputChanges,
		keyChange(window, platform.KeyLeftAlt, true, false, platform.ModifierAlt|platform.ModifierCapsLock),
		keyChange(window, platform.KeyMouseRight, true, false, platform.ModifierAlt|platform.ModifierCapsLock),
		keyChange(window, platform.KeyTab, true, false, platform.ModifierAlt|platform.ModifierCapsLock),
		platform.InputChange{Kind: platform.InputChangeFocus, Window: window},
	)
	h.pump()

	want := []any{
		platform.KeyEvent{Window: window, Key: platform.KeyLeftAlt, Pressed: true, Modifiers: platform.ModifierAlt | platform.ModifierCapsLock},
		platform.KeyEvent{Window: window, Key: platform.KeyMouseRight, Pressed: true, Modifiers: platform.ModifierAlt | platform.ModifierCapsLock},
		platform.KeyEvent{Window: window, Key: platform.KeyTab, Pressed: true, Modifiers: platform.ModifierAlt | platform.ModifierCapsLock},
		platform.KeyEvent{Window: window, Key: platform.KeyTab, Modifiers: platform.ModifierCapsLock},
		platform.KeyEvent{Window: window, Key: platform.KeyLeftAlt, Modifiers: platform.ModifierCapsLock},
		platform.KeyEvent{Window: window, Key: platform.KeyMouseRight, Modifiers: platform.ModifierCapsLock},
		platform.FocusEvent{Window: window},
	}
	if got := h.takePublished(); !slices.Equal(got, want) {
		t.Fatalf("the pump published\n%v\nwant\n%v", got, want)
	}
	for _, key := range []platform.Key{platform.KeyLeftAlt, platform.KeyMouseRight, platform.KeyTab} {
		if h.input.IsHeld(key) {
			t.Errorf("%v is held after focus loss", key)
		}
	}
	if h.manager.IsFocused(window) {
		t.Error("the window is focused after focus loss")
	}

	h.fake.inputChanges = append(h.fake.inputChanges, keyChange(window, platform.KeyTab, false, false, 0))
	h.pump()
	if got := h.takePublished(); len(got) != 0 {
		t.Fatalf("releasing a key focus loss released published %v", got)
	}
}

func TestInputNamingNoKeyPanics(t *testing.T) {
	h := spawn(t, newFake())
	window := openFocused(t, h)

	h.fake.inputChanges = append(h.fake.inputChanges,
		keyChange(window, platform.KeyW, true, false, 0),
		keyChange(window, platform.Key(60000), true, false, 0),
	)
	h.pump()
	h.mutex.Lock()
	logged := slices.Clone(h.errors)
	h.mutex.Unlock()
	if len(logged) != 1 || !strings.Contains(logged[0].Error(), "platform: an input change names Key(60000)") {
		t.Fatalf("the pump logged %v, want its panic over the key", logged)
	}
	if got := h.takePublished(); len(got) != 0 {
		t.Fatalf("the pump published %v from a batch naming no key", got)
	}
}

// TestEveryKeyPrintsAsItsNameAndParsesBack reads the Key constants from
// key.go, in order, so a key added without a name fails here.
func TestEveryKeyPrintsAsItsNameAndParsesBack(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "key.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, declaration := range file.Decls {
		group, ok := declaration.(*ast.GenDecl)
		if !ok || group.Tok != token.CONST {
			continue
		}
		for _, spec := range group.Specs {
			for _, name := range spec.(*ast.ValueSpec).Names {
				if name.IsExported() {
					names = append(names, name.Name)
				}
			}
		}
	}
	if len(names) < 100 || names[0] != "KeyUnknown" {
		t.Fatalf("key.go declares %d keys starting %q, want KeyUnknown and the rest", len(names), names[0])
	}
	for index, name := range names {
		key := platform.Key(index)
		if got := key.String(); got != name {
			t.Errorf("Key(%d) prints as %q, want %q", index, got, name)
		}
		parsed, err := platform.ParseKey(name)
		if err != nil || parsed != key {
			t.Errorf("ParseKey(%q) is %v, %v, want %v", name, parsed, err, key)
		}
	}
	beyond := platform.Key(len(names))
	if got, want := beyond.String(), fmt.Sprintf("Key(%d)", len(names)); got != want {
		t.Errorf("the key past the last prints as %q, want %q", got, want)
	}
	if _, err := platform.ParseKey(beyond.String()); err == nil {
		t.Errorf("ParseKey(%q) parsed a key past the last", beyond.String())
	}
}

func TestKeysAreJSONNames(t *testing.T) {
	type binding struct{ Jump, Fire platform.Key }
	encoded, err := json.Marshal(binding{Jump: platform.KeySpace, Fire: platform.KeyMouseLeft})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(encoded), `{"Jump":"KeySpace","Fire":"KeyMouseLeft"}`; got != want {
		t.Fatalf("encoded as %s, want %s", got, want)
	}
	var decoded binding
	if err := json.Unmarshal(encoded, &decoded); err != nil || decoded != (binding{Jump: platform.KeySpace, Fire: platform.KeyMouseLeft}) {
		t.Fatalf("decoded as %v, %v", decoded, err)
	}
	if err := json.Unmarshal([]byte(`{"Jump":"Ctrl"}`), &decoded); err == nil {
		t.Fatal("a name no key has decoded")
	}
}

func TestEdgesHoldForTheLastPumpRunOnly(t *testing.T) {
	h := spawn(t, newFake())
	window := openFocused(t, h)

	h.fake.inputChanges = append(h.fake.inputChanges,
		keyChange(window, platform.KeyA, true, false, 0),
		keyChange(window, platform.KeyS, true, false, 0),
		keyChange(window, platform.KeyS, false, false, 0),
	)
	h.pump()
	expectEdges(t, h, "the run of the presses", map[platform.Key][2]bool{
		platform.KeyA: {true, false},
		platform.KeyS: {true, true},
		platform.KeyD: {false, false},
	})

	h.fake.inputChanges = append(h.fake.inputChanges, keyChange(window, platform.KeyA, true, true, 0))
	h.pump()
	expectEdges(t, h, "the run of a repeat", map[platform.Key][2]bool{
		platform.KeyA: {false, false},
		platform.KeyS: {false, false},
	})

	h.fake.inputChanges = append(h.fake.inputChanges,
		keyChange(window, platform.KeyD, false, false, 0),
		platform.InputChange{Kind: platform.InputChangeFocus, Window: window},
	)
	h.pump()
	expectEdges(t, h, "the run of the focus loss", map[platform.Key][2]bool{
		platform.KeyA: {false, true},
		platform.KeyD: {false, false},
	})

	h.pump()
	expectEdges(t, h, "a run of nothing", map[platform.Key][2]bool{
		platform.KeyA: {false, false},
	})
}

// expectEdges checks JustPressed and JustReleased of each key against want.
func expectEdges(t *testing.T, h *harness, run string, want map[platform.Key][2]bool) {
	t.Helper()
	for key, edges := range want {
		if got := [2]bool{h.input.JustPressed(key), h.input.JustReleased(key)}; got != edges {
			t.Errorf("after %s, %v just pressed and released are %v, want %v", run, key, got, edges)
		}
	}
}
