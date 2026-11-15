package rendertest_test

import (
	"slices"
	"testing"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gfx/recording"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/platform"
	"github.com/dvoyni/cog/render"
	"github.com/dvoyni/cog/render/rendertest"
)

func TestASurfaceRequestReachesEverySurfaceBeforeTheNextAcquire(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.Spawn(nil, &firstFramePlugin[render.Surface]{onFirstFrame: func(_ *kernel.Kernel, surface render.Surface) {
		surface.Request(render.SurfaceRequest{Format: render.TextureFormatBGRA8UnormSrgb, PresentMode: render.PresentModeMailbox})
	}})
	windowSizes := []platform.Size{{Width: 64, Height: 48}, {Width: 32, Height: 16}, {Width: 16, Height: 8}}
	windows := []platform.Window{h.OpenWindow(windowSizes[0]), h.OpenWindow(windowSizes[1])}
	h.Step()
	callCountBefore := len(h.Calls())

	h.Step()
	windows = append(windows, h.OpenWindow(windowSizes[2]))
	h.Step()

	var configureArguments []recording.ConfigureSurfaceArguments
	for _, call := range h.Calls()[callCountBefore:] {
		switch call.Kind {
		case recording.CallKindConfigureSurface:
			configureArguments = append(configureArguments, call.Arguments.(recording.ConfigureSurfaceArguments))
		case recording.CallKindAcquireFramebuffer:
			if len(configureArguments) < 2 {
				t.Fatalf("an acquire after %d configures, want both windows configured before the next acquire", len(configureArguments))
			}
		}
	}
	for index, window := range windows {
		want := recording.ConfigureSurfaceArguments{Window: window, Configuration: gfx.SurfaceConfiguration{
			Format:      gfx.TextureFormatBGRA8UnormSrgb,
			PresentMode: gfx.PresentModeMailbox,
			Width:       uint32(windowSizes[index].Width),
			Height:      uint32(windowSizes[index].Height),
		}}
		if !slices.Contains(configureArguments, want) {
			t.Errorf("configured %+v, want %+v among them", configureArguments, want)
		}
	}
}
