package internal

import (
	"testing"

	"github.com/dvoyni/cog/slots/gfx"
)

func TestViewportFixedWidth(t *testing.T) {
	k := newTestKernel(t, newPlugin())
	k.ExecuteCommand[gfx.SetDesiredViewportCmd](
		gfx.SetDesiredViewportRequest{Mode: gfx.ViewportFixedWidth, Size: 1280})

	response, _ := k.ExecuteCommand[gfx.SetViewportCmd](gfx.SetViewportRequest{Width: 1920, Height: 1080})
	want := gfx.Viewport{Width: 1280, Height: 720, WindowWidth: 1920, WindowHeight: 1080}
	if response.Viewport != want {
		t.Errorf("viewport = %#v, want %#v", response.Viewport, want)
	}

	response, _ = k.ExecuteCommand[gfx.SetViewportCmd](gfx.SetViewportRequest{Width: 500, Height: 1000})
	want = gfx.Viewport{Width: 1280, Height: 2560, WindowWidth: 500, WindowHeight: 1000}
	if response.Viewport != want {
		t.Errorf("portrait viewport = %#v, want %#v", response.Viewport, want)
	}
}

func TestViewportCarriesFramebufferSizeAcrossPreferenceChanges(t *testing.T) {
	k := newTestKernel(t, newPlugin())
	response, _ := k.ExecuteCommand[gfx.SetViewportCmd](gfx.SetViewportRequest{
		Width: 1280, Height: 720, FramebufferWidth: 2560, FramebufferHeight: 1440,
	})
	if response.Viewport.FramebufferWidth != 2560 || response.Viewport.FramebufferHeight != 1440 {
		t.Fatalf("framebuffer = %vx%v, want 2560x1440", response.Viewport.FramebufferWidth, response.Viewport.FramebufferHeight)
	}
	desiredResponse, _ := k.ExecuteCommand[gfx.SetDesiredViewportCmd](gfx.SetDesiredViewportRequest{
		Mode: gfx.ViewportFit, Width: 1280, Height: 720,
	})
	if desiredResponse.Viewport.FramebufferWidth != 2560 || desiredResponse.Viewport.FramebufferHeight != 1440 {
		t.Fatalf("framebuffer after preference = %vx%v, want 2560x1440", desiredResponse.Viewport.FramebufferWidth, desiredResponse.Viewport.FramebufferHeight)
	}
}

func TestViewportFixedHeight(t *testing.T) {
	k := newTestKernel(t, newPlugin())
	k.ExecuteCommand[gfx.SetDesiredViewportCmd](
		gfx.SetDesiredViewportRequest{Mode: gfx.ViewportFixedHeight, Size: 720})

	response, _ := k.ExecuteCommand[gfx.SetViewportCmd](gfx.SetViewportRequest{Width: 1000, Height: 1000})
	want := gfx.Viewport{Width: 720, Height: 720, WindowWidth: 1000, WindowHeight: 1000}
	if response.Viewport != want {
		t.Errorf("viewport = %#v, want %#v", response.Viewport, want)
	}
}

func TestViewportFitAndCover(t *testing.T) {
	tests := []struct {
		name   string
		mode   gfx.ViewportMode
		width  float32
		height float32
		want   gfx.Viewport
	}{
		{name: "fit wider", mode: gfx.ViewportFit, width: 2000, height: 1000, want: gfx.Viewport{Width: 1440, Height: 720, WindowWidth: 2000, WindowHeight: 1000}},
		{name: "fit narrower", mode: gfx.ViewportFit, width: 1000, height: 1000, want: gfx.Viewport{Width: 1280, Height: 1280, WindowWidth: 1000, WindowHeight: 1000}},
		{name: "cover wider", mode: gfx.ViewportCover, width: 2000, height: 1000, want: gfx.Viewport{Width: 1280, Height: 640, WindowWidth: 2000, WindowHeight: 1000}},
		{name: "cover narrower", mode: gfx.ViewportCover, width: 1000, height: 1000, want: gfx.Viewport{Width: 720, Height: 720, WindowWidth: 1000, WindowHeight: 1000}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			k := newTestKernel(t, newPlugin())
			k.ExecuteCommand[gfx.SetDesiredViewportCmd](
				gfx.SetDesiredViewportRequest{Mode: test.mode, Width: 1280, Height: 720})

			response, _ := k.ExecuteCommand[gfx.SetViewportCmd](
				gfx.SetViewportRequest{Width: test.width, Height: test.height})
			if response.Viewport != test.want {
				t.Errorf("viewport = %#v, want %#v", response.Viewport, test.want)
			}
		})
	}
}
