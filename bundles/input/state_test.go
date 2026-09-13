package input

import "testing"

// A key press is live immediately; JustPressed appears only for the tick after
// advance, then clears; the key stays held until released.
func TestStateKeyPressEdges(t *testing.T) {
	s := newState()

	s.apply(KeyChange(KeyA, 0, true))
	if !s.Pressed(KeyA) {
		t.Fatal("Pressed(A) should be true immediately")
	}
	if s.JustPressed(KeyA) {
		t.Fatal("JustPressed should be false before advance")
	}

	s.advance()
	if !s.JustPressed(KeyA) || !s.Pressed(KeyA) {
		t.Fatal("JustPressed(A) and Pressed(A) should be true this tick")
	}

	s.advance()
	if s.JustPressed(KeyA) {
		t.Fatal("JustPressed(A) should clear next tick")
	}
	if !s.Pressed(KeyA) {
		t.Fatal("Pressed(A) should stay true while held")
	}

	s.apply(KeyChange(KeyA, 0, false))
	if s.Pressed(KeyA) {
		t.Fatal("Pressed(A) should be false after release")
	}
	s.advance()
	if !s.JustReleased(KeyA) {
		t.Fatal("JustReleased(A) should be true this tick")
	}
}

// Mouse buttons live in the same Key space and behave like any key.
func TestStateMouseButtonUnified(t *testing.T) {
	s := newState()
	s.apply(KeyChange(KeyMouseLeft, 0, true))
	s.advance()
	if !s.Pressed(KeyMouseLeft) || !s.JustPressed(KeyMouseLeft) {
		t.Fatal("mouse button should behave like a key")
	}
}

// Scroll and text accumulate per tick and are visible only after advance, then clear.
func TestStateScrollAndText(t *testing.T) {
	s := newState()
	s.apply(ScrollChange(1, 2))
	s.apply(ScrollChange(0, 3))
	s.apply(TextChange('h'))
	s.apply(TextChange('i'))

	if dx, dy := s.Scroll(); dx != 0 || dy != 0 {
		t.Fatal("scroll should not be visible before advance")
	}
	s.advance()
	if dx, dy := s.Scroll(); dx != 1 || dy != 5 {
		t.Fatalf("scroll = %v,%v want 1,5", dx, dy)
	}
	if string(s.Text()) != "hi" {
		t.Fatalf("text = %q want %q", string(s.Text()), "hi")
	}
	s.advance()
	if dx, dy := s.Scroll(); dx != 0 || dy != 0 {
		t.Fatal("scroll should clear next tick")
	}
	if len(s.Text()) != 0 {
		t.Fatal("text should clear next tick")
	}
}

// Pointer is live: it updates immediately, not per tick.
func TestStatePointerLive(t *testing.T) {
	s := newState()
	s.apply(PointerChange(Pos{X: 3, Y: 4}))
	if s.Pointer() != (Pos{X: 3, Y: 4}) {
		t.Fatalf("pointer = %+v want {3 4}", s.Pointer())
	}
}

// A press+release within one tick gap surfaces both edges on the next advance.
func TestStateTransientPressAndRelease(t *testing.T) {
	s := newState()
	s.apply(KeyChange(KeySpace, 0, true))
	s.apply(KeyChange(KeySpace, 0, false))
	s.advance()
	if !s.JustPressed(KeySpace) || !s.JustReleased(KeySpace) {
		t.Fatal("transient press+release should report both edges")
	}
	if s.Pressed(KeySpace) {
		t.Fatal("key should not be held after release")
	}
}
