package input

import (
	"encoding/json"
	"regexp"
	"slices"
	"testing"
)

// The trap, asserted in code because prose loses to a model's prior. The
// numeric form is cog's own key code and nothing else: every other numbering a
// caller has ever seen says 13 is Enter, and here it is the letter M.
func TestKey_TheNumberIsCogsOwnKeyCodeAndNotAnyoneElses(t *testing.T) {
	thirteen, err := ParseKey("#13")
	if err != nil {
		t.Fatalf("#13: %v", err)
	}
	if thirteen != KeyM {
		t.Errorf("#13 parsed to %d, want KeyM (%d) — ASCII, keyCode and HID all say Enter", thirteen, KeyM)
	}
	if KeyEnter == 13 {
		t.Fatal("KeyEnter is 13, so the trap this test documents no longer exists")
	}
	if got := KeyEnter.String(); got != "enter" {
		t.Errorf("KeyEnter printed as %q, want enter", got)
	}
	eightyFour, err := ParseKey("#84")
	if err != nil {
		t.Fatalf("#84: %v", err)
	}
	if eightyFour != KeyEnter {
		t.Errorf("#84 parsed to %d, want KeyEnter (%d)", eightyFour, KeyEnter)
	}
}

// A driver can produce a Key the table does not name, and the down-set prints
// on every response, so what is printed has to parse back. Negative values
// round-trip too, because mouse buttons live below zero.
func TestKey_AnUnnamedKeyRoundTripsThroughItsPrintedForm(t *testing.T) {
	for _, key := range []Key{Key(9999), Key(-42), Key(208), Key(32)} {
		if _, named := keyNames[key]; named {
			t.Fatalf("%d is named, so it does not exercise the printed form", key)
		}
		printed := key.String()
		back, err := ParseKey(printed)
		if err != nil {
			t.Fatalf("%d printed as %q, which does not parse: %v", key, printed, err)
		}
		if back != key {
			t.Errorf("%d round-tripped through %q to %d", key, printed, back)
		}
	}
}

// The table is the single source of truth for both directions, so every name
// it prints is a name it reads, and no two keys share one.
func TestKey_EveryNameRoundTripsBothWays(t *testing.T) {
	for key, name := range keyNames {
		if got := key.String(); got != name {
			t.Errorf("%d printed as %q, want %q", key, got, name)
		}
		back, err := ParseKey(name)
		if err != nil {
			t.Errorf("%q does not parse: %v", name, err)
			continue
		}
		if back != key {
			t.Errorf("%q parsed to %d, want %d", name, back, key)
		}
	}
	if len(keyByName) != len(keyNames) {
		t.Errorf("%d names index %d keys; two keys share a name", len(keyByName), len(keyNames))
	}
}

// What ParseKey refuses and what the published pattern refuses have to be the
// same set, or a value the schema lets through fails at the engine instead.
func TestKey_RejectsExactlyWhatTheSchemaRejects(t *testing.T) {
	for _, bad := range []string{"", "Enter", "enter ", "#", "#+5", "#1.5", "#a", "return", "13", "##1"} {
		if _, err := ParseKey(bad); err == nil {
			t.Errorf("%q parsed, want a refusal", bad)
		}
	}
	values, other := Key(0).TextValues()
	pattern := regexp.MustCompile(other)
	for _, bad := range []string{"", "Enter", "enter ", "#", "#+5", "#1.5", "#a", "return", "13", "##1"} {
		if slices.Contains(values, bad) || pattern.MatchString(bad) {
			t.Errorf("the schema admits %q, which ParseKey refuses", bad)
		}
	}
	for _, good := range []string{"w", "escape", "mouse_left", "#13", "#-42"} {
		if !slices.Contains(values, good) && !pattern.MatchString(good) {
			t.Errorf("the schema refuses %q, which ParseKey accepts", good)
		}
	}
}

// The enum the broker publishes is the table's own names, in a stable order,
// and every one of them parses.
func TestKey_TextValuesPublishTheTable(t *testing.T) {
	values, other := Key(0).TextValues()
	if other != keyPattern {
		t.Errorf("pattern = %q, want %q", other, keyPattern)
	}
	if len(values) != len(keyNames) {
		t.Fatalf("%d values for %d names", len(values), len(keyNames))
	}
	for _, wanted := range []string{"w", "escape", "space", "mouse_left", "enter", "left_control"} {
		if !slices.Contains(values, wanted) {
			t.Errorf("the enum never offers %q", wanted)
		}
	}
	again, _ := Key(0).TextValues()
	if !slices.Equal(values, again) {
		t.Error("the enum is not stable between calls")
	}
	values[0] = "mutated"
	third, _ := Key(0).TextValues()
	if third[0] == "mutated" {
		t.Error("a caller can edit the published enum")
	}
}

// A Key is a JSON string wherever it appears — in a step and in the down-set —
// rather than the integer reflecting on its Go kind would infer.
func TestKey_CrossesAsAJSONString(t *testing.T) {
	encoded, err := json.Marshal(Action{Do: ActionKeyDown, Key: KeyW})
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"do":"key_down","key":"w"}` {
		t.Errorf("encoded as %s", encoded)
	}

	var step Action
	if err := json.Unmarshal([]byte(`{"do":"key_up","key":"mouse_left"}`), &step); err != nil {
		t.Fatal(err)
	}
	if step.Key != KeyMouseLeft {
		t.Errorf("key = %d, want KeyMouseLeft", step.Key)
	}

	seam, err := json.Marshal(StateResponse{Down: []Key{KeyLeftControl, Key(9999)}})
	if err != nil {
		t.Fatal(err)
	}
	if string(seam) != `{"down":["left_control","#9999"],"pointer":{"x":0,"y":0}}` {
		t.Errorf("the seam encoded as %s", seam)
	}

	if err := json.Unmarshal([]byte(`{"do":"key_down","key":"ctrl"}`), &step); err == nil {
		t.Error("a key name nothing names was accepted")
	}
}
