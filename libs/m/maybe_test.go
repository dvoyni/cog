package m

import "testing"

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
