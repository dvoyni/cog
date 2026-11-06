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

func TestAResizeFromTheTestIsPublishedFromTheNextPump(t *testing.T) {
	h := spawn(t)
	var window platform.Window
	h.change(func(manager *platform.WindowManager) {
		window = manager.Open(platform.WindowEntry{Size: platform.Size{Width: 640, Height: 480}})
	})
	h.pump()
	h.takePublished()
	size := platform.Size{Width: 800, Height: 600}
	framebufferSize := platform.Size{Width: 1600, Height: 1200}
	h.windows.Resize(window, size, framebufferSize, 2)
	h.pump()
	want := []any{platform.ResizeEvent{Window: window, Size: size, FramebufferSize: framebufferSize, Scale: 2}}
	if published := h.takePublished(); !slices.Equal(published, want) {
		t.Fatalf("the pump published %v, want %v", published, want)
	}
	h.change(func(manager *platform.WindowManager) {
		if got := manager.Size(window); got != size {
			t.Errorf("Size = %v, want %v", got, size)
		}
		if got := manager.FramebufferSize(window); got != framebufferSize {
			t.Errorf("FramebufferSize = %v, want %v", got, framebufferSize)
		}
		if got := manager.Scale(window); got != 2 {
			t.Errorf("Scale = %v, want 2", got)
		}
	})
}

func TestAResizeFromAnotherGoroutineReachesTheWindow(t *testing.T) {
	h := spawn(t)
	var window platform.Window
	h.change(func(manager *platform.WindowManager) { window = manager.Open(platform.WindowEntry{}) })
	h.pump()
	done := make(chan struct{})
	go func() {
		for side := 1; side <= 50; side++ {
			size := platform.Size{Width: side, Height: side}
			h.windows.Resize(window, size, size, 1)
		}
		close(done)
	}()
	for running := true; running; {
		select {
		case <-done:
			running = false
		default:
		}
		h.pump()
	}
	h.change(func(manager *platform.WindowManager) {
		if got, want := manager.Size(window), (platform.Size{Width: 50, Height: 50}); got != want {
			t.Errorf("Size = %v, want the last resize, %v", got, want)
		}
	})
}

func TestAResizeOfAWindowClosedBeforeThePumpIsDropped(t *testing.T) {
	h := spawn(t)
	var window platform.Window
	h.change(func(manager *platform.WindowManager) { window = manager.Open(platform.WindowEntry{}) })
	h.pump()
	h.takePublished()
	size := platform.Size{Width: 800, Height: 600}
	h.windows.Resize(window, size, size, 1)
	h.change(func(manager *platform.WindowManager) { manager.Close(window) })
	h.pump()
	if published := h.takePublished(); !slices.Equal(published, []any{platform.WindowClosedEvent{Window: window}}) {
		t.Fatalf("the pump published %v, want the window closed alone", published)
	}
	h.windows.Resize(window, size, size, 1)
	h.pump()
	if published := h.takePublished(); len(published) != 0 {
		t.Errorf("a resize of the closed window published %v, want nothing", published)
	}
}

func TestHidingThroughTheRequestIsAppliedByThePump(t *testing.T) {
	h := spawn(t)
	var window platform.Window
	h.change(func(manager *platform.WindowManager) { window = manager.Open(platform.WindowEntry{}) })
	h.pump()
	h.change(func(manager *platform.WindowManager) { manager.SetHidden(window, true) })
	if h.windows.IsHidden(window) {
		t.Fatal("the window is hidden before a pump")
	}
	h.pump()
	if !h.windows.IsHidden(window) {
		t.Fatal("the window is not hidden after the pump")
	}
	h.change(func(manager *platform.WindowManager) { manager.SetHidden(window, false) })
	h.pump()
	if h.windows.IsHidden(window) {
		t.Error("the window is still hidden after it was shown")
	}
}

func TestAWindowOpenedHiddenIsHidden(t *testing.T) {
	h := spawn(t)
	var window platform.Window
	h.change(func(manager *platform.WindowManager) { window = manager.Open(platform.WindowEntry{Hidden: true}) })
	h.pump()
	if !h.windows.IsHidden(window) {
		t.Error("the window opened hidden is not hidden")
	}
}
