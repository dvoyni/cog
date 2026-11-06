package gfx_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gfx/recording"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/platform"
)

func TestAnOpenedWindowGetsASurfaceFromItsNativeHandle(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil)

	h.openWindow()
	window := h.openWindow()
	var native platform.NativeHandle
	h.editWindows(func(manager *platform.WindowManager) { native = manager.Native(window) })

	var created []recording.CreateSurfaceArguments
	for _, call := range h.waitForCalls(recording.CallKindCreateSurface, 2) {
		created = append(created, call.Arguments.(recording.CreateSurfaceArguments))
	}
	if !slices.Contains(created, recording.CreateSurfaceArguments{Window: window, Native: native}) {
		t.Fatalf("created surfaces %+v, want one for %v from %+v", created, window, native)
	}
}

func TestAClosedWindowsSurfaceIsReleased(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil)
	window := h.openWindow()
	h.waitForCalls(recording.CallKindCreateSurface, 1)

	h.closeWindow(window)

	if call := h.waitForCalls(recording.CallKindReleaseSurface, 1)[0]; call.Arguments != window || call.Error != nil {
		t.Fatalf("released the surface of %v, answering %v, want %v's", call.Arguments, call.Error, window)
	}
}

func TestDestroyReleasesTheSurfacesOfOpenWindowsFirst(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil)
	window := h.openWindow()
	h.waitForCalls(recording.CallKindCreateSurface, 1)

	h.quit()

	calls := h.instance.Recording().Calls
	want := []recording.CallKind{
		recording.CallKindReleaseSurface, recording.CallKindReleaseDevice,
		recording.CallKindReleaseAdapter, recording.CallKindReleaseInstance,
	}
	if got := kinds(calls[len(calls)-len(want):]); !slices.Equal(got, want) {
		t.Fatalf("last calls = %v, want %v", kinds(calls), want)
	}
	if released := calls[len(calls)-len(want)]; released.Arguments != window || released.Error != nil {
		t.Errorf("released the surface of %v, answering %v, want %v's", released.Arguments, released.Error, window)
	}
}

func TestAFailedSurfaceIsLoggedAndNeverReleased(t *testing.T) {
	failure := errors.New("no surface")
	h := newHarness(t)
	h.instance.AnswerNext(recording.CallKindCreateSurface, recording.Answer{Error: failure})
	h.mustSpawn(nil)
	window := h.openWindow()
	h.waitForCalls(recording.CallKindCreateSurface, 1)

	h.closeWindow(window)
	h.quit()

	if logged := h.takeLogged(); len(logged) != 1 || !errors.Is(logged[0], failure) {
		t.Errorf("logged %v, want %v", logged, failure)
	}
	for _, call := range h.instance.Recording().Calls {
		if call.Kind == recording.CallKindReleaseSurface {
			t.Fatalf("released the surface of %v, which was never created", call.Arguments)
		}
	}
}

func TestHandlersHoldTheSurfaceSubscriptions(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil)

	if !h.handlersFound {
		t.Fatal("gfx installed no Handlers")
	}
	if h.handlers.CreateSurface == (kernel.Sub[platform.WindowOpenedEvent]{}) {
		t.Error("Handlers.CreateSurface is the zero Sub")
	}
	if h.handlers.ReleaseSurface == (kernel.Sub[platform.WindowClosedEvent]{}) {
		t.Error("Handlers.ReleaseSurface is the zero Sub")
	}
}

func TestANewSurfaceIsConfiguredWithBGRA8UnormAndFifoBeforeItsFirstAcquire(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil)
	window := h.openWindow()
	h.waitForCalls(recording.CallKindCreateSurface, 1)

	h.advance(time.Millisecond)

	want := gfx.SurfaceConfiguration{Format: gfx.TextureFormatBGRA8Unorm, PresentMode: gfx.PresentModeFifo, Width: 64, Height: 48}
	calls := surfaceCalls(h.instance.Recording().Calls)
	if got := kinds(calls); !slices.Equal(got, []recording.CallKind{
		recording.CallKindSurfaceCapabilities, recording.CallKindConfigureSurface,
		recording.CallKindAcquireFramebuffer, recording.CallKindPresent,
	}) {
		t.Fatalf("surface calls = %v, want the capabilities and the configure before the acquire", got)
	}
	if configured := calls[1]; configured.Arguments != (recording.ConfigureSurfaceArguments{Window: window, Configuration: want}) || configured.Error != nil {
		t.Errorf("configured %+v, answering %v, want %v with %+v", configured.Arguments, configured.Error, window, want)
	}
	h.readDevice(func(device gfx.Device) {
		if got, ok := device.SurfaceConfiguration(window); !ok || got != want {
			t.Errorf("SurfaceConfiguration = %+v, %v, want %+v", got, ok, want)
		}
	})
}

func TestEverySurfaceIsConfiguredFromConfigSurface(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(&gfx.Config{Surface: gfx.SurfaceRequest{Format: gfx.TextureFormatBGRA8UnormSrgb, PresentMode: gfx.PresentModeMailbox}})
	first := h.openWindow()
	second := h.openWindow()
	h.waitForCalls(recording.CallKindCreateSurface, 2)

	h.advance(time.Millisecond)

	var configured []recording.ConfigureSurfaceArguments
	for _, call := range h.waitForCalls(recording.CallKindConfigureSurface, 2) {
		configured = append(configured, call.Arguments.(recording.ConfigureSurfaceArguments))
	}
	want := gfx.SurfaceConfiguration{Format: gfx.TextureFormatBGRA8UnormSrgb, PresentMode: gfx.PresentModeMailbox, Width: 64, Height: 48}
	for _, window := range []platform.Window{first, second} {
		if !slices.Contains(configured, recording.ConfigureSurfaceArguments{Window: window, Configuration: want}) {
			t.Errorf("configured %+v, want %v with %+v", configured, window, want)
		}
	}
}

func TestASurfaceIsConfiguredOnceAcrossFrames(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil)
	h.openWindow()
	h.waitForCalls(recording.CallKindCreateSurface, 1)

	for range 3 {
		h.advance(time.Millisecond)
	}

	count := 0
	for _, call := range h.instance.Recording().Calls {
		if call.Kind == recording.CallKindConfigureSurface {
			count++
		}
	}
	if count != 1 {
		t.Errorf("%d configures over three frames, want 1", count)
	}
}

func TestAPartTheSurfaceLacksFallsBackWithALogErr(t *testing.T) {
	h := newHarness(t)
	h.instance.AnswerNext(recording.CallKindSurfaceCapabilities, recording.Answer{Capabilities: gfx.SurfaceCapabilities{
		Formats:      []gfx.TextureFormat{gfx.TextureFormatRGBA8Unorm, gfx.TextureFormatBGRA8Unorm},
		PresentModes: []gfx.PresentMode{gfx.PresentModeFifo},
		AlphaModes:   []gfx.CompositeAlphaMode{gfx.CompositeAlphaModeOpaque},
	}})
	h.mustSpawn(&gfx.Config{Surface: gfx.SurfaceRequest{Format: gfx.TextureFormatRGBA8UnormSrgb, PresentMode: gfx.PresentModeMailbox}})
	window := h.openWindow()
	h.waitForCalls(recording.CallKindCreateSurface, 1)

	h.advance(time.Millisecond)

	want := gfx.SurfaceConfiguration{Format: gfx.TextureFormatBGRA8Unorm, PresentMode: gfx.PresentModeFifo, Width: 64, Height: 48}
	if configured := h.waitForCalls(recording.CallKindConfigureSurface, 1)[0]; configured.Arguments != (recording.ConfigureSurfaceArguments{Window: window, Configuration: want}) {
		t.Errorf("configured %+v, want %v with %+v", configured.Arguments, window, want)
	}
	if logged := h.takeLogged(); len(logged) != 2 {
		t.Errorf("logged %v, want the format's and the present mode's fallbacks", logged)
	}
}

func TestAFailingConfigureIsLoggedAndItsWindowNotAcquired(t *testing.T) {
	failure := errors.New("no configure")
	h := newHarness(t)
	h.instance.AnswerNext(recording.CallKindConfigureSurface, recording.Answer{Error: failure})
	h.mustSpawn(nil)
	window := h.openWindow()
	h.waitForCalls(recording.CallKindCreateSurface, 1)

	h.advance(time.Millisecond)

	if logged := h.takeLogged(); len(logged) != 1 || !errors.Is(logged[0], failure) {
		t.Errorf("logged %v, want %v", logged, failure)
	}
	if calls := frameCalls(h.instance.Recording().Calls); len(calls) != 0 {
		t.Errorf("frame calls = %v, want none for a surface not configured", kinds(calls))
	}
	h.readDevice(func(device gfx.Device) {
		if got, ok := device.SurfaceConfiguration(window); ok {
			t.Errorf("SurfaceConfiguration = %+v, want none", got)
		}
	})
}

func TestAClosedWindowHasNoSurfaceConfiguration(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil)
	window := h.openWindow()
	h.waitForCalls(recording.CallKindCreateSurface, 1)
	h.advance(time.Millisecond)

	h.closeWindow(window)
	h.waitForCalls(recording.CallKindReleaseSurface, 1)

	h.readDevice(func(device gfx.Device) {
		if got, ok := device.SurfaceConfiguration(window); ok {
			t.Errorf("SurfaceConfiguration = %+v after the close, want none", got)
		}
	})
}

// surfaceCalls answers the capabilities, configures, acquires and presents
// among calls, in order.
func surfaceCalls(calls []recording.Call) []recording.Call {
	var answer []recording.Call
	for _, call := range calls {
		switch call.Kind {
		case recording.CallKindSurfaceCapabilities, recording.CallKindConfigureSurface,
			recording.CallKindAcquireFramebuffer, recording.CallKindPresent:
			answer = append(answer, call)
		}
	}
	return answer
}

func TestTasksHoldTheSurfaceJobsForABackendToPin(t *testing.T) {
	h := newHarness(t)
	h.pinSurfaceTasks = true
	h.mustSpawn(nil)

	if !h.tasksFound {
		t.Fatal("gfx installed no Tasks")
	}
	if h.tasks.CreateSurface == nil || h.tasks.ReleaseSurface == nil || h.tasks.ConfigureSurfaces == nil ||
		h.tasks.AcquireFramebuffers == nil || h.tasks.PresentFramebuffers == nil {
		t.Errorf("Tasks = %+v, want every surface job", h.tasks)
	}
}
