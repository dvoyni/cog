package gfx_test

import (
	"errors"
	"slices"
	"testing"

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
