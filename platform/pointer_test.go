package platform_test

import (
	"slices"
	"testing"

	"github.com/dvoyni/cog/platform"
)

// pointerChange is a pointer change of kind in window, at position.
func pointerChange(kind platform.InputChangeKind, window platform.Window, position platform.Point) platform.InputChange {
	return platform.InputChange{Kind: kind, Window: window, Position: position}
}

// expectPointer checks the input state's pointer in window against position
// and inside.
func expectPointer(t *testing.T, h *harness, window platform.Window, position platform.Point, inside bool) {
	t.Helper()
	if got := h.input.PointerPosition(window); got != position {
		t.Errorf("the pointer of %v is at %v, want %v", window, got, position)
	}
	if got := h.input.IsPointerInside(window); got != inside {
		t.Errorf("the pointer inside %v is %v, want %v", window, got, inside)
	}
}

func TestPointerMovesCoalesceIntoOneEventPerWindow(t *testing.T) {
	h := spawn(t, newFake())
	first := openFocused(t, h)
	second := h.open(platform.WindowEntry{Title: "second"})
	h.pump()
	h.takePublished()

	h.fake.inputChanges = append(h.fake.inputChanges,
		pointerChange(platform.InputChangePointerEnter, first, platform.Point{X: 10, Y: 10}),
		pointerChange(platform.InputChangePointerMove, first, platform.Point{X: 12, Y: 10}),
		platform.InputChange{Kind: platform.InputChangeScroll, Window: first, ScrollDelta: platform.Point{Y: -2}, ScrollLines: true},
		pointerChange(platform.InputChangePointerMove, first, platform.Point{X: 15, Y: 14}),
		pointerChange(platform.InputChangePointerLeave, first, platform.Point{}),
		pointerChange(platform.InputChangePointerEnter, second, platform.Point{Y: 5}),
		pointerChange(platform.InputChangePointerMove, second, platform.Point{X: 1.5, Y: 6}),
		pointerChange(platform.InputChangePointerMove, second, platform.Point{X: 3, Y: 9}),
	)
	h.pump()
	want := []any{
		platform.PointerEnterEvent{Window: first},
		platform.ScrollEvent{Window: first, Delta: platform.Point{Y: -2}, Lines: true},
		platform.PointerMoveEvent{Window: first, Position: platform.Point{X: 15, Y: 14}, Delta: platform.Point{X: 5, Y: 4}},
		platform.PointerLeaveEvent{Window: first},
		platform.PointerEnterEvent{Window: second},
		platform.PointerMoveEvent{Window: second, Position: platform.Point{X: 3, Y: 9}, Delta: platform.Point{X: 3, Y: 4}},
	}
	if got := h.takePublished(); !slices.Equal(got, want) {
		t.Fatalf("the pump published\n%v\nwant\n%v", got, want)
	}
	expectPointer(t, h, first, platform.Point{X: 15, Y: 14}, false)
	expectPointer(t, h, second, platform.Point{X: 3, Y: 9}, true)

	press := keyChange(second, platform.KeyMouseLeft, true, false, 0)
	press.Position = platform.Point{X: 3, Y: 9}
	h.fake.inputChanges = append(h.fake.inputChanges,
		press,
		pointerChange(platform.InputChangePointerMove, second, platform.Point{X: -4, Y: 9.5}),
		platform.InputChange{Kind: platform.InputChangeScroll, Window: second, ScrollDelta: platform.Point{X: 0.5, Y: 12.5}},
	)
	h.pump()
	want = []any{
		platform.KeyEvent{Window: second, Key: platform.KeyMouseLeft, Pressed: true, Position: platform.Point{X: 3, Y: 9}},
		platform.PointerMoveEvent{Window: second, Position: platform.Point{X: -4, Y: 9.5}, Delta: platform.Point{X: -7, Y: 0.5}},
		platform.ScrollEvent{Window: second, Delta: platform.Point{X: 0.5, Y: 12.5}},
	}
	if got := h.takePublished(); !slices.Equal(got, want) {
		t.Fatalf("the drag published\n%v\nwant\n%v", got, want)
	}
	expectPointer(t, h, second, platform.Point{X: -4, Y: 9.5}, true)
}

func TestTheFirstMoveInAWindowHasNoMotion(t *testing.T) {
	h := spawn(t, newFake())
	window := openFocused(t, h)

	h.fake.inputChanges = append(h.fake.inputChanges, pointerChange(platform.InputChangePointerMove, window, platform.Point{X: 7, Y: 8}))
	h.pump()
	want := []any{platform.PointerMoveEvent{Window: window, Position: platform.Point{X: 7, Y: 8}}}
	if got := h.takePublished(); !slices.Equal(got, want) {
		t.Fatalf("the pump published %v, want %v", got, want)
	}
	expectPointer(t, h, window, platform.Point{X: 7, Y: 8}, false)
}

func TestAResizeReachesTheWindowStateInWindowSpace(t *testing.T) {
	h := spawn(t, newFake())
	window := openFocused(t, h)
	if got := h.manager.Scale(window); got != 2 {
		t.Fatalf("the fake opened a window at scale %v, want 2", got)
	}

	resize := platform.InputChange{Kind: platform.InputChangeResize, Window: window, Size: platform.Size{Width: 300, Height: 200}, FramebufferSize: platform.Size{Width: 900, Height: 600}, Scale: 3}
	h.fake.windowChanges = append(h.fake.windowChanges, resize)
	h.fake.inputChanges = append(h.fake.inputChanges, pointerChange(platform.InputChangePointerEnter, window, platform.Point{X: 299.5, Y: 199.5}))
	h.pump()
	want := []any{
		platform.ResizeEvent{Window: window, Size: platform.Size{Width: 300, Height: 200}, FramebufferSize: platform.Size{Width: 900, Height: 600}, Scale: 3},
		platform.PointerEnterEvent{Window: window},
	}
	if got := h.takePublished(); !slices.Equal(got, want) {
		t.Fatalf("the pump published\n%v\nwant\n%v", got, want)
	}
	if got := h.manager.Size(window); got != (platform.Size{Width: 300, Height: 200}) {
		t.Errorf("the window state's size is %v", got)
	}
	if got := h.manager.FramebufferSize(window); got != (platform.Size{Width: 900, Height: 600}) {
		t.Errorf("the window state's framebuffer size is %v", got)
	}
	if got := h.manager.Scale(window); got != 3 {
		t.Errorf("the window state's scale is %v", got)
	}
	expectPointer(t, h, window, platform.Point{X: 299.5, Y: 199.5}, true)
}

func TestLosingFocusReleasesMouseButtonsAtThePointer(t *testing.T) {
	h := spawn(t, newFake())
	window := openFocused(t, h)

	press := keyChange(window, platform.KeyMouseLeft, true, false, 0)
	press.Position = platform.Point{X: 4, Y: 5}
	h.fake.inputChanges = append(h.fake.inputChanges,
		pointerChange(platform.InputChangePointerEnter, window, platform.Point{X: 4, Y: 5}),
		press,
		pointerChange(platform.InputChangePointerMove, window, platform.Point{X: 8, Y: 6}),
		keyChange(window, platform.KeyA, true, false, 0),
		platform.InputChange{Kind: platform.InputChangeFocus, Window: window},
	)
	h.pump()
	want := []any{
		platform.PointerEnterEvent{Window: window},
		platform.KeyEvent{Window: window, Key: platform.KeyMouseLeft, Pressed: true, Position: platform.Point{X: 4, Y: 5}},
		platform.PointerMoveEvent{Window: window, Position: platform.Point{X: 8, Y: 6}, Delta: platform.Point{X: 4, Y: 1}},
		platform.KeyEvent{Window: window, Key: platform.KeyA, Pressed: true},
		platform.KeyEvent{Window: window, Key: platform.KeyA},
		platform.KeyEvent{Window: window, Key: platform.KeyMouseLeft, Position: platform.Point{X: 8, Y: 6}},
		platform.FocusEvent{Window: window},
	}
	if got := h.takePublished(); !slices.Equal(got, want) {
		t.Fatalf("the pump published\n%v\nwant\n%v", got, want)
	}
}

func TestAClosedWindowForgetsItsPointer(t *testing.T) {
	h := spawn(t, newFake())
	closedByTheApplication := openFocused(t, h)
	closedByItself := h.open(platform.WindowEntry{Title: "second"})
	h.pump()
	h.fake.inputChanges = append(h.fake.inputChanges,
		pointerChange(platform.InputChangePointerEnter, closedByTheApplication, platform.Point{X: 1, Y: 2}),
		pointerChange(platform.InputChangePointerLeave, closedByTheApplication, platform.Point{}),
		pointerChange(platform.InputChangePointerEnter, closedByItself, platform.Point{X: 3, Y: 4}),
	)
	h.pump()
	expectPointer(t, h, closedByTheApplication, platform.Point{X: 1, Y: 2}, false)
	expectPointer(t, h, closedByItself, platform.Point{X: 3, Y: 4}, true)

	h.change(func(manager *platform.WindowManager) { manager.Close(closedByTheApplication) })
	h.fake.windowChanges = append(h.fake.windowChanges, platform.InputChange{Kind: platform.InputChangeWindowClosed, Window: closedByItself})
	h.pump()
	expectPointer(t, h, closedByTheApplication, platform.Point{}, false)
	expectPointer(t, h, closedByItself, platform.Point{}, false)
}

func TestTheZeroWindowReadsTheOnlyWindow(t *testing.T) {
	h := spawn(t, newFake())
	window := openFocused(t, h)
	h.fake.inputChanges = append(h.fake.inputChanges, pointerChange(platform.InputChangePointerEnter, window, platform.Point{X: 6, Y: 7}))
	h.pump()

	expectPointer(t, h, platform.Window{}, platform.Point{X: 6, Y: 7}, true)
	if got := h.manager.Scale(platform.Window{}); got != 2 {
		t.Errorf("the zero window's scale is %v, want the only window's, 2", got)
	}
	if !h.manager.IsFocused(platform.Window{}) || !h.manager.IsOpen(platform.Window{}) {
		t.Error("the zero window does not read the only window, open and focused")
	}
}

func TestTheZeroWindowPanicsUnlessOneWindowIsKnown(t *testing.T) {
	h := spawn(t, newFake())
	expectZeroWindowPanics(t, h, "no window")

	openFocused(t, h)
	h.open(platform.WindowEntry{Title: "second"})
	h.pump()
	expectZeroWindowPanics(t, h, "two windows")
}

// expectZeroWindowPanics checks that every getter given the zero Window
// panics, with the windows known as known says.
func expectZeroWindowPanics(t *testing.T, h *harness, known string) {
	t.Helper()
	getters := map[string]func(){
		"PointerPosition": func() { h.input.PointerPosition(platform.Window{}) },
		"IsPointerInside": func() { h.input.IsPointerInside(platform.Window{}) },
		"IsOpen":          func() { h.manager.IsOpen(platform.Window{}) },
		"Size":            func() { h.manager.Size(platform.Window{}) },
		"FramebufferSize": func() { h.manager.FramebufferSize(platform.Window{}) },
		"Scale":           func() { h.manager.Scale(platform.Window{}) },
		"Native":          func() { h.manager.Native(platform.Window{}) },
		"IsFocused":       func() { h.manager.IsFocused(platform.Window{}) },
	}
	for name, getter := range getters {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s of the zero window with %s did not panic", name, known)
				}
			}()
			getter()
		}()
	}
}
