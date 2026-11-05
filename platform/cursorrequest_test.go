package platform_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/dvoyni/cog/platform"
)

// expectCalls checks the calls the fake recorded since the last check against
// want, and forgets them.
func expectCalls(t *testing.T, h *harness, want ...string) {
	t.Helper()
	if got := h.fake.calls; !slices.Equal(got, want) {
		t.Errorf("the fake was called %q, want %q", got, want)
	}
	h.fake.calls = nil
}

// expectEffectiveCursorMode checks window's effective cursor mode in the
// window state against mode.
func expectEffectiveCursorMode(t *testing.T, h *harness, window platform.Window, mode platform.CursorMode) {
	t.Helper()
	if got := h.manager.EffectiveCursorMode(window); got != mode {
		t.Errorf("the effective cursor mode of %v is %v, want %v", window, got, mode)
	}
}

// setCursorMode requests mode for window's cursor and pumps.
func setCursorMode(t *testing.T, h *harness, window platform.Window, mode platform.CursorMode) {
	t.Helper()
	h.change(func(manager *platform.WindowManager) { manager.SetCursorMode(window, mode) })
	h.pump()
}

func TestTheCursorRequestAppliesOnlyWhatChanged(t *testing.T) {
	h := spawn(t, newFake())
	var first, second platform.Window
	h.change(func(manager *platform.WindowManager) {
		first = manager.Open(platform.WindowEntry{Title: "first"})
		manager.SetCursorShape(first, platform.CursorShapeText)
		second = manager.Open(platform.WindowEntry{Title: "second"})
	})
	h.pump()
	expectCalls(t, h, "open first", fmt.Sprintf("cursor %v", platform.CursorRequest{Shape: platform.CursorShapeText}), "open second")

	hiddenPointer := platform.CursorRequest{Shape: platform.CursorShapePointer, Hidden: true, Mode: platform.CursorModeConfined}
	h.change(func(manager *platform.WindowManager) {
		manager.SetCursorShape(first, platform.CursorShapeCrosshair)
		manager.SetCursorShape(first, platform.CursorShapePointer)
		manager.SetCursorHidden(first, true)
		manager.SetCursorMode(first, platform.CursorModeConfined)
		manager.SetTitle(second, "renamed")
	})
	h.pump()
	expectCalls(t, h,
		fmt.Sprintf("cursor %v", hiddenPointer),
		fmt.Sprintf("change %v to %v", platform.WindowEntry{Title: "second"}, platform.WindowEntry{Title: "renamed"}),
	)

	h.change(func(manager *platform.WindowManager) {
		manager.SetCursorShape(first, platform.CursorShapePointer)
		manager.SetCursorHidden(first, true)
		manager.SetCursorMode(first, platform.CursorModeConfined)
		manager.SetTitle(first, "renamed")
	})
	h.pump()
	expectCalls(t, h, fmt.Sprintf("change %v to %v", platform.WindowEntry{Title: "first"}, platform.WindowEntry{Title: "renamed"}))

	h.change(func(manager *platform.WindowManager) { manager.Close(first) })
	var answers []bool
	h.change(func(manager *platform.WindowManager) {
		answers = append(answers,
			manager.SetCursorShape(first, platform.CursorShapeText),
			manager.SetCursorHidden(first, false),
			manager.SetCursorMode(first, platform.CursorModeLocked),
			manager.SetCursorPosition(first, platform.Point{X: 1, Y: 1}),
		)
	})
	if slices.Contains(answers, true) {
		t.Errorf("the cursor setters on a closed window answered %v, want all false", answers)
	}
}

func TestEveryCursorShapeAndModeIsDistinct(t *testing.T) {
	shapes := []platform.CursorShape{
		platform.CursorShapeDefault,
		platform.CursorShapePointer,
		platform.CursorShapeText,
		platform.CursorShapeCrosshair,
		platform.CursorShapeMove,
		platform.CursorShapeResizeNorthSouth,
		platform.CursorShapeResizeEastWest,
		platform.CursorShapeResizeNorthwestSoutheast,
		platform.CursorShapeResizeNortheastSouthwest,
		platform.CursorShapeNotAllowed,
		platform.CursorShapeWait,
	}
	modes := []platform.CursorMode{platform.CursorModeNormal, platform.CursorModeConfined, platform.CursorModeLocked}
	if shapes[0] != 0 || modes[0] != 0 {
		t.Error("the zero cursor request is not the default shape in the normal mode")
	}
	if len(slices.Compact(slices.Sorted(slices.Values(shapes)))) != len(shapes) {
		t.Errorf("two cursor shapes share a value: %v", shapes)
	}
	if len(slices.Compact(slices.Sorted(slices.Values(modes)))) != len(modes) {
		t.Errorf("two cursor modes share a value: %v", modes)
	}
}

func TestALockedPointerStaysAndItsDeltaCarriesTheMotion(t *testing.T) {
	h := spawn(t, newFake())
	window := openFocused(t, h)
	h.fake.inputChanges = append(h.fake.inputChanges, pointerChange(platform.InputChangePointerEnter, window, platform.Point{X: 10, Y: 10}))
	h.pump()
	h.takePublished()
	expectEffectiveCursorMode(t, h, window, platform.CursorModeNormal)

	setCursorMode(t, h, window, platform.CursorModeLocked)
	expectEffectiveCursorMode(t, h, window, platform.CursorModeLocked)
	h.fake.inputChanges = append(h.fake.inputChanges,
		platform.InputChange{Kind: platform.InputChangePointerMove, Window: window, Position: platform.Point{X: 300, Y: 300}, PointerDelta: platform.Point{X: 3, Y: 1}},
		platform.InputChange{Kind: platform.InputChangePointerMove, Window: window, Position: platform.Point{X: 301, Y: 300}, PointerDelta: platform.Point{X: 2, Y: -4}},
	)
	h.pump()
	want := []any{platform.PointerMoveEvent{Window: window, Position: platform.Point{X: 10, Y: 10}, Delta: platform.Point{X: 5, Y: -3}}}
	if got := h.takePublished(); !slices.Equal(got, want) {
		t.Fatalf("moving while locked published %v, want %v", got, want)
	}
	expectPointer(t, h, window, platform.Point{X: 10, Y: 10}, true)

	setCursorMode(t, h, window, platform.CursorModeNormal)
	h.fake.inputChanges = append(h.fake.inputChanges,
		platform.InputChange{Kind: platform.InputChangePointerMove, Window: window, Position: platform.Point{X: 12, Y: 10}, PointerDelta: platform.Point{X: 40, Y: 40}},
	)
	h.pump()
	want = []any{platform.PointerMoveEvent{Window: window, Position: platform.Point{X: 12, Y: 10}, Delta: platform.Point{X: 2}}}
	if got := h.takePublished(); !slices.Equal(got, want) {
		t.Fatalf("moving once unlocked published %v, want %v", got, want)
	}
}

func TestFocusReturningReappliesTheRequestedMode(t *testing.T) {
	h := spawn(t, newFake())
	window := openFocused(t, h)
	h.fake.calls = nil
	locked := platform.CursorRequest{Mode: platform.CursorModeLocked}
	setCursorMode(t, h, window, platform.CursorModeLocked)
	expectCalls(t, h, fmt.Sprintf("cursor %v", locked))

	h.fake.cursorModes[window] = platform.CursorModeNormal
	h.fake.inputChanges = append(h.fake.inputChanges, platform.InputChange{Kind: platform.InputChangeFocus, Window: window})
	h.pump()
	expectCalls(t, h)
	expectEffectiveCursorMode(t, h, window, platform.CursorModeNormal)

	h.fake.inputChanges = append(h.fake.inputChanges, platform.InputChange{Kind: platform.InputChangeFocus, Window: window, Focused: true})
	h.pump()
	expectCalls(t, h, fmt.Sprintf("cursor %v", locked))
	expectEffectiveCursorMode(t, h, window, platform.CursorModeLocked)

	h.fake.inputChanges = append(h.fake.inputChanges, platform.InputChange{Kind: platform.InputChangeFocus, Window: window, Focused: true})
	h.pump()
	expectCalls(t, h)
}

func TestAPlatformLackingTheModeLeavesItNotInForce(t *testing.T) {
	f := newFake()
	f.lackedCursorMode = platform.CursorModeConfined
	h := spawn(t, f)
	window := openFocused(t, h)
	h.fake.calls = nil
	confined := platform.CursorRequest{Mode: platform.CursorModeConfined}
	setCursorMode(t, h, window, platform.CursorModeConfined)
	expectCalls(t, h, fmt.Sprintf("cursor %v", confined))
	expectEffectiveCursorMode(t, h, window, platform.CursorModeNormal)

	h.fake.inputChanges = append(h.fake.inputChanges,
		platform.InputChange{Kind: platform.InputChangeFocus, Window: window},
		platform.InputChange{Kind: platform.InputChangeFocus, Window: window, Focused: true},
	)
	h.pump()
	expectCalls(t, h, fmt.Sprintf("cursor %v", confined))
	expectEffectiveCursorMode(t, h, window, platform.CursorModeNormal)
}

func TestTheCursorPositionMovesThePointerWithoutAMove(t *testing.T) {
	h := spawn(t, newFake())
	window := openFocused(t, h)
	h.fake.inputChanges = append(h.fake.inputChanges, pointerChange(platform.InputChangePointerEnter, window, platform.Point{X: 1, Y: 2}))
	h.pump()
	h.takePublished()
	h.fake.calls = nil

	h.change(func(manager *platform.WindowManager) {
		manager.SetCursorPosition(window, platform.Point{X: 5, Y: 5})
		manager.SetCursorPosition(window, platform.Point{X: 7, Y: 8})
	})
	h.pump()
	expectCalls(t, h, fmt.Sprintf("cursor position %v", platform.Point{X: 7, Y: 8}))
	if got := h.takePublished(); len(got) != 0 {
		t.Errorf("setting the cursor position published %v, want nothing", got)
	}
	expectPointer(t, h, window, platform.Point{X: 7, Y: 8}, true)

	h.fake.inputChanges = append(h.fake.inputChanges, pointerChange(platform.InputChangePointerMove, window, platform.Point{X: 10, Y: 8}))
	h.pump()
	expectCalls(t, h)
	want := []any{platform.PointerMoveEvent{Window: window, Position: platform.Point{X: 10, Y: 8}, Delta: platform.Point{X: 3}}}
	if got := h.takePublished(); !slices.Equal(got, want) {
		t.Fatalf("the move after the cursor position published %v, want %v", got, want)
	}

	locked := platform.CursorRequest{Mode: platform.CursorModeLocked}
	h.change(func(manager *platform.WindowManager) {
		manager.SetCursorPosition(window, platform.Point{X: 1, Y: 1})
		manager.SetCursorMode(window, platform.CursorModeLocked)
	})
	h.pump()
	setCursorMode(t, h, window, platform.CursorModeNormal)
	expectCalls(t, h, fmt.Sprintf("cursor %v", locked), fmt.Sprintf("cursor %v", platform.CursorRequest{}))
	expectPointer(t, h, window, platform.Point{X: 10, Y: 8}, true)
}

func TestACursorPositionTheWindowsCannotSetIsLogged(t *testing.T) {
	f := newFake()
	f.cursorPositionError = errors.New("unsupported")
	h := spawn(t, f)
	window := openFocused(t, h)
	h.change(func(manager *platform.WindowManager) { manager.SetCursorPosition(window, platform.Point{X: 4, Y: 4}) })
	h.pump()
	h.mutex.Lock()
	logged := slices.Clone(h.errors)
	h.mutex.Unlock()
	if len(logged) != 1 || !strings.Contains(logged[0].Error(), "unsupported") {
		t.Fatalf("the pump logged %v, want the cursor position's error", logged)
	}
	expectPointer(t, h, window, platform.Point{}, false)
}
