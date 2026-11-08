package gfx_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gfx/recording"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/metronome"
	"github.com/dvoyni/cog/platform"
)

func TestSpawnWithoutInstanceSocketPanics(t *testing.T) {
	h := newHarness(t)
	h.unbound = true
	defer func() {
		recovered := recover()
		message, _ := recovered.(string)
		if !strings.Contains(message, "gfx.InstanceSocket") || !strings.Contains(message, "has no binding") {
			t.Fatalf("Spawn panicked with %v, want the kernel's panic for an unbound gfx.InstanceSocket", recovered)
		}
	}()
	h.spawn(nil)
}

func TestDependenciesNamePlatformAndMetronome(t *testing.T) {
	got := gfx.NewPlugin().Dependencies()
	if want := []kernel.PluginName{platform.PluginName, metronome.PluginName}; !slices.Equal(got, want) {
		t.Fatalf("Dependencies = %v, want %v", got, want)
	}
}

func TestStartRequestsTheAdapterAndDeviceFromConfig(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(&gfx.Config{
		PowerPreference:      gfx.PowerPreferenceHighPerformance,
		ForceFallbackAdapter: true,
		RequiredFeatures:     gfx.FeatureFlagDepthClipControl | gfx.FeatureFlagTextureCompressionBC,
	})

	calls := h.instance.Recording().Calls
	want := []recording.CallKind{recording.CallKindRequestAdapter, recording.CallKindRequestDevice}
	if !slices.Equal(kinds(calls), want) {
		t.Fatalf("calls = %v, want %v", kinds(calls), want)
	}
	options := gfx.RequestAdapterOptions{PowerPreference: gfx.PowerPreferenceHighPerformance, ForceFallbackAdapter: true}
	if calls[0].Arguments != options {
		t.Errorf("RequestAdapter got %+v, want %+v", calls[0].Arguments, options)
	}
	descriptor := gfx.DeviceDescriptor{
		RequiredFeatures: gfx.FeatureFlagDepthClipControl | gfx.FeatureFlagTextureCompressionBC,
		RequiredLimits:   gfx.DefaultLimits(),
	}
	if calls[1].Arguments != descriptor {
		t.Errorf("RequestDevice got %+v, want %+v", calls[1].Arguments, descriptor)
	}
}

func TestZeroRequiredLimitsRequestDefaultLimits(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil)

	calls := h.instance.Recording().Calls
	descriptor, _ := calls[len(calls)-1].Arguments.(gfx.DeviceDescriptor)
	if descriptor.RequiredLimits != gfx.DefaultLimits() {
		t.Fatalf("RequestDevice limits = %+v, want DefaultLimits()", descriptor.RequiredLimits)
	}
}

func TestRequiredLimitsPassAsGiven(t *testing.T) {
	limits := gfx.DefaultLimits()
	limits.MaxBindGroups = 8
	h := newHarness(t)
	h.mustSpawn(&gfx.Config{RequiredLimits: limits})

	calls := h.instance.Recording().Calls
	descriptor, _ := calls[len(calls)-1].Arguments.(gfx.DeviceDescriptor)
	if descriptor.RequiredLimits != limits {
		t.Fatalf("RequestDevice limits = %+v, want %+v", descriptor.RequiredLimits, limits)
	}
}

func TestNoAdapterIsAnErrorFromStart(t *testing.T) {
	noAdapter := errors.New("no adapter")
	h := newHarness(t)
	h.instance.AnswerNext(recording.CallKindRequestAdapter, recording.Answer{Error: noAdapter})

	if err := h.spawn(nil); !errors.Is(err, noAdapter) {
		t.Fatalf("Spawn answered %v, want %v", err, noAdapter)
	}
	want := []recording.CallKind{recording.CallKindRequestAdapter, recording.CallKindReleaseInstance}
	if got := kinds(h.instance.Recording().Calls); !slices.Equal(got, want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
}

func TestNoDeviceIsAnErrorFromStart(t *testing.T) {
	noDevice := errors.New("no device")
	h := newHarness(t)
	h.instance.AnswerNext(recording.CallKindRequestDevice, recording.Answer{Error: noDevice})

	if err := h.spawn(nil); !errors.Is(err, noDevice) {
		t.Fatalf("Spawn answered %v, want %v", err, noDevice)
	}
	want := []recording.CallKind{
		recording.CallKindRequestAdapter, recording.CallKindRequestDevice,
		recording.CallKindReleaseAdapter, recording.CallKindReleaseInstance,
	}
	if got := kinds(h.instance.Recording().Calls); !slices.Equal(got, want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
}

func TestDeviceAnswersWhatItWasCreatedWith(t *testing.T) {
	features := gfx.FeatureFlagDepthClipControl | gfx.FeatureFlagShaderF16
	h := newHarness(t)
	h.mustSpawn(&gfx.Config{RequiredFeatures: features})

	h.readDevice(func(device gfx.Device) {
		if got := device.Features(); got != features {
			t.Errorf("Features = %#x, want %#x", got, features)
		}
		if got := device.Limits(); got != gfx.DefaultLimits() {
			t.Errorf("Limits = %+v, want DefaultLimits()", got)
		}
		if got := device.AdapterInfo(); got != recording.AdapterInfo {
			t.Errorf("AdapterInfo = %+v, want %+v", got, recording.AdapterInfo)
		}
	})
}

func TestElevatedDeviceWaitsIdle(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil)

	h.writeDevice(func(device kernel.Write[gfx.Device]) {
		if err := gfx.ElevateDeviceAccess(device).WaitIdle(); err != nil {
			t.Errorf("WaitIdle answered %v", err)
		}
	})
	calls := h.instance.Recording().Calls
	if got := calls[len(calls)-1].Kind; got != recording.CallKindWaitIdle {
		t.Fatalf("last call = %v, want %v", got, recording.CallKindWaitIdle)
	}
}

func TestDeviceAccessIsNotElevatedByAssertion(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil)

	h.readDevice(func(device gfx.Device) {
		if _, ok := device.(gfx.ExclusiveDevice); ok {
			t.Error("the read Device asserts into an ExclusiveDevice")
		}
	})
}

func TestTeardownReleasesTheDeviceAdapterAndInstance(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil)
	h.quit()

	want := []recording.CallKind{
		recording.CallKindRequestAdapter, recording.CallKindRequestDevice,
		recording.CallKindReleaseDevice, recording.CallKindReleaseAdapter, recording.CallKindReleaseInstance,
	}
	if got := kinds(h.instance.Recording().Calls); !slices.Equal(got, want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
}

func TestTeardownReleasesTheDeviceInAJobBeforeAnyDestroy(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil)

	h.quit()

	assertDeviceReleasedBeforeAnyDestroy(t, h)
}

func TestAPinnedDeviceReleaseWaitsForTheMainThread(t *testing.T) {
	h := newHarness(t)
	h.pinTasks = true
	h.mustSpawn(nil)
	callCount := len(h.instance.Recording().Calls)

	h.supervisor.Quit()
	select {
	case <-h.supervisor.Done():
		t.Error("the supervisor quit with the main thread never ticked, want the pinned release still waiting for it")
	case <-time.After(50 * time.Millisecond):
	}
	if calls := h.instance.Recording().Calls[callCount:]; len(calls) != 0 {
		t.Errorf("the teardown made calls %v before the main thread ticked, want none", kinds(calls))
	}
	h.quit()

	assertDeviceReleasedBeforeAnyDestroy(t, h)
}

func TestABeatAfterTheDeviceIsReleasedDrawsNothing(t *testing.T) {
	h := newHarness(t)
	h.beatAtStop = true
	h.mustSpawn(nil)

	h.quit()

	if calls := h.instance.Recording().Calls; calls[len(calls)-1].Kind != recording.CallKindReleaseInstance {
		t.Errorf("calls = %v, want none after the instance's release", kinds(calls))
	}
}

// assertDeviceReleasedBeforeAnyDestroy fails t unless the last calls h's
// instance had recorded when the harness's Destroy ran, before gfx's, are the
// device's, the adapter's and the instance's releases.
func assertDeviceReleasedBeforeAnyDestroy(t *testing.T, h *harness) {
	t.Helper()
	want := []recording.CallKind{recording.CallKindReleaseDevice, recording.CallKindReleaseAdapter, recording.CallKindReleaseInstance}
	if got := kinds(h.callsAtDestroy); len(got) < len(want) || !slices.Equal(got[len(got)-len(want):], want) {
		t.Errorf("calls when the first Destroy ran = %v, want them ending with %v", got, want)
	}
}
