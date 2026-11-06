package headless_test

import (
	"slices"
	"testing"

	"github.com/dvoyni/cog/platform"
)

func TestAWindowOpensFromTheNextPumpWithAZeroNativeHandle(t *testing.T) {
	h := spawn(t)
	var window platform.Window
	var openBefore bool
	h.change(func(manager *platform.WindowManager) {
		window = manager.Open(platform.WindowEntry{Title: "main", Size: platform.Size{Width: 640, Height: 480}})
		openBefore = manager.IsOpen(window)
	})
	if openBefore {
		t.Fatal("the window is open before a pump")
	}
	h.pump()
	if published := h.takePublished(); !slices.Equal(published, []any{platform.WindowOpenedEvent{Window: window}}) {
		t.Fatalf("the pump published %v, want the window opened", published)
	}
	h.change(func(manager *platform.WindowManager) {
		if !manager.IsOpen(window) {
			t.Error("the window is not open after the pump")
		}
		size := platform.Size{Width: 640, Height: 480}
		if got := manager.Size(window); got != size {
			t.Errorf("Size = %v, want %v", got, size)
		}
		if got := manager.FramebufferSize(window); got != size {
			t.Errorf("FramebufferSize = %v, want %v", got, size)
		}
		if got := manager.Scale(window); got != 1 {
			t.Errorf("Scale = %v, want 1", got)
		}
		if got := manager.Native(window); got != (platform.NativeHandle{}) {
			t.Errorf("Native = %v, want the zero handle", got)
		}
	})
}

func TestAClosedWindowIsGoneFromTheNextPump(t *testing.T) {
	h := spawn(t)
	var first, second platform.Window
	h.change(func(manager *platform.WindowManager) {
		first = manager.Open(platform.WindowEntry{Title: "first"})
		second = manager.Open(platform.WindowEntry{Title: "second"})
	})
	h.pump()
	h.takePublished()
	h.change(func(manager *platform.WindowManager) { manager.Close(first) })
	h.pump()
	if published := h.takePublished(); !slices.Equal(published, []any{platform.WindowClosedEvent{Window: first}}) {
		t.Fatalf("the pump published %v, want the first window closed", published)
	}
	h.change(func(manager *platform.WindowManager) {
		if manager.IsOpen(first) {
			t.Error("the closed window is still open")
		}
		if !manager.IsOpen(second) {
			t.Error("the other window closed with it")
		}
	})
	h.pump()
	if published := h.takePublished(); len(published) != 0 {
		t.Errorf("a later pump published %v, want nothing", published)
	}
}
