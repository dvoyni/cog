package gfx_test

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gfx/recording"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/platform"
)

func TestADeviceLossFromABackendCallIsLoggedThenPublishedOnce(t *testing.T) {
	for _, test := range []struct {
		name string
		kind recording.CallKind
		// lose makes the call of kind under the read lock; when nil, the
		// next frame makes it.
		lose func(device gfx.Device, buffer gfx.Buffer) error
	}{
		{"CreateBuffer", recording.CallKindCreateBuffer, func(device gfx.Device, _ gfx.Buffer) error {
			_, err := device.CreateBuffer(gfx.BufferDescriptor{Size: 4, Usage: gfx.BufferUsageFlagCopyDst})
			return err
		}},
		{"WriteBuffer", recording.CallKindWriteBuffer, func(device gfx.Device, buffer gfx.Buffer) error {
			return device.WriteBuffer(buffer, 0, make([]byte, 4))
		}},
		{"MapBuffer", recording.CallKindMapBuffer, func(device gfx.Device, buffer gfx.Buffer) error {
			return device.MapBuffer(buffer, gfx.MapModeRead, 0, 4)
		}},
		{"a standalone Submit", recording.CallKindSubmit, func(device gfx.Device, buffer gfx.Buffer) error {
			encoder, err := device.CreateCommandEncoder(gfx.CommandEncoderDescriptor{})
			if err != nil {
				return err
			}
			encoder.ClearBuffer(buffer, 0, 4)
			commandBuffer, err := encoder.Finish()
			if err != nil {
				return err
			}
			return device.Submit(commandBuffer)
		}},
		{"ConfigureSurface", recording.CallKindConfigureSurface, nil},
		{"AcquireFramebuffer", recording.CallKindAcquireFramebuffer, nil},
		{"a frame encoder's Finish", recording.CallKindFinish, nil},
		{"a frame's Submit", recording.CallKindSubmit, nil},
		{"Present", recording.CallKindPresent, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t)
			var buffer gfx.Buffer
			h.readFrame = func(event gfx.DrawEvent, device gfx.Device) {
				encoder, _ := device.Encoder(event.Frame, 0)
				encoder.ClearBuffer(buffer, 0, 4)
			}
			h.mustSpawn(nil)
			h.openWindow()
			h.waitForCalls(recording.CallKindCreateSurface, 1)
			h.readDevice(func(device gfx.Device) { buffer = createReadbackBuffer(t, device, 256) })
			backendLoss := fmt.Errorf("vulkan: %w", gfx.ErrDeviceLost)
			h.instance.AnswerNext(test.kind, recording.Answer{Error: backendLoss})

			if test.lose != nil {
				h.readDevice(func(device gfx.Device) {
					if err := test.lose(device, buffer); !errors.Is(err, backendLoss) {
						t.Errorf("the losing call answered %v, want %v", err, backendLoss)
					}
				})
			} else {
				h.advance(time.Millisecond)
			}

			if lost := h.lostEvents(); !reflect.DeepEqual(lost, []int{1}) {
				t.Errorf("DeviceLostEvent published with these errors logged before each: %v, want once, after one", lost)
			}
			if logged := h.takeLogged(); len(logged) != 1 || !errors.Is(logged[0], backendLoss) {
				t.Errorf("logged %v, want the loss once", logged)
			}
			callCount := len(h.instance.Recording().Calls)

			h.readDevice(func(device gfx.Device) {
				if _, err := device.CreateBuffer(gfx.BufferDescriptor{Size: 4, Usage: gfx.BufferUsageFlagCopyDst}); !errors.Is(err, gfx.ErrDeviceLost) {
					t.Errorf("CreateBuffer after the loss answered %v, want ErrDeviceLost", err)
				}
			})

			if lost := h.lostEvents(); len(lost) != 1 {
				t.Errorf("DeviceLostEvent published %d times, want once", len(lost))
			}
			if logged := h.takeLogged(); len(logged) != 0 {
				t.Errorf("logged %v after the loss, want nothing more", logged)
			}
			if calls := h.instance.Recording().Calls[callCount:]; len(calls) != 0 {
				t.Errorf("calls %v reached the backend after the loss, want none", kinds(calls))
			}
		})
	}
}

func TestOnceTheDeviceIsLostNoCallReachesTheBackendAndCallsWithErrorsAnswerErrDeviceLost(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil)
	var buffer gfx.Buffer
	var commandBuffer gfx.CommandBuffer
	h.readDevice(func(device gfx.Device) {
		buffer = createReadbackBuffer(t, device, 256)
		encoder, err := device.CreateCommandEncoder(gfx.CommandEncoderDescriptor{})
		mustSucceed(t, err)
		encoder.ClearBuffer(buffer, 0, 4)
		commandBuffer, err = encoder.Finish()
		mustSucceed(t, err)
	})
	h.instance.AnswerNext(recording.CallKindCreateBuffer, recording.Answer{Error: gfx.ErrDeviceLost})
	h.readDevice(func(device gfx.Device) {
		_, _ = device.CreateBuffer(gfx.BufferDescriptor{Size: 4, Usage: gfx.BufferUsageFlagCopyDst})
	})
	callCount := len(h.instance.Recording().Calls)

	h.readDevice(func(device gfx.Device) {
		for name, call := range map[string]func() error{
			"CreateBuffer": func() error {
				_, err := device.CreateBuffer(gfx.BufferDescriptor{Size: 4, Usage: gfx.BufferUsageFlagCopyDst})
				return err
			},
			"CreateTexture": func() error {
				_, err := device.CreateTexture(gfx.TextureDescriptor{
					Size:          gfx.Extent3D{Width: 1, Height: 1, DepthOrArrayLayers: 1},
					MipLevelCount: 1,
					SampleCount:   1,
					Dimension:     gfx.TextureDimension2D,
					Format:        gfx.TextureFormatRGBA8Unorm,
					Usage:         gfx.TextureUsageFlagCopyDst,
				})
				return err
			},
			"CreateTextureView": func() error {
				_, err := device.CreateTextureView(gfx.Texture{}, gfx.TextureViewDescriptor{})
				return err
			},
			"CreateSampler": func() error {
				_, err := device.CreateSampler(gfx.SamplerDescriptor{})
				return err
			},
			"CreateShaderModule": func() error {
				_, err := device.CreateShaderModule(gfx.ShaderModuleDescriptor{WGSL: triangleWGSL})
				return err
			},
			"CreateBindGroupLayout": func() error {
				_, err := device.CreateBindGroupLayout(gfx.BindGroupLayoutDescriptor{})
				return err
			},
			"CreateBindGroup": func() error {
				_, err := device.CreateBindGroup(gfx.BindGroupDescriptor{})
				return err
			},
			"CreatePipelineLayout": func() error {
				_, err := device.CreatePipelineLayout(gfx.PipelineLayoutDescriptor{})
				return err
			},
			"CreateRenderPipeline": func() error {
				_, err := device.CreateRenderPipeline(gfx.RenderPipelineDescriptor{})
				return err
			},
			"CreateComputePipeline": func() error {
				_, err := device.CreateComputePipeline(gfx.ComputePipelineDescriptor{})
				return err
			},
			"WriteBuffer": func() error { return device.WriteBuffer(buffer, 0, make([]byte, 4)) },
			"WriteTexture": func() error {
				return device.WriteTexture(gfx.ImageCopyTexture{}, nil, gfx.ImageDataLayout{}, gfx.Extent3D{})
			},
			"Encoder": func() error {
				encoder, err := device.Encoder(gfx.Frame{}, 0)
				encoder.ClearBuffer(buffer, 0, 4)
				return err
			},
			"CreateCommandEncoder": func() error {
				_, err := device.CreateCommandEncoder(gfx.CommandEncoderDescriptor{})
				return err
			},
			"Submit":         func() error { return device.Submit(commandBuffer) },
			"MapBuffer":      func() error { return device.MapBuffer(buffer, gfx.MapModeRead, 0, 4) },
			"MappedRange":    func() error { _, err := device.MappedRange(buffer, 0, 4); return err },
			"UnmapBuffer":    func() error { return device.UnmapBuffer(buffer) },
			"RequestCapture": func() error { return device.RequestCapture(platform.Window{}) },
		} {
			if err := call(); !errors.Is(err, gfx.ErrDeviceLost) {
				t.Errorf("%s answered %v, want ErrDeviceLost", name, err)
			}
		}
		device.Poll()
		device.ReleaseBuffer(buffer)
	})
	h.writeDevice(func(device kernel.Write[gfx.Device]) {
		if err := gfx.ElevateDeviceAccess(device).WaitIdle(); !errors.Is(err, gfx.ErrDeviceLost) {
			t.Errorf("WaitIdle answered %v, want ErrDeviceLost", err)
		}
	})

	if calls := h.instance.Recording().Calls[callCount:]; len(calls) != 0 {
		t.Errorf("calls %v reached the backend after the loss, want none", kinds(calls))
	}
	if submissions := h.instance.Recording().Submissions; len(submissions) != 0 {
		t.Errorf("submissions = %+v, want none", submissions)
	}
	if logged := h.takeLogged(); len(logged) != 1 {
		t.Errorf("logged %v, want the loss once", logged)
	}
}

func TestTheFrameInFlightAtALossSkipsItsSubmitAndPresent(t *testing.T) {
	for _, test := range []struct {
		name string
		// lose queues the answers that lose the device in the frame, which
		// takes two encoders.
		lose func(h *harness)
		// skipped are the kinds of call the lost frame makes none of.
		skipped []recording.CallKind
	}{
		{"lost by a recording task", func(h *harness) {
			h.instance.AnswerNext(recording.CallKindCreateBuffer, recording.Answer{Error: gfx.ErrDeviceLost})
		}, []recording.CallKind{recording.CallKindFinish, recording.CallKindSubmit, recording.CallKindPresent}},
		{"lost by the second encoder's Finish", func(h *harness) {
			h.instance.AnswerNext(recording.CallKindFinish, recording.Answer{})
			h.instance.AnswerNext(recording.CallKindFinish, recording.Answer{Error: gfx.ErrDeviceLost})
		}, []recording.CallKind{recording.CallKindSubmit, recording.CallKindPresent}},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t)
			var buffer gfx.Buffer
			h.readFrame = func(event gfx.DrawEvent, device gfx.Device) {
				for order := range gfx.SubmitOrder(2) {
					encoder, err := device.Encoder(event.Frame, order)
					mustSucceed(t, err)
					encoder.ClearBuffer(buffer, 0, 4)
				}
				_, _ = device.CreateBuffer(gfx.BufferDescriptor{Size: 4, Usage: gfx.BufferUsageFlagCopyDst})
			}
			h.mustSpawn(nil)
			h.openWindow()
			h.waitForCalls(recording.CallKindCreateSurface, 1)
			h.readDevice(func(device gfx.Device) { buffer = createReadbackBuffer(t, device, 256) })
			test.lose(h)

			h.advance(time.Millisecond)

			called := kinds(h.instance.Recording().Calls)
			if !slices.Contains(called, recording.CallKindAcquireFramebuffer) {
				t.Fatalf("calls %v, want the frame's acquire", called)
			}
			for _, kind := range test.skipped {
				if slices.Contains(called, kind) {
					t.Errorf("calls %v hold a %v, want the lost frame to make none", called, kind)
				}
			}
			if logged := h.takeLogged(); len(logged) != 1 || !errors.Is(logged[0], gfx.ErrDeviceLost) {
				t.Errorf("logged %v, want the loss once", logged)
			}
		})
	}
}

func TestALossAnsweredByTwoCallsIsPublishedOnce(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil)
	h.instance.AnswerNext(recording.CallKindMapStatus, recording.Answer{Error: gfx.ErrDeviceLost})
	h.instance.AnswerNext(recording.CallKindMapStatus, recording.Answer{Error: gfx.ErrDeviceLost})

	h.readDevice(func(device gfx.Device) {
		for range 2 {
			mustSucceed(t, device.MapBuffer(createReadbackBuffer(t, device, 256), gfx.MapModeRead, 0, 4))
		}
		device.Poll()
	})

	if lost := h.lostEvents(); !reflect.DeepEqual(lost, []int{1}) {
		t.Errorf("DeviceLostEvent published with these errors logged before each: %v, want once, after one", lost)
	}
	if logged := h.takeLogged(); len(logged) != 1 {
		t.Errorf("logged %v, want the loss once", logged)
	}
}

func TestTheBeatAfterALossRecreatesEveryObjectUnderItsOldHandleInsteadOfDrawing(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil)
	window := h.openWindow()
	h.waitForCalls(recording.CallKindCreateSurface, 1)
	h.advance(time.Millisecond)
	h.readDevice(func(device gfx.Device) {
		sampler, err := device.CreateSampler(gfx.SamplerDescriptor{})
		mustSucceed(t, err)
		device.ReleaseSampler(sampler)
	})
	before := len(h.instance.Recording().Calls)
	h.instance.AnswerNext(recording.CallKindReflectRenderPipelineLayout, recording.Answer{BindGroupLayouts: reflectedGroups()})
	h.readDevice(func(device gfx.Device) {
		live := createFixture(t, device)
		_, err := device.CreateRenderPipeline(renderPipelineDescriptor(live))
		mustSucceed(t, err)
		auto := renderPipelineDescriptor(live)
		auto.Layout = gfx.PipelineLayout{}
		_, err = device.CreateRenderPipeline(auto)
		mustSucceed(t, err)
		_, err = device.CreateComputePipeline(computePipelineDescriptor(live))
		mustSucceed(t, err)
		_, err = device.CreateBindGroup(gfx.BindGroupDescriptor{
			Layout:  live.groupLayout,
			Entries: []gfx.BindGroupEntry{{Binding: 0, Buffer: live.buffer, Size: 16}},
		})
		mustSucceed(t, err)
	})
	var created []recording.Call
	for _, call := range h.instance.Recording().Calls[before:] {
		if slices.Contains(recreationOrder, call.Kind) {
			created = append(created, call)
		}
	}
	slices.SortStableFunc(created, func(a, b recording.Call) int {
		return slices.Index(recreationOrder, a.Kind) - slices.Index(recreationOrder, b.Kind)
	})
	loseDevice(h)
	callCount := len(h.instance.Recording().Calls)
	drawCount := len(h.drawEvents())

	h.advance(time.Millisecond)

	if drawn := h.drawEvents(); len(drawn) != drawCount {
		t.Errorf("%d DrawEvents published by the recovering beat, want none", len(drawn)-drawCount)
	}
	if recovered := h.recoveredEvents(); recovered != 1 {
		t.Errorf("DeviceRecoveredEvent published %d times, want once", recovered)
	}
	want := append([]recording.Call{
		{Kind: recording.CallKindReleaseDevice},
		{Kind: recording.CallKindRequestDevice, Arguments: gfx.DeviceDescriptor{RequiredLimits: gfx.DefaultLimits()}},
	}, created...)
	if calls := h.instance.Recording().Calls[callCount:]; !reflect.DeepEqual(calls, want) {
		t.Errorf("the recovery made calls %v, want %v", calls, want)
	}
	h.readDevice(func(device gfx.Device) {
		_, err := device.CreateBuffer(gfx.BufferDescriptor{Size: 4, Usage: gfx.BufferUsageFlagCopyDst})
		mustSucceed(t, err)
	})
	callCount = len(h.instance.Recording().Calls)

	h.advance(time.Millisecond)

	if drawn := h.drawEvents(); len(drawn) != drawCount+1 {
		t.Errorf("%d DrawEvents published after the recovery, want one", len(drawn)-drawCount)
	}
	var surfaceCalls []recording.CallKind
	for _, call := range h.instance.Recording().Calls[callCount:] {
		if call.Error != nil {
			t.Errorf("the frame after the recovery made %v, answered %v", call.Kind, call.Error)
		}
		if call.Kind != recording.CallKindSurfaceCapabilities {
			surfaceCalls = append(surfaceCalls, call.Kind)
		}
	}
	if want := []recording.CallKind{recording.CallKindConfigureSurface, recording.CallKindAcquireFramebuffer, recording.CallKindPresent}; !reflect.DeepEqual(surfaceCalls, want) {
		t.Errorf("the frame after the recovery made %v for %v, want its surface configured again, then acquired and presented", surfaceCalls, window)
	}
}

func TestAFrameLostMidwayIsDrawnAgainAfterTheRecovery(t *testing.T) {
	h := newHarness(t)
	var buffer gfx.Buffer
	h.readFrame = func(event gfx.DrawEvent, device gfx.Device) {
		encoder, err := device.Encoder(event.Frame, 0)
		if err == nil {
			encoder.ClearBuffer(buffer, 0, 4)
		}
	}
	h.mustSpawn(nil)
	h.openWindow()
	h.waitForCalls(recording.CallKindCreateSurface, 1)
	h.readDevice(func(device gfx.Device) { buffer = createReadbackBuffer(t, device, 256) })
	h.instance.AnswerNext(recording.CallKindFinish, recording.Answer{Error: gfx.ErrDeviceLost})
	h.advance(time.Millisecond)
	if logged := h.takeLogged(); len(logged) != 1 || !errors.Is(logged[0], gfx.ErrDeviceLost) {
		t.Fatalf("logged %v, want the loss once", logged)
	}
	h.advance(time.Millisecond)
	callCount := len(h.instance.Recording().Calls)

	h.advance(time.Millisecond)

	calls := h.instance.Recording().Calls[callCount:]
	for _, call := range calls {
		if call.Error != nil {
			t.Errorf("the frame after the recovery made %v, answered %v", call.Kind, call.Error)
		}
	}
	for _, kind := range []recording.CallKind{recording.CallKindConfigureSurface, recording.CallKindAcquireFramebuffer, recording.CallKindSubmit, recording.CallKindPresent} {
		if !slices.Contains(kinds(calls), kind) {
			t.Errorf("the frame after the recovery made %v, want a %v", kinds(calls), kind)
		}
	}
	if drawn := h.drawEvents(); len(drawn) != 2 {
		t.Errorf("%d DrawEvents published, want the lost frame's and the one after the recovery", len(drawn))
	}
}

func TestARecoveryWhoseAdapterRefusesADeviceRequestsOneFromAFreshAdapter(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil)
	loseDevice(h)
	refusal := errors.New("vulkan: no device")
	h.instance.AnswerNext(recording.CallKindRequestDevice, recording.Answer{Error: refusal})
	callCount := len(h.instance.Recording().Calls)

	h.advance(time.Millisecond)

	request := gfx.DeviceDescriptor{RequiredLimits: gfx.DefaultLimits()}
	want := []recording.Call{
		{Kind: recording.CallKindReleaseDevice},
		{Kind: recording.CallKindRequestDevice, Arguments: request, Error: refusal},
		{Kind: recording.CallKindReleaseAdapter},
		{Kind: recording.CallKindRequestAdapter, Arguments: gfx.RequestAdapterOptions{}},
		{Kind: recording.CallKindRequestDevice, Arguments: request},
	}
	if calls := h.instance.Recording().Calls[callCount:]; !reflect.DeepEqual(calls, want) {
		t.Errorf("the recovery made calls %v, want %v", calls, want)
	}
	if recovered := h.recoveredEvents(); recovered != 1 {
		t.Errorf("DeviceRecoveredEvent published %d times, want once", recovered)
	}
}

func TestARecoveryThatFailsIsLoggedAndLeavesTheDeviceLost(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil)
	var buffer gfx.Buffer
	h.readDevice(func(device gfx.Device) { buffer = createReadbackBuffer(t, device, 256) })
	loseDevice(h)
	refusal := errors.New("vulkan: no device")
	h.instance.AnswerNext(recording.CallKindRequestDevice, recording.Answer{Error: refusal})
	h.instance.AnswerNext(recording.CallKindRequestDevice, recording.Answer{Error: refusal})

	h.advance(time.Millisecond)

	if logged := h.takeLogged(); len(logged) != 1 || !errors.Is(logged[0], refusal) {
		t.Errorf("logged %v, want the failed recovery once", logged)
	}
	if recovered := h.recoveredEvents(); recovered != 0 {
		t.Errorf("DeviceRecoveredEvent published %d times, want none", recovered)
	}
	callCount := len(h.instance.Recording().Calls)
	h.readDevice(func(device gfx.Device) {
		if _, err := device.CreateBuffer(gfx.BufferDescriptor{Size: 4, Usage: gfx.BufferUsageFlagCopyDst}); !errors.Is(err, gfx.ErrDeviceLost) {
			t.Errorf("CreateBuffer answered %v, want ErrDeviceLost", err)
		}
		device.Poll()
		device.ReleaseBuffer(buffer)
	})
	if calls := h.instance.Recording().Calls[callCount:]; len(calls) != 0 {
		t.Errorf("calls %v reached the backend after the failed recovery, want none", kinds(calls))
	}
}

// recreationOrder is the order a recovery creates the objects of each type
// again in: what an object names comes before it.
var recreationOrder = []recording.CallKind{
	recording.CallKindCreateBuffer,
	recording.CallKindCreateTexture,
	recording.CallKindCreateSampler,
	recording.CallKindCreateShaderModule,
	recording.CallKindCreateTextureView,
	recording.CallKindCreateBindGroupLayout,
	recording.CallKindCreatePipelineLayout,
	recording.CallKindCreateRenderPipeline,
	recording.CallKindCreateComputePipeline,
	recording.CallKindCreateBindGroup,
}

// loseDevice loses h's device through a CreateBuffer under the read lock,
// and takes the loss logged.
func loseDevice(h *harness) {
	h.t.Helper()
	h.instance.AnswerNext(recording.CallKindCreateBuffer, recording.Answer{Error: gfx.ErrDeviceLost})
	h.readDevice(func(device gfx.Device) {
		_, _ = device.CreateBuffer(gfx.BufferDescriptor{Size: 4, Usage: gfx.BufferUsageFlagCopyDst})
	})
	if logged := h.takeLogged(); len(logged) != 1 || !errors.Is(logged[0], gfx.ErrDeviceLost) {
		h.t.Fatalf("logged %v, want the loss once", logged)
	}
}

func TestBeatsHandledWhileARecoveryWaitsRecoverOnce(t *testing.T) {
	h := newHarness(t)
	h.render.MaxInFlight = 2
	h.mustSpawn(nil)
	loseDevice(h)
	started := make(chan struct{})
	release := make(chan struct{})
	h.read = func(gfx.Device) {
		close(started)
		<-release
	}
	_, held := h.kernel.Publish(readEvent{})
	<-started
	callCount := len(h.instance.Recording().Calls)

	h.clock.Jump(time.Millisecond)
	h.waitForRenders(1)
	h.clock.Jump(time.Millisecond)
	h.waitForRenders(2)
	close(release)
	h.wait(held)
	h.settle()

	var releases int
	for _, call := range h.instance.Recording().Calls[callCount:] {
		if call.Kind == recording.CallKindReleaseDevice {
			releases++
		}
	}
	if releases != 1 {
		t.Errorf("the device released %d times, want once: the second beat finds it recovered", releases)
	}
	if recovered := h.recoveredEvents(); recovered != 1 {
		t.Errorf("DeviceRecoveredEvent published %d times, want once", recovered)
	}
}

func TestAFailedRecoveryIsRetriedAtMostOnceASecondAndLoggedOnce(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil)
	loseDevice(h)
	refusal := errors.New("vulkan: no device")
	for range 4 {
		h.instance.AnswerNext(recording.CallKindRequestDevice, recording.Answer{Error: refusal})
	}

	for _, step := range []struct {
		name     string
		advance  time.Duration
		requests int
		logged   int
	}{
		{"the beat after the loss", time.Millisecond, 2, 1},
		{"a beat under a second later", time.Second - time.Millisecond, 0, 0},
		{"a beat a second later", time.Millisecond, 2, 0},
		{"a beat another second later", time.Second, 1, 0},
	} {
		callCount := len(h.instance.Recording().Calls)
		h.advance(step.advance)
		var requests int
		for _, call := range h.instance.Recording().Calls[callCount:] {
			if call.Kind == recording.CallKindRequestDevice {
				requests++
			}
		}
		if requests != step.requests {
			t.Errorf("%s requested %d devices, want %d", step.name, requests, step.requests)
		}
		if logged := h.takeLogged(); len(logged) != step.logged || (step.logged > 0 && !errors.Is(logged[0], refusal)) {
			t.Errorf("%s logged %v, want the failed recovery %d times", step.name, logged, step.logged)
		}
	}
	if recovered := h.recoveredEvents(); recovered != 1 {
		t.Errorf("DeviceRecoveredEvent published %d times, want once, by the third attempt", recovered)
	}
}

func TestARecoveryFailingAfterAnEarlierRecoveryIsLoggedAgain(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil)
	refusal := errors.New("vulkan: no device")
	loseDevice(h)
	h.instance.AnswerNext(recording.CallKindRequestDevice, recording.Answer{Error: refusal})
	h.instance.AnswerNext(recording.CallKindRequestDevice, recording.Answer{Error: refusal})
	h.advance(time.Millisecond)
	_ = h.takeLogged()
	h.advance(time.Second)
	loseDevice(h)
	h.instance.AnswerNext(recording.CallKindRequestDevice, recording.Answer{Error: refusal})
	h.instance.AnswerNext(recording.CallKindRequestDevice, recording.Answer{Error: refusal})

	h.advance(time.Second)

	if logged := h.takeLogged(); len(logged) != 1 || !errors.Is(logged[0], refusal) {
		t.Errorf("logged %v, want the second loss's failed recovery once", logged)
	}
}

func TestAMapPendingAtALossPublishesMapStatusFailedUnlessItsBufferWasReleased(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil)
	var buffer gfx.Buffer
	h.readDevice(func(device gfx.Device) {
		buffer = createReadbackBuffer(t, device, 4)
		mustSucceed(t, device.MapBuffer(buffer, gfx.MapModeRead, 0, 4))
		released := createReadbackBuffer(t, device, 4)
		mustSucceed(t, device.MapBuffer(released, gfx.MapModeRead, 0, 4))
		device.ReleaseBuffer(released)
	})

	loseDevice(h)

	want := []gfx.BufferMappedEvent{{Buffer: buffer, Status: gfx.MapStatusFailed}}
	if mapped := h.mappedEvents(); !reflect.DeepEqual(mapped, want) {
		t.Errorf("published %+v at the loss, want %+v", mapped, want)
	}
}

func TestMapsAndCapturesPendingWhenAPollFindsALossHearAsFailures(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(&gfx.Config{Surface: gfx.SurfaceRequest{Capturable: true}})
	window := h.openWindow()
	h.waitForCalls(recording.CallKindCreateSurface, 1)
	h.advance(time.Millisecond)
	var buffer gfx.Buffer
	h.readDevice(func(device gfx.Device) {
		buffer = createReadbackBuffer(t, device, 4)
		mustSucceed(t, device.MapBuffer(buffer, gfx.MapModeRead, 0, 4))
		mustSucceed(t, device.RequestCapture(window))
	})
	h.instance.AnswerNext(recording.CallKindMapStatus, recording.Answer{Error: gfx.ErrDeviceLost})

	h.advance(time.Millisecond)

	if logged := h.takeLogged(); len(logged) != 1 || !errors.Is(logged[0], gfx.ErrDeviceLost) {
		t.Errorf("logged %v, want the loss once", logged)
	}
	want := []gfx.BufferMappedEvent{{Buffer: buffer, Status: gfx.MapStatusFailed}}
	if mapped := h.mappedEvents(); !reflect.DeepEqual(mapped, want) {
		t.Errorf("published %+v, want %+v", mapped, want)
	}
	captured := h.capturedEvents()
	if len(captured) != 1 || captured[0].Status != gfx.CaptureStatusFailed || captured[0].Buffer != (gfx.Buffer{}) || captured[0].Window != window {
		t.Errorf("published %+v, want one CaptureStatusFailed of %v with no buffer", captured, window)
	}
}
