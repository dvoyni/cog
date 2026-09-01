package wgpu

import (
	"reflect"
	"testing"

	"github.com/dvoyni/cog/input"
	"github.com/gogpu/gpucontext"
)

// input.Key mirrors gpucontext.Key, so mapKey is a cast. These boundary cases
// (including the last key, KeyPause) catch any order/length drift between the two
// enums.
func TestMapKeyAlignment(t *testing.T) {
	cases := map[gpucontext.Key]input.Key{
		gpucontext.KeyUnknown:     input.KeyUnknown,
		gpucontext.KeyA:           input.KeyA,
		gpucontext.KeyZ:           input.KeyZ,
		gpucontext.Key0:           input.Key0,
		gpucontext.Key9:           input.Key9,
		gpucontext.KeyF1:          input.KeyF1,
		gpucontext.KeyF6:          input.KeyF6,
		gpucontext.KeyF7:          input.KeyF7,
		gpucontext.KeyF8:          input.KeyF8,
		gpucontext.KeyF12:         input.KeyF12,
		gpucontext.KeyEscape:      input.KeyEscape,
		gpucontext.KeySpace:       input.KeySpace,
		gpucontext.KeyInsert:      input.KeyInsert,
		gpucontext.KeyPageDown:    input.KeyPageDown,
		gpucontext.KeyDown:        input.KeyDown,
		gpucontext.KeyLeftControl: input.KeyLeftControl,
		gpucontext.KeyRightSuper:  input.KeyRightSuper,
		gpucontext.KeyMinus:       input.KeyMinus,
		gpucontext.KeySlash:       input.KeySlash,
		gpucontext.KeyNumpad0:     input.KeyNumpad0,
		gpucontext.KeyNumpadEnter: input.KeyNumpadEnter,
		gpucontext.KeyCapsLock:    input.KeyCapsLock,
		gpucontext.KeyPause:       input.KeyPause,
	}
	for gk, want := range cases {
		if got := mapKey(gk); got != want {
			t.Errorf("mapKey(%v) = %d, want %d (input.Key must mirror gpucontext.Key order)", gk, got, want)
		}
	}
}

func TestMapMouseButton(t *testing.T) {
	cases := map[gpucontext.MouseButton]input.Key{
		gpucontext.MouseButtonLeft:   input.KeyMouseLeft,
		gpucontext.MouseButtonRight:  input.KeyMouseRight,
		gpucontext.MouseButtonMiddle: input.KeyMouseMiddle,
		gpucontext.MouseButton4:      input.KeyMouse4,
		gpucontext.MouseButton5:      input.KeyMouse5,
	}
	for b, want := range cases {
		if got := mapMouseButton(b); got != want {
			t.Errorf("mapMouseButton(%v) = %d, want %d", b, got, want)
		}
	}
	// Mouse keys are negative so they never collide with keyboard keys (>= 0).
	if input.KeyMouseLeft >= 0 {
		t.Errorf("mouse keys must be negative, got %d", input.KeyMouseLeft)
	}
}

func TestMapPointerButton(t *testing.T) {
	cases := map[gpucontext.Button]input.Key{
		gpucontext.ButtonLeft:   input.KeyMouseLeft,
		gpucontext.ButtonRight:  input.KeyMouseRight,
		gpucontext.ButtonMiddle: input.KeyMouseMiddle,
		gpucontext.ButtonX1:     input.KeyMouse4,
		gpucontext.ButtonX2:     input.KeyMouse5,
	}
	for button, want := range cases {
		got, ok := mapPointerButton(gpucontext.PointerTypeMouse, button)
		if !ok || got != want {
			t.Errorf("mapPointerButton(Mouse, %v) = (%d, %v), want (%d, true)", button, got, ok, want)
		}
	}
	if _, ok := mapPointerButton(gpucontext.PointerTypeMouse, gpucontext.ButtonNone); ok {
		t.Error("ButtonNone should not map to a mouse key")
	}
	if got, ok := mapPointerButton(gpucontext.PointerTypeTouch, gpucontext.ButtonNone); !ok || got != input.KeyMouseLeft {
		t.Errorf("touch = (%d, %v), want (%d, true)", got, ok, input.KeyMouseLeft)
	}
}

func TestPointerChangesBrowserTouch(t *testing.T) {
	position := input.Pos{X: 12, Y: 34}
	event := gpucontext.PointerEvent{
		Type:        gpucontext.PointerDown,
		X:           position.X,
		Y:           position.Y,
		PointerType: gpucontext.PointerTypeTouch,
	}
	want := []input.Change{
		input.PointerChange(position),
		input.KeyChange(input.KeyMouseLeft, 0, true),
	}
	if got := pointerChanges(event); !reflect.DeepEqual(got, want) {
		t.Errorf("pointerChanges(touch down) = %#v, want %#v", got, want)
	}

	event.Type = gpucontext.PointerUp
	want[1] = input.KeyChange(input.KeyMouseLeft, 0, false)
	if got := pointerChanges(event); !reflect.DeepEqual(got, want) {
		t.Errorf("pointerChanges(touch up) = %#v, want %#v", got, want)
	}
}

func TestPointerChangesIgnoresSecondaryTouch(t *testing.T) {
	event := gpucontext.PointerEvent{
		Type:        gpucontext.PointerDown,
		PointerID:   2,
		PointerType: gpucontext.PointerTypeTouch,
		IsPrimary:   false,
	}
	if got := pointerChanges(event); len(got) != 0 {
		t.Errorf("pointerChanges(secondary touch) = %#v, want no changes", got)
	}
}

func TestMapMods(t *testing.T) {
	m := mapMods(gpucontext.ModShift | gpucontext.ModControl)
	if !m.Has(input.ModShift) || !m.Has(input.ModCtrl) {
		t.Errorf("mods = %b, want shift+ctrl", m)
	}
	if m.Has(input.ModAlt) || m.Has(input.ModSuper) {
		t.Error("alt/super should not be set")
	}
	// Bit positions must line up for the cast to be correct.
	if input.Mods(gpucontext.ModCapsLock) != input.ModCapsLock {
		t.Error("ModCapsLock bit position misaligned with gpucontext")
	}
}
