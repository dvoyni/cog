package gfx_test

import (
	"errors"
	"image"
	"image/color"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gfx/recording"
	"github.com/dvoyni/cog/platform"
)

func TestACapturableRequestConfiguresEverySurfaceCapturable(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(&gfx.Config{Surface: gfx.SurfaceRequest{Capturable: true}})
	window := h.openWindow()
	h.waitForCalls(recording.CallKindCreateSurface, 1)

	h.advance(time.Millisecond)

	want := gfx.SurfaceConfiguration{Format: gfx.TextureFormatBGRA8Unorm, PresentMode: gfx.PresentModeFifo, Width: 64, Height: 48, Capturable: true}
	if configured := h.waitForCalls(recording.CallKindConfigureSurface, 1)[0]; configured.Arguments != (recording.ConfigureSurfaceArguments{Window: window, Configuration: want}) {
		t.Errorf("configured %+v, want %v with %+v", configured.Arguments, window, want)
	}
	h.readDevice(func(device gfx.Device) {
		if got, ok := device.SurfaceConfiguration(window); !ok || got != want {
			t.Errorf("SurfaceConfiguration = %+v, %v, want %+v", got, ok, want)
		}
	})
}

func TestAFramebuffersTextureIsDescribedByItsConfigurationUntilThePresent(t *testing.T) {
	h := newHarness(t)
	var mutex sync.Mutex
	var texture gfx.Texture
	var described gfx.TextureDescriptor
	var window platform.Window
	h.readFrame = func(event gfx.DrawEvent, device gfx.Device) {
		mutex.Lock()
		defer mutex.Unlock()
		texture = device.TextureViewTexture(device.Framebuffer(event.Frame, window))
		described = device.TextureDescriptor(texture)
	}
	h.mustSpawn(&gfx.Config{Surface: gfx.SurfaceRequest{Capturable: true}})
	window = h.openWindow()
	h.waitForCalls(recording.CallKindCreateSurface, 1)

	h.advance(time.Millisecond)

	mutex.Lock()
	defer mutex.Unlock()
	want := gfx.TextureDescriptor{
		Size:          gfx.Extent3D{Width: 64, Height: 48, DepthOrArrayLayers: 1},
		MipLevelCount: 1,
		SampleCount:   1,
		Dimension:     gfx.TextureDimension2D,
		Format:        gfx.TextureFormatBGRA8Unorm,
		Usage:         gfx.TextureUsageFlagRenderAttachment | gfx.TextureUsageFlagCopySrc,
	}
	if acquired := frameCalls(h.instance.Recording().Calls)[0].Arguments.(recording.AcquireFramebufferArguments); texture != acquired.Texture || !reflect.DeepEqual(described, want) {
		t.Errorf("the framebuffer's texture %v is described %+v, want %v described %+v", texture, described, acquired.Texture, want)
	}
	h.readDevice(func(device gfx.Device) {
		if got := device.TextureDescriptor(texture); !reflect.DeepEqual(got, gfx.TextureDescriptor{}) {
			t.Errorf("the presented framebuffer's texture is described %+v, want the zero descriptor", got)
		}
	})
}

func TestCapturableTheSurfaceLacksFallsBackWithALogErr(t *testing.T) {
	h := newHarness(t)
	capabilities := recording.SurfaceCapabilities
	capabilities.Capturable = false
	h.instance.AnswerNext(recording.CallKindSurfaceCapabilities, recording.Answer{Capabilities: capabilities})
	h.mustSpawn(&gfx.Config{Surface: gfx.SurfaceRequest{Capturable: true}})
	window := h.openWindow()
	h.waitForCalls(recording.CallKindCreateSurface, 1)

	h.advance(time.Millisecond)

	want := gfx.SurfaceConfiguration{Format: gfx.TextureFormatBGRA8Unorm, PresentMode: gfx.PresentModeFifo, Width: 64, Height: 48}
	if configured := h.waitForCalls(recording.CallKindConfigureSurface, 1)[0]; configured.Arguments != (recording.ConfigureSurfaceArguments{Window: window, Configuration: want}) {
		t.Errorf("configured %+v, want %v with %+v", configured.Arguments, window, want)
	}
	if logged := h.takeLogged(); len(logged) != 1 {
		t.Errorf("logged %v, want the fallback", logged)
	}
}

func TestAWindowThatCannotBeCapturedAnswersErrCaptureUnsupported(t *testing.T) {
	for _, test := range []struct {
		name    string
		request gfx.SurfaceRequest
		// configured is whether a frame configures the window before the
		// request.
		configured bool
	}{
		{"not capturable", gfx.SurfaceRequest{}, true},
		{"not 8-bit BGRA or RGBA", gfx.SurfaceRequest{Format: gfx.TextureFormatRGBA16Float, Capturable: true}, true},
		{"not yet configured", gfx.SurfaceRequest{Capturable: true}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t)
			capabilities := recording.SurfaceCapabilities
			capabilities.Formats = append(slices.Clone(capabilities.Formats), gfx.TextureFormatRGBA16Float)
			h.instance.AnswerNext(recording.CallKindSurfaceCapabilities, recording.Answer{Capabilities: capabilities})
			h.mustSpawn(&gfx.Config{Surface: test.request})
			window := h.openWindow()
			h.waitForCalls(recording.CallKindCreateSurface, 1)
			if test.configured {
				h.advance(time.Millisecond)
			}

			var err error
			h.readDevice(func(device gfx.Device) { err = device.RequestCapture(window) })

			if !errors.Is(err, gfx.ErrCaptureUnsupported) {
				t.Errorf("RequestCapture answered %v, want ErrCaptureUnsupported", err)
			}
		})
	}
}

func TestACaptureIsCopiedAfterEverySubmitOrderAndDeliveredWithItsFramesBeat(t *testing.T) {
	h := newHarness(t)
	var buffers []gfx.Buffer
	h.readFrame = func(event gfx.DrawEvent, device gfx.Device) {
		encoder, err := device.Encoder(event.Frame, 1000)
		if err != nil {
			t.Errorf("Encoder answered %v", err)
		}
		encoder.ClearBuffer(buffers[0], 0, 4)
	}
	var mutex sync.Mutex
	var read []byte
	var readErr error
	h.readCapture = func(event gfx.CapturedEvent, device gfx.Device) {
		mutex.Lock()
		defer mutex.Unlock()
		read, readErr = device.MappedRange(event.Buffer, 0, uint64(event.BytesPerRow*event.Height))
		read = slices.Clone(read)
	}
	h.mustSpawn(&gfx.Config{Surface: gfx.SurfaceRequest{Capturable: true}})
	window := h.openWindowOfSize(platform.Size{Width: 50, Height: 30})
	h.waitForCalls(recording.CallKindCreateSurface, 1)
	buffers = h.createBuffers(1)
	h.advance(time.Millisecond)
	h.readDevice(func(device gfx.Device) { mustSucceed(t, device.RequestCapture(window)) })

	h.advance(time.Millisecond)
	h.waitForCalls(recording.CallKindReleaseBuffer, 1)

	captured := h.capturedEvents()
	if len(captured) != 1 {
		t.Fatalf("published %+v, want one CapturedEvent", captured)
	}
	staging := captured[0].Buffer
	want := gfx.CapturedEvent{
		Window: window, Beat: h.drawEvents()[1].Beat, Buffer: staging,
		Width: 50, Height: 30, Format: gfx.TextureFormatBGRA8Unorm, BytesPerRow: 256,
		Status: gfx.CaptureStatusCaptured,
	}
	if staging == (gfx.Buffer{}) || captured[0] != want {
		t.Errorf("published %+v, want %+v with a buffer", captured[0], want)
	}
	if created := createdBuffer(h, staging); created.Size != 256*30 || created.Usage != gfx.BufferUsageFlagCopyDst|gfx.BufferUsageFlagMapRead {
		t.Errorf("created the capture's buffer with %+v, want 7680 bytes of CopyDst and MapRead", created)
	}
	frames := h.instance.Recording().Frames()
	if len(frames) != 2 || len(frames[1].Submissions) != 1 || !slices.Equal(frames[1].Presented, []platform.Window{window}) {
		t.Fatalf("Frames() = %+v, want the second frame's one submission before its present", frames)
	}
	acquired := frameCalls(h.instance.Recording().Calls)[2].Arguments.(recording.AcquireFramebufferArguments)
	copied := recording.CommandBuffer{Commands: []recording.Command{{
		Kind: recording.CommandKindCopyTextureToBuffer,
		Arguments: recording.CopyTextureToBufferArguments{Source: acquired.Texture, Destination: staging, Regions: []gfx.BufferTextureCopy{{
			BufferLayout: gfx.ImageDataLayout{BytesPerRow: 256, RowsPerImage: 30},
			TextureBase:  gfx.ImageCopyTexture{Texture: acquired.Texture},
			Size:         gfx.Extent3D{Width: 50, Height: 30, DepthOrArrayLayers: 1},
		}}},
	}}}
	wantSubmission := recording.Submission{Buffers: append(submissionOf(buffers[0]).Buffers, copied)}
	if acquired.Texture == (gfx.Texture{}) || !reflect.DeepEqual(frames[1].Submissions[0], wantSubmission) {
		t.Errorf("submitted %+v, want %+v", frames[1].Submissions[0], wantSubmission)
	}
	mutex.Lock()
	defer mutex.Unlock()
	if readErr != nil || len(read) != 256*30 || slices.ContainsFunc(read, func(b byte) bool { return b != 0 }) {
		t.Errorf("MappedRange answered %d bytes, %v, want 7680 zeros", len(read), readErr)
	}
}

func TestACapturesBufferIsUnmappedAndReleasedAfterThePublicationsOutDependency(t *testing.T) {
	h := newHarness(t)
	var mutex sync.Mutex
	var releasedEarly bool
	h.readCapture = func(event gfx.CapturedEvent, device gfx.Device) {
		deadline := time.Now().Add(20 * time.Millisecond)
		for time.Now().Before(deadline) {
			for _, call := range h.instance.Recording().Calls {
				if (call.Kind == recording.CallKindUnmapBuffer || call.Kind == recording.CallKindReleaseBuffer) && call.Arguments == event.Buffer {
					mutex.Lock()
					releasedEarly = true
					mutex.Unlock()
				}
			}
		}
	}
	h.mustSpawn(&gfx.Config{Surface: gfx.SurfaceRequest{Capturable: true}})
	window := h.openWindow()
	h.waitForCalls(recording.CallKindCreateSurface, 1)
	h.advance(time.Millisecond)
	h.readDevice(func(device gfx.Device) { mustSucceed(t, device.RequestCapture(window)) })

	h.advance(time.Millisecond)
	h.waitForCalls(recording.CallKindReleaseBuffer, 1)

	captured := h.capturedEvents()
	if len(captured) != 1 {
		t.Fatalf("published %+v, want one CapturedEvent", captured)
	}
	mutex.Lock()
	defer mutex.Unlock()
	if releasedEarly {
		t.Error("the capture's buffer was unmapped or released while a task joined into the publication ran")
	}
	if got, want := callsOf(h, captured[0].Buffer), []recording.CallKind{
		recording.CallKindCreateBuffer, recording.CallKindMapBuffer,
		recording.CallKindUnmapBuffer, recording.CallKindReleaseBuffer,
	}; !slices.Equal(got, want) {
		t.Errorf("the capture's buffer had calls %v, want %v", got, want)
	}
}

func TestASecondCaptureInFlightAnswersErrCaptureBusyUntilTheFirstIsDelivered(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(&gfx.Config{Surface: gfx.SurfaceRequest{Capturable: true}})
	window := h.openWindow()
	h.waitForCalls(recording.CallKindCreateSurface, 1)
	h.advance(time.Millisecond)

	h.readDevice(func(device gfx.Device) {
		mustSucceed(t, device.RequestCapture(window))
		if err := device.RequestCapture(window); !errors.Is(err, gfx.ErrCaptureBusy) {
			t.Errorf("a second RequestCapture answered %v, want ErrCaptureBusy", err)
		}
	})
	h.advance(time.Millisecond)
	h.readDevice(func(device gfx.Device) {
		if err := device.RequestCapture(window); err != nil {
			t.Errorf("RequestCapture after the delivery answered %v", err)
		}
	})
	h.advance(time.Millisecond)

	if captured := h.capturedEvents(); len(captured) != 2 {
		t.Errorf("published %+v, want two CapturedEvents", captured)
	}
}

func TestAWindowAcquiringNothingInTheFrameIsDeliveredNotAcquired(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(&gfx.Config{Surface: gfx.SurfaceRequest{Capturable: true}})
	window := h.openWindow()
	h.waitForCalls(recording.CallKindCreateSurface, 1)
	h.advance(time.Millisecond)
	h.readDevice(func(device gfx.Device) { mustSucceed(t, device.RequestCapture(window)) })
	h.instance.AnswerNext(recording.CallKindAcquireFramebuffer, recording.Answer{Status: uint8(gfx.AcquireStatusTimeout)})

	h.advance(time.Millisecond)

	want := []gfx.CapturedEvent{{Window: window, Beat: h.drawEvents()[1].Beat, Status: gfx.CaptureStatusNotAcquired}}
	if captured := h.capturedEvents(); !reflect.DeepEqual(captured, want) {
		t.Errorf("published %+v, want %+v", captured, want)
	}
	for _, call := range h.instance.Recording().Calls {
		if call.Kind == recording.CallKindCreateBuffer {
			t.Errorf("created a buffer, %+v, for a capture of nothing", call.Arguments)
		}
	}
}

func TestACaptureTheBackendFailsIsDeliveredFailedWithItsErrorInLogErr(t *testing.T) {
	for _, test := range []struct {
		name string
		kind recording.CallKind
	}{
		{"its copy's finish", recording.CallKindFinish},
		{"the frame's submit", recording.CallKindSubmit},
		{"its map", recording.CallKindMapBuffer},
		{"its map's status", recording.CallKindMapStatus},
	} {
		t.Run(test.name, func(t *testing.T) {
			failure := errors.New("refused")
			h := newHarness(t)
			h.mustSpawn(&gfx.Config{Surface: gfx.SurfaceRequest{Capturable: true}})
			window := h.openWindow()
			h.waitForCalls(recording.CallKindCreateSurface, 1)
			h.advance(time.Millisecond)
			h.readDevice(func(device gfx.Device) { mustSucceed(t, device.RequestCapture(window)) })
			h.instance.AnswerNext(test.kind, recording.Answer{Error: failure})

			h.advance(time.Millisecond)

			captured := h.capturedEvents()
			if len(captured) != 1 || captured[0].Status != gfx.CaptureStatusFailed || captured[0].Buffer != (gfx.Buffer{}) || captured[0].Window != window {
				t.Errorf("published %+v, want one CaptureStatusFailed of %v with no buffer", captured, window)
			}
			if logged := h.takeLogged(); len(logged) != 1 || !errors.Is(logged[0], failure) {
				t.Errorf("logged %v, want %v once", logged, failure)
			}
			var created []gfx.Buffer
			for _, call := range h.instance.Recording().Calls {
				if call.Kind == recording.CallKindCreateBuffer {
					created = append(created, call.Arguments.(recording.CreateBufferArguments).Buffer)
				}
			}
			if len(created) != 1 || callsOf(h, created[0])[len(callsOf(h, created[0]))-1] != recording.CallKindReleaseBuffer {
				t.Errorf("created buffers %v, want the capture's one released", created)
			}
		})
	}
}

// createdBuffer answers the descriptor buffer was created with, as h's
// instance recorded it.
func createdBuffer(h *harness, buffer gfx.Buffer) gfx.BufferDescriptor {
	for _, call := range h.instance.Recording().Calls {
		if arguments, ok := call.Arguments.(recording.CreateBufferArguments); ok && arguments.Buffer == buffer {
			return arguments.Descriptor
		}
	}
	return gfx.BufferDescriptor{}
}

// callsOf answers the kinds of the calls h's instance recorded naming
// buffer, in order.
func callsOf(h *harness, buffer gfx.Buffer) []recording.CallKind {
	var answer []recording.CallKind
	for _, call := range h.instance.Recording().Calls {
		switch arguments := call.Arguments.(type) {
		case gfx.Buffer:
			if arguments == buffer {
				answer = append(answer, call.Kind)
			}
		case recording.CreateBufferArguments:
			if arguments.Buffer == buffer {
				answer = append(answer, call.Kind)
			}
		case recording.MapBufferArguments:
			if arguments.Buffer == buffer {
				answer = append(answer, call.Kind)
			}
		}
	}
	return answer
}

func TestReadCaptureAnswersTheFramebufferUnstridedWithAlphaOpaqueAndBGRASwapped(t *testing.T) {
	for _, test := range []struct {
		name    string
		format  gfx.TextureFormat
		swapped bool
	}{
		{"BGRA8Unorm", gfx.TextureFormatBGRA8Unorm, true},
		{"BGRA8UnormSrgb", gfx.TextureFormatBGRA8UnormSrgb, true},
		{"RGBA8Unorm", gfx.TextureFormatRGBA8Unorm, false},
		{"RGBA8UnormSrgb", gfx.TextureFormatRGBA8UnormSrgb, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t)
			texel := func(x, y int) []byte { return []byte{byte(x), byte(10 + y), byte(20 + x + y), 0x40} }
			var mutex sync.Mutex
			var read *image.NRGBA
			var readErr error
			h.readCapture = func(event gfx.CapturedEvent, device gfx.Device) {
				// The recording's copy holds no texels, so the test writes what
				// the GPU would have, the row padding included.
				mapped, err := device.MappedRange(event.Buffer, 0, uint64(event.BytesPerRow*event.Height))
				if err != nil {
					t.Errorf("MappedRange answered %v", err)
					return
				}
				for i := range mapped {
					mapped[i] = 0xEE
				}
				for y := range int(event.Height) {
					for x := range int(event.Width) {
						copy(mapped[y*int(event.BytesPerRow)+x*4:], texel(x, y))
					}
				}
				mutex.Lock()
				defer mutex.Unlock()
				read, readErr = gfx.ReadCapture(device, event)
			}
			h.mustSpawn(&gfx.Config{Surface: gfx.SurfaceRequest{Format: test.format, Capturable: true}})
			window := h.openWindowOfSize(platform.Size{Width: 3, Height: 2})
			h.waitForCalls(recording.CallKindCreateSurface, 1)
			h.advance(time.Millisecond)
			h.readDevice(func(device gfx.Device) { mustSucceed(t, device.RequestCapture(window)) })

			h.advance(time.Millisecond)
			h.waitForCalls(recording.CallKindReleaseBuffer, 1)

			want := image.NewNRGBA(image.Rect(0, 0, 3, 2))
			for y := range 2 {
				for x := range 3 {
					written := texel(x, y)
					if test.swapped {
						want.SetNRGBA(x, y, color.NRGBA{R: written[2], G: written[1], B: written[0], A: 0xFF})
					} else {
						want.SetNRGBA(x, y, color.NRGBA{R: written[0], G: written[1], B: written[2], A: 0xFF})
					}
				}
			}
			mutex.Lock()
			defer mutex.Unlock()
			if readErr != nil || !reflect.DeepEqual(read, want) {
				t.Errorf("ReadCapture answered %+v, %v, want %+v", read, readErr, want)
			}
		})
	}
}

func TestReadCaptureOfAnEventWithNoPictureAnswersAnError(t *testing.T) {
	for _, test := range []struct {
		name   string
		status gfx.CaptureStatus
		mapped bool
	}{
		{"not acquired", gfx.CaptureStatusNotAcquired, true},
		{"failed", gfx.CaptureStatusFailed, true},
		{"captured into no mapped buffer", gfx.CaptureStatusCaptured, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t)
			h.mustSpawn(nil)

			h.readDevice(func(device gfx.Device) {
				event := gfx.CapturedEvent{Width: 3, Height: 2, Format: gfx.TextureFormatBGRA8Unorm, BytesPerRow: 256, Status: test.status}
				if test.mapped {
					// A buffer mapped and big enough, so only the status refuses it.
					event.Buffer = createReadbackBuffer(t, device, 512)
					mustSucceed(t, device.MapBuffer(event.Buffer, gfx.MapModeRead, 0, 512))
					device.Poll()
				}
				if read, err := gfx.ReadCapture(device, event); read != nil || err == nil {
					t.Errorf("ReadCapture answered %+v, %v, want an error", read, err)
				}
			})
		})
	}
}
