package internal

import (
	"testing"
)

func TestViewportFixedWidth(t *testing.T) {
	k := newTestKernel(t, newPlugin())
	k.ExecuteCommand[SetDesiredViewportCmd](
		SetDesiredViewportRequest{Mode: ViewportFixedWidth, Size: 1280})

	response := k.ExecuteCommand[SetViewportCmd](SetViewportRequest{Width: 1920, Height: 1080})
	want := Viewport{Width: 1280, Height: 720, WindowWidth: 1920, WindowHeight: 1080}
	if response.Viewport != want {
		t.Errorf("viewport = %#v, want %#v", response.Viewport, want)
	}

	response = k.ExecuteCommand[SetViewportCmd](SetViewportRequest{Width: 500, Height: 1000})
	want = Viewport{Width: 1280, Height: 2560, WindowWidth: 500, WindowHeight: 1000}
	if response.Viewport != want {
		t.Errorf("portrait viewport = %#v, want %#v", response.Viewport, want)
	}
}

func TestViewportCarriesFramebufferSizeAcrossPreferenceChanges(t *testing.T) {
	k := newTestKernel(t, newPlugin())
	response := k.ExecuteCommand[SetViewportCmd](SetViewportRequest{
		Width: 1280, Height: 720, FramebufferWidth: 2560, FramebufferHeight: 1440,
	})
	if response.Viewport.FramebufferWidth != 2560 || response.Viewport.FramebufferHeight != 1440 {
		t.Fatalf("framebuffer = %vx%v, want 2560x1440", response.Viewport.FramebufferWidth, response.Viewport.FramebufferHeight)
	}
	desiredResponse := k.ExecuteCommand[SetDesiredViewportCmd](SetDesiredViewportRequest{
		Mode: ViewportFit, Width: 1280, Height: 720,
	})
	if desiredResponse.Viewport.FramebufferWidth != 2560 || desiredResponse.Viewport.FramebufferHeight != 1440 {
		t.Fatalf("framebuffer after preference = %vx%v, want 2560x1440", desiredResponse.Viewport.FramebufferWidth, desiredResponse.Viewport.FramebufferHeight)
	}
}

func TestViewportFixedHeight(t *testing.T) {
	k := newTestKernel(t, newPlugin())
	k.ExecuteCommand[SetDesiredViewportCmd](
		SetDesiredViewportRequest{Mode: ViewportFixedHeight, Size: 720})

	response := k.ExecuteCommand[SetViewportCmd](SetViewportRequest{Width: 1000, Height: 1000})
	want := Viewport{Width: 720, Height: 720, WindowWidth: 1000, WindowHeight: 1000}
	if response.Viewport != want {
		t.Errorf("viewport = %#v, want %#v", response.Viewport, want)
	}
}

func TestViewportFitAndCover(t *testing.T) {
	tests := []struct {
		name   string
		mode   ViewportMode
		width  float32
		height float32
		want   Viewport
	}{
		{name: "fit wider", mode: ViewportFit, width: 2000, height: 1000, want: Viewport{Width: 1440, Height: 720, WindowWidth: 2000, WindowHeight: 1000}},
		{name: "fit narrower", mode: ViewportFit, width: 1000, height: 1000, want: Viewport{Width: 1280, Height: 1280, WindowWidth: 1000, WindowHeight: 1000}},
		{name: "cover wider", mode: ViewportCover, width: 2000, height: 1000, want: Viewport{Width: 1280, Height: 640, WindowWidth: 2000, WindowHeight: 1000}},
		{name: "cover narrower", mode: ViewportCover, width: 1000, height: 1000, want: Viewport{Width: 720, Height: 720, WindowWidth: 1000, WindowHeight: 1000}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			k := newTestKernel(t, newPlugin())
			k.ExecuteCommand[SetDesiredViewportCmd](
				SetDesiredViewportRequest{Mode: test.mode, Width: 1280, Height: 720})

			response := k.ExecuteCommand[SetViewportCmd](
				SetViewportRequest{Width: test.width, Height: test.height})
			if response.Viewport != test.want {
				t.Errorf("viewport = %#v, want %#v", response.Viewport, test.want)
			}
		})
	}
}
