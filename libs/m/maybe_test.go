package m

import (
	"encoding/json"
	"testing"
)

func TestAZeroMaybeIsAbsent(t *testing.T) {
	var clear Maybe[Color]
	if value, ok := clear.Get(); ok {
		t.Fatalf("the zero Maybe reads as present, holding %v", value)
	}
	if got := clear.Or(White); got != White {
		t.Fatalf("Or on the zero Maybe = %v, want the fallback", got)
	}
}

// A present zero is the case Maybe exists for: a clear depth of zero, a colour
// of transparent black, are real values and must not read as "not set".
func TestSomeOfAZeroValueIsPresent(t *testing.T) {
	depth := Some[float32](0)
	value, ok := depth.Get()
	if !ok || value != 0 {
		t.Fatalf("Some(0).Get() = %v, %v; want 0, true", value, ok)
	}
	if got := depth.Or(1); got != 0 {
		t.Fatalf("Some(0).Or(1) = %v, want the held 0", got)
	}
}

func TestMaybeIsComparable(t *testing.T) {
	if Some[float32](1) != Some[float32](1) {
		t.Fatal("two Somes of one value are unequal")
	}
	if Some[float32](0) == (Maybe[float32]{}) {
		t.Fatal("Some(0) equals the absent Maybe")
	}
}

// A Maybe crosses JSON exactly as the pointer it replaced did: an absent one
// is left out under omitzero, a missing or null field reads as absent, and a
// present zero is written and read back as the zero it is.
func TestMaybeCrossesJSONAsAPointerDid(t *testing.T) {
	type filter struct {
		From Maybe[int]  `json:"from,omitzero"`
		To   Maybe[int]  `json:"to,omitzero"`
		Size Maybe[Vec2] `json:"size,omitzero"`
	}

	encoded, err := json.Marshal(filter{From: Some(0), Size: Some(Vec2{X: 1, Y: 2})})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got, want := string(encoded), `{"from":0,"size":{"X":1,"Y":2}}`; got != want {
		t.Fatalf("encoded = %s, want %s", got, want)
	}

	for _, document := range []string{`{}`, `{"from":null,"to":null,"size":null}`} {
		var decoded filter
		if err := json.Unmarshal([]byte(document), &decoded); err != nil {
			t.Fatalf("unmarshal %s: %v", document, err)
		}
		if decoded != (filter{}) {
			t.Errorf("%s decoded to %+v, want every field absent", document, decoded)
		}
	}

	var decoded filter
	if err := json.Unmarshal([]byte(`{"from":0,"to":7}`), &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.From != Some(0) || decoded.To != Some(7) || decoded.Size.Present() {
		t.Errorf("decoded = %+v, want from 0 and to 7 present and size absent", decoded)
	}
}

// Without omitzero there is nothing to leave out, so an absent Maybe is
// written as the null a nil pointer was.
func TestAnAbsentMaybeWithoutOmitzeroIsNull(t *testing.T) {
	encoded, err := json.Marshal(struct {
		Depth Maybe[float32] `json:"depth"`
	}{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got, want := string(encoded), `{"depth":null}`; got != want {
		t.Fatalf("encoded = %s, want %s", got, want)
	}
}

func TestAMalformedPresentValueIsAnError(t *testing.T) {
	var depth Maybe[float32]
	if err := json.Unmarshal([]byte(`"deep"`), &depth); err == nil {
		t.Fatalf("a string decoded into Maybe[float32] as %+v, want an error", depth)
	}
}

func TestPresentSaysWhetherAValueIsHeld(t *testing.T) {
	if (Maybe[int]{}).Present() {
		t.Error("the zero Maybe reads as present")
	}
	if !Some(0).Present() {
		t.Error("Some(0) reads as absent")
	}
}
