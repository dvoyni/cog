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
			h.advance(time.Millisecond)

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

func TestOnceTheDeviceIsLostCallsWithErrorsAnswerErrDeviceLostReachingNoBackend(t *testing.T) {
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
