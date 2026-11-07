package gfx

import (
	"fmt"
	"image"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/metronome"
	"github.com/dvoyni/cog/platform"
)

// MapMode is what a buffer is mapped for, numbered as gputypes numbers it.
type MapMode uint32

const (
	// MapModeRead maps a MapRead buffer to read what the GPU wrote: readback.
	MapModeRead MapMode = 0x1
	// MapModeWrite maps a MapWrite buffer to write it; uploads go through
	// Device.WriteBuffer instead.
	MapModeWrite MapMode = 0x2
)

// MapStatus is how a buffer's map ended, carried by BufferMappedEvent.
type MapStatus uint8

const (
	// MapStatusMapped is a mapped buffer, for Device.MappedRange until
	// Device.UnmapBuffer.
	MapStatusMapped MapStatus = iota
	// MapStatusFailed is a map the backend refused: wrong usage, range or
	// state, or a lost device; the error went to LogErr.
	MapStatusFailed
)

// BufferMappedEvent is published, from the job that found it, once a map
// Device.MapBuffer started has resolved. gfx checks its pending maps after
// every submit, so a map resolves at the submit after the one it waits on.
type BufferMappedEvent struct {
	Buffer Buffer
	Status MapStatus
}

// CaptureStatus is how a capture ended, carried by CapturedEvent.
type CaptureStatus uint8

const (
	// CaptureStatusCaptured is a framebuffer copied into Buffer and mapped.
	CaptureStatusCaptured CaptureStatus = iota
	// CaptureStatusNotAcquired is no capture: the window acquired no
	// framebuffer that frame (hidden, outdated, timed out), and Buffer is
	// zero.
	CaptureStatusNotAcquired
	// CaptureStatusFailed is a copy or map the backend refused; the error
	// went to LogErr, and Buffer is zero.
	CaptureStatusFailed
)

// CapturedEvent is published one frame after the frame it captures, from
// the job that found its map resolved. Buffer is gfx's staging buffer,
// mapped; gfx unmaps and releases it after the publication's out
// dependency, so a handler reads it in tasks it joins, with ReadCapture.
type CapturedEvent struct {
	Window platform.Window
	// Beat is the captured frame's: Tick and InterpolatedTick are the moment
	// it shows.
	Beat          metronome.Beat
	Buffer        Buffer
	Width, Height uint32
	Format        TextureFormat
	// BytesPerRow is the padded row length in Buffer, a multiple of 256.
	BytesPerRow uint32
	Status      CaptureStatus
}

// ReadCapture answers event's framebuffer as straight-alpha pixels: the rows
// un-strided, BGRA swapped to RGBA and alpha set opaque, with no colour
// conversion, since the bytes are what the window displayed. A task joined
// into the CapturedEvent publication calls it under kernel.Read[Device]. An
// event whose Status is not CaptureStatusCaptured answers an error.
func ReadCapture(device Device, event CapturedEvent) (*image.NRGBA, error) {
	if event.Status != CaptureStatusCaptured {
		return nil, fmt.Errorf("gfx: reading a capture of %v that holds no picture, status %d", event.Window, event.Status)
	}
	mapped, err := device.MappedRange(event.Buffer, 0, uint64(event.BytesPerRow)*uint64(event.Height))
	if err != nil {
		return nil, fmt.Errorf("gfx: reading a capture of %v: %w", event.Window, err)
	}
	swapped := event.Format == TextureFormatBGRA8Unorm || event.Format == TextureFormatBGRA8UnormSrgb
	picture := image.NewNRGBA(image.Rect(0, 0, int(event.Width), int(event.Height)))
	for y := range int(event.Height) {
		row := mapped[y*int(event.BytesPerRow) : y*int(event.BytesPerRow)+int(event.Width)*4]
		pixels := picture.Pix[y*picture.Stride : y*picture.Stride+int(event.Width)*4]
		copy(pixels, row)
		for x := 0; x < len(pixels); x += 4 {
			if swapped {
				pixels[x] = row[x+2]
				pixels[x+2] = row[x]
			}
			pixels[x+3] = 0xFF
		}
	}
	return picture, nil
}

// captureDeliveryEvent carries a capture resolved, or ended without one, from
// the job that found it to gfx's handler, which publishes it as
// CapturedEvent in its own batch, to release its buffer after.
type captureDeliveryEvent struct {
	captured CapturedEvent
}

// publishCapture handles captureDeliveryEvent: it publishes the
// CapturedEvent after in and, when it has a buffer, schedules its unmap and
// release after the publication's out dependency, which it returns.
func (p *Plugin) publishCapture(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event captureDeliveryEvent) (captureDeliveryEvent, kernel.JobHandle) {
	_, out := ex.Publish(in, event.captured)
	if event.captured.Buffer == (Buffer{}) {
		return event, out
	}
	return event, ex.Schedule(p.releaseCaptureTask, event.captured.Buffer, out).Handle()
}

// releaseCapture unmaps and releases a capture's buffer, under the read
// lock; an unmap that fails goes to LogErr, but for a device loss, logged
// once already.
func releaseCapture(k *kernel.Kernel, buffer Buffer, deviceHandle kernel.Read[Device]) kernel.NoResult {
	d := deviceHandle.Get().(*device)
	if err := d.UnmapBuffer(buffer); err != nil {
		d.logErr(fmt.Errorf("gfx: unmapping a capture's buffer: %w", err))
	}
	d.ReleaseBuffer(buffer)
	return kernel.NoResult{}
}
