package gfx

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/platform"
)

// PresentMode is how a surface hands its framebuffers to the display,
// numbered as gputypes numbers it.
type PresentMode uint32

const (
	// PresentModeUndefined is the zero mode, which a SurfaceRequest reads as
	// PresentModeFifo.
	PresentModeUndefined PresentMode = 0x0
	// PresentModeFifo waits for the display: the acquire blocks, which paces
	// metronome's vsync preset. Every backend has it.
	PresentModeFifo PresentMode = 0x1
	// PresentModeFifoRelaxed is Fifo that tears when a frame is late; on
	// Metal it turns vsync off.
	PresentModeFifoRelaxed PresentMode = 0x2
	// PresentModeImmediate presents at once and may tear; for metronome's
	// limit and uncapped presets.
	PresentModeImmediate PresentMode = 0x3
	// PresentModeMailbox replaces the queued framebuffer and does not tear;
	// on Metal it is Immediate.
	PresentModeMailbox PresentMode = 0x4
)

// CompositeAlphaMode is how the compositor blends a surface's alpha with
// what lies behind the window, numbered as gputypes numbers it. gfx
// configures every surface Opaque until platform's window request can ask for
// a transparent window.
type CompositeAlphaMode uint32

const (
	CompositeAlphaModeAuto            CompositeAlphaMode = 0x0
	CompositeAlphaModeOpaque          CompositeAlphaMode = 0x1
	CompositeAlphaModePremultiplied   CompositeAlphaMode = 0x2
	CompositeAlphaModeUnpremultiplied CompositeAlphaMode = 0x3
	CompositeAlphaModeInherit         CompositeAlphaMode = 0x4
)

// SurfaceRequest is what the application asks every window's surface to be
// configured with: Config.Surface at start, ExclusiveDevice.RequestSurface
// after. A part the surface cannot honour falls back, with a LogErr, and
// Device.SurfaceConfiguration answers what each window got.
type SurfaceRequest struct {
	// Format is the framebuffer's format; zero is TextureFormatBGRA8Unorm,
	// which every backend accepts, leaving sRGB encoding to the last pass.
	// TextureFormatBGRA8UnormSrgb is honoured where the backend's surfaces
	// offer it; gogpu's offer BGRA8Unorm alone.
	Format TextureFormat
	// PresentMode is the present mode; zero is PresentModeFifo, the fallback
	// when the surface lacks the one asked for. gogpu's surfaces offer Fifo
	// alone.
	PresentMode PresentMode
}

// SurfaceConfiguration is what one window's surface is configured with.
type SurfaceConfiguration struct {
	Format        TextureFormat
	PresentMode   PresentMode
	Width, Height uint32
}

// SurfaceCapabilities is what one window's surface can be configured with,
// as the backend reports it.
type SurfaceCapabilities struct {
	Formats      []TextureFormat
	PresentModes []PresentMode
	AlphaModes   []CompositeAlphaMode
}

// AcquireStatus is what acquiring a window's framebuffer answered, mapped by
// the backend from its own errors so gfx handles every backend alike.
type AcquireStatus uint8

const (
	// AcquireStatusAcquired is a framebuffer to draw.
	AcquireStatusAcquired AcquireStatus = iota
	// AcquireStatusSuboptimal is a framebuffer to draw, with the surface to
	// be configured again before the next frame.
	AcquireStatusSuboptimal
	// AcquireStatusOutdated is no framebuffer: the window draws nothing this
	// frame, and its surface is configured again before the next.
	AcquireStatusOutdated
	// AcquireStatusLost is no framebuffer: the surface is recreated from the
	// window's native handle and configured again.
	AcquireStatusLost
	// AcquireStatusTimeout is no framebuffer this frame, and nothing else.
	AcquireStatusTimeout
)

// createSurface creates window's surface from its native handle, under the
// write lock, and marks it for configuring; a failure goes to LogErr, and
// window stays without a surface.
func createSurface(k *kernel.Kernel, window platform.Window, manager kernel.Read[*platform.WindowManager], deviceHandle kernel.Write[Device]) kernel.NoResult {
	d := deviceHandle.Get().(*device)
	if err := d.instance.CreateSurface(window, manager.Get().Native(window)); err != nil {
		k.Logger().LogErr(fmt.Errorf("gfx: creating the surface of %v: %w", window, err))
		return kernel.NoResult{}
	}
	d.surfaces = append(d.surfaces, window)
	d.frames.markUnconfigured(window)
	return kernel.NoResult{}
}

// releaseSurface releases window's surface, under the write lock, after the
// last present when its window closed, and drops its closing and configuring
// marks, its lost mark and its configuration; a window without one, its creation failed,
// releases nothing.
func releaseSurface(k *kernel.Kernel, window platform.Window, deviceHandle kernel.Write[Device]) kernel.NoResult {
	d := deviceHandle.Get().(*device)
	d.frames.unmarkClosing(window)
	d.frames.unmarkUnconfigured(window)
	d.frames.unmarkLost(window)
	delete(d.configurations, window)
	index := slices.Index(d.surfaces, window)
	if index < 0 {
		return kernel.NoResult{}
	}
	d.surfaces = slices.Delete(d.surfaces, index, index+1)
	d.instance.ReleaseSurface(window)
	return kernel.NoResult{}
}

// stopEvent is published by Stop, for gfx's handler to schedule the
// release of the surfaces still open, and then of the device.
type stopEvent struct{}

// scheduleRelease handles stopEvent: it schedules Tasks.ReleaseOpenSurfaces
// after in and the last present, then Tasks.ReleaseDevice, keeps the
// device's release as the last present, so a frame published after it
// acquires nothing, and returns it.
func (p *Plugin) scheduleRelease(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event stopEvent) (stopEvent, kernel.JobHandle) {
	surfacesReleased := ex.Schedule(p.tasks.ReleaseOpenSurfaces, kernel.NoArg{}, in, p.device.frames.lastPresentJob()).Handle()
	deviceReleased := ex.Schedule(p.tasks.ReleaseDevice, kernel.NoArg{}, surfacesReleased).Handle()
	p.device.frames.setLastPresent(deviceReleased)
	return event, deviceReleased
}

// releaseOpenSurfaces releases the surface of every window still open, as
// releaseSurface releases a closed window's, under the write lock.
func releaseOpenSurfaces(k *kernel.Kernel, _ kernel.NoArg, deviceHandle kernel.Write[Device]) kernel.NoResult {
	d := deviceHandle.Get().(*device)
	for _, window := range slices.Clone(d.surfaces) {
		releaseSurface(k, window, deviceHandle)
	}
	return kernel.NoResult{}
}

// configureSurfaces configures each surface marked for it from the surface
// request at its window's framebuffer size, under the write lock, between the
// last present and the next acquire. A part of the request the surface's
// capabilities lack falls back to the default, with a LogErr; a configure
// that fails goes to LogErr and leaves the window unconfigured, so it is not
// acquired. A window closing, or whose surface is gone, is skipped. A
// surface an acquire answered lost is released and created again from its
// window's native handle first; a creation that fails goes to LogErr, and
// the window stays without a surface. A window hidden, or whose framebuffer
// is zero, is skipped and stays marked, to be configured once it is drawable.
// A lost device configures nothing, and its surfaces stay marked.
func configureSurfaces(k *kernel.Kernel, _ kernel.NoArg, manager kernel.Read[*platform.WindowManager], deviceHandle kernel.Write[Device]) kernel.NoResult {
	d := deviceHandle.Get().(*device)
	if d.lost.Load() {
		return kernel.NoResult{}
	}
	for _, window := range d.frames.takeUnconfigured() {
		if !slices.Contains(d.surfaces, window) || d.frames.isClosing(window) {
			continue
		}
		if d.frames.unmarkLost(window) {
			delete(d.configurations, window)
			d.instance.ReleaseSurface(window)
			if err := d.instance.CreateSurface(window, manager.Get().Native(window)); err != nil {
				k.Logger().LogErr(fmt.Errorf("gfx: recreating the lost surface of %v: %w", window, err))
				d.surfaces = slices.DeleteFunc(d.surfaces, func(surface platform.Window) bool { return surface == window })
				continue
			}
		}
		if !isWindowDrawable(manager.Get(), window) {
			d.frames.markUnconfigured(window)
			continue
		}
		capabilities := d.backend.SurfaceCapabilities(window)
		format := cmp.Or(d.surfaceRequest.Format, TextureFormatBGRA8Unorm)
		if !slices.Contains(capabilities.Formats, format) {
			k.Logger().LogErr(fmt.Errorf("gfx: the surface of %v lacks format %v, falling back to BGRA8Unorm", window, format))
			format = TextureFormatBGRA8Unorm
		}
		presentMode := cmp.Or(d.surfaceRequest.PresentMode, PresentModeFifo)
		if !slices.Contains(capabilities.PresentModes, presentMode) {
			k.Logger().LogErr(fmt.Errorf("gfx: the surface of %v lacks present mode %v, falling back to Fifo", window, presentMode))
			presentMode = PresentModeFifo
		}
		size := manager.Get().FramebufferSize(window)
		configuration := SurfaceConfiguration{
			Format:      format,
			PresentMode: presentMode,
			Width:       uint32(size.Width),
			Height:      uint32(size.Height),
		}
		if err := d.backend.ConfigureSurface(window, configuration); err != nil {
			d.logErr(fmt.Errorf("gfx: configuring the surface of %v: %w", window, err))
			delete(d.configurations, window)
			continue
		}
		d.configurations[window] = configuration
	}
	return kernel.NoResult{}
}

// isWindowDrawable answers whether window is shown, as requested, with a
// framebuffer of at least one pixel: no other is configured or acquired.
func isWindowDrawable(manager *platform.WindowManager, window platform.Window) bool {
	entry, _ := manager.Entry(window)
	size := manager.FramebufferSize(window)
	return !entry.Hidden && size.Width > 0 && size.Height > 0
}
