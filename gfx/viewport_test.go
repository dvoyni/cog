package gfx

import (
	"testing"

	"github.com/dvoyni/cog/app"
)

func TestViewportFixedWidth(t *testing.T) {
	k := newTestKernel(t, New())
	k.ExecuteCommand[app.SetDesiredViewportCmd](
		app.SetDesiredViewportRequest{Mode: app.ViewportFixedWidth, Size: 1280})

	response, _ := k.ExecuteCommand[app.SetViewportCmd](app.SetViewportRequest{Width: 1920, Height: 1080})
	want := app.Viewport{Width: 1280, Height: 720, WindowWidth: 1920, WindowHeight: 1080}
	if response.Viewport != want {
		t.Errorf("viewport = %#v, want %#v", response.Viewport, want)
	}

	response, _ = k.ExecuteCommand[app.SetViewportCmd](app.SetViewportRequest{Width: 500, Height: 1000})
	want = app.Viewport{Width: 1280, Height: 2560, WindowWidth: 500, WindowHeight: 1000}
	if response.Viewport != want {
		t.Errorf("portrait viewport = %#v, want %#v", response.Viewport, want)
	}
}

func TestViewportCarriesFramebufferSizeAcrossPreferenceChanges(t *testing.T) {
	k := newTestKernel(t, New())
	response, _ := k.ExecuteCommand[app.SetViewportCmd](app.SetViewportRequest{
		Width: 1280, Height: 720, FramebufferWidth: 2560, FramebufferHeight: 1440,
	})
	if response.Viewport.FramebufferWidth != 2560 || response.Viewport.FramebufferHeight != 1440 {
		t.Fatalf("framebuffer = %vx%v, want 2560x1440", response.Viewport.FramebufferWidth, response.Viewport.FramebufferHeight)
	}
	desiredResponse, _ := k.ExecuteCommand[app.SetDesiredViewportCmd](app.SetDesiredViewportRequest{
		Mode: app.ViewportFit, Width: 1280, Height: 720,
	})
	if desiredResponse.Viewport.FramebufferWidth != 2560 || desiredResponse.Viewport.FramebufferHeight != 1440 {
		t.Fatalf("framebuffer after preference = %vx%v, want 2560x1440", desiredResponse.Viewport.FramebufferWidth, desiredResponse.Viewport.FramebufferHeight)
	}
}

func TestViewportFixedHeight(t *testing.T) {
	k := newTestKernel(t, New())
	k.ExecuteCommand[app.SetDesiredViewportCmd](
		app.SetDesiredViewportRequest{Mode: app.ViewportFixedHeight, Size: 720})

	response, _ := k.ExecuteCommand[app.SetViewportCmd](app.SetViewportRequest{Width: 1000, Height: 1000})
	want := app.Viewport{Width: 720, Height: 720, WindowWidth: 1000, WindowHeight: 1000}
	if response.Viewport != want {
		t.Errorf("viewport = %#v, want %#v", response.Viewport, want)
	}
}

func TestViewportFitAndCover(t *testing.T) {
	tests := []struct {
		name   string
		mode   app.ViewportMode
		width  float32
		height float32
		want   app.Viewport
	}{
		{name: "fit wider", mode: app.ViewportFit, width: 2000, height: 1000, want: app.Viewport{Width: 1440, Height: 720, WindowWidth: 2000, WindowHeight: 1000}},
		{name: "fit narrower", mode: app.ViewportFit, width: 1000, height: 1000, want: app.Viewport{Width: 1280, Height: 1280, WindowWidth: 1000, WindowHeight: 1000}},
		{name: "cover wider", mode: app.ViewportCover, width: 2000, height: 1000, want: app.Viewport{Width: 1280, Height: 640, WindowWidth: 2000, WindowHeight: 1000}},
		{name: "cover narrower", mode: app.ViewportCover, width: 1000, height: 1000, want: app.Viewport{Width: 720, Height: 720, WindowWidth: 1000, WindowHeight: 1000}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			k := newTestKernel(t, New())
			k.ExecuteCommand[app.SetDesiredViewportCmd](
				app.SetDesiredViewportRequest{Mode: test.mode, Width: 1280, Height: 720})

			response, _ := k.ExecuteCommand[app.SetViewportCmd](
				app.SetViewportRequest{Width: test.width, Height: test.height})
			if response.Viewport != test.want {
				t.Errorf("viewport = %#v, want %#v", response.Viewport, test.want)
			}
		})
	}
}
