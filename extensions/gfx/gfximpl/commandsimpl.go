package gfximpl

import (
	"math"

	"github.com/dvoyni/cog/extensions/gfx"
	"github.com/dvoyni/cog/extensions/gfx/internal"
	"github.com/dvoyni/cog/kernel"
)

// presentCmdImpl swaps the recorded OpQueue into the ready slot (latest-wins).
func (p *Plugin) presentCmdImpl() (kernel.Lock, kernel.Execute[gfx.PresentRequest, gfx.PresentResponse]) {
	var write kernel.Write[*gfx.OpQueue]
	var ready kernel.Write[*readyList]
	return func(access kernel.ResourceAccess) {
			write = access.GetWrite[*gfx.OpQueue]()
			ready = access.GetWrite[*readyList]()
		}, func(kernel.Kernel, gfx.PresentRequest) (gfx.PresentResponse, error) {
			present(write, ready)
			return gfx.PresentResponse{}, nil
		}
}

// acquireCmdImpl advances the internal read queue, reporting whether it moved.
func (p *Plugin) acquireCmdImpl() (kernel.Lock, kernel.Execute[gfx.AcquireRequest, gfx.AcquireResponse]) {
	var read kernel.Write[*readList]
	var ready kernel.Write[*readyList]
	return func(access kernel.ResourceAccess) {
			read = access.GetWrite[*readList]()
			ready = access.GetWrite[*readyList]()
		}, func(kernel.Kernel, gfx.AcquireRequest) (gfx.AcquireResponse, error) {
			return gfx.AcquireResponse{Advanced: acquire(read, ready)}, nil
		}
}

func (p *Plugin) releaseCachedResourceCmdImpl() (kernel.Lock, kernel.Execute[gfx.ReleaseCachedResourceRequest, gfx.ReleaseCachedResourceResponse]) {
	var resources kernel.Write[*gfx.ResourceQueue]
	return func(access kernel.ResourceAccess) {
			resources = access.GetWrite[*gfx.ResourceQueue]()
		}, func(_ kernel.Kernel, request gfx.ReleaseCachedResourceRequest) (gfx.ReleaseCachedResourceResponse, error) {
			internal.ResourceQueueReleaseCachedResource(resources.Get(), request.Path)
			return gfx.ReleaseCachedResourceResponse{}, nil
		}
}

func (p *Plugin) freeCachedResourcesCmdImpl() (kernel.Lock, kernel.Execute[gfx.FreeCachedResourcesRequest, gfx.FreeCachedResourcesResponse]) {
	var resources kernel.Write[*gfx.ResourceQueue]
	return func(access kernel.ResourceAccess) {
			resources = access.GetWrite[*gfx.ResourceQueue]()
		}, func(kernel.Kernel, gfx.FreeCachedResourcesRequest) (gfx.FreeCachedResourcesResponse, error) {
			internal.ResourceQueueFreeCachedResources(resources.Get())
			return gfx.FreeCachedResourcesResponse{}, nil
		}
}

// armCaptureCmdImpl installs the frame's one capture request and hands back
// the channel its stills arrive on, plus the window size the caller cannot
// read for itself. The Viewport read is the only lock it needs: the capture
// slot is plugin-owned and carries its own.
func (p *Plugin) armCaptureCmdImpl() (kernel.Lock, kernel.Execute[gfx.ArmCaptureRequest, gfx.ArmCaptureResponse]) {
	var viewport kernel.Read[*gfx.Viewport]
	return func(access kernel.ResourceAccess) {
			viewport = access.GetRead[*gfx.Viewport]()
		}, func(_ kernel.Kernel, request gfx.ArmCaptureRequest) (gfx.ArmCaptureResponse, error) {
			live, err := p.captures.arm(request)
			if err != nil {
				return gfx.ArmCaptureResponse{}, err
			}
			return gfx.ArmCaptureResponse{Done: live.done, Viewport: *viewport.Get()}, nil
		}
}

// armFrameCmdImpl installs the frame's one snapshot request and hands back
// the channel the result arrives on, plus the viewport the caller cannot read
// for itself. The Viewport read is the only lock it needs: the snapshot slot
// is plugin-owned and carries its own.
func (p *Plugin) armFrameCmdImpl() (kernel.Lock, kernel.Execute[gfx.ArmFrameRequest, gfx.ArmFrameResponse]) {
	var viewport kernel.Read[*gfx.Viewport]
	return func(access kernel.ResourceAccess) {
			viewport = access.GetRead[*gfx.Viewport]()
		}, func(_ kernel.Kernel, request gfx.ArmFrameRequest) (gfx.ArmFrameResponse, error) {
			live, err := p.snapshots.arm(request)
			if err != nil {
				return gfx.ArmFrameResponse{}, err
			}
			return gfx.ArmFrameResponse{Done: live.done, Viewport: *viewport.Get()}, nil
		}
}

func setViewportCmdImpl() (kernel.Lock, kernel.Execute[gfx.SetViewportRequest, gfx.SetViewportResponse]) {
	var preference kernel.Read[*desiredViewport]
	var current kernel.Write[*gfx.Viewport]
	return func(access kernel.ResourceAccess) {
			preference = access.GetRead[*desiredViewport]()
			current = access.GetWrite[*gfx.Viewport]()
		}, func(_ kernel.Kernel, request gfx.SetViewportRequest) (gfx.SetViewportResponse, error) {
			viewport := resolveViewport(request.Width, request.Height, *preference.Get())
			viewport.FramebufferWidth = request.FramebufferWidth
			viewport.FramebufferHeight = request.FramebufferHeight
			*current.Get() = viewport
			return gfx.SetViewportResponse{Viewport: viewport}, nil
		}
}

func setDesiredViewportCmdImpl() (kernel.Lock, kernel.Execute[gfx.SetDesiredViewportRequest, gfx.SetDesiredViewportResponse]) {
	var stored kernel.Write[*desiredViewport]
	var current kernel.Write[*gfx.Viewport]
	return func(access kernel.ResourceAccess) {
			stored = access.GetWrite[*desiredViewport]()
			current = access.GetWrite[*gfx.Viewport]()
		}, func(_ kernel.Kernel, request gfx.SetDesiredViewportRequest) (gfx.SetDesiredViewportResponse, error) {
			preference := desiredViewport{
				mode: request.Mode, width: request.Width, height: request.Height, size: request.Size,
			}
			valid := request.Mode == gfx.ViewportWindow ||
				((request.Mode == gfx.ViewportFixedWidth || request.Mode == gfx.ViewportFixedHeight) && request.Size > 0) ||
				((request.Mode == gfx.ViewportFit || request.Mode == gfx.ViewportCover) && request.Width > 0 && request.Height > 0)
			if !valid {
				preference = desiredViewport{}
			}
			*stored.Get() = preference
			viewport := resolveViewport(current.Get().WindowWidth, current.Get().WindowHeight, preference)
			viewport.FramebufferWidth = current.Get().FramebufferWidth
			viewport.FramebufferHeight = current.Get().FramebufferHeight
			*current.Get() = viewport
			return gfx.SetDesiredViewportResponse{Viewport: viewport}, nil
		}
}

func resolveViewport(windowWidth, windowHeight float32, preference desiredViewport) gfx.Viewport {
	viewport := gfx.Viewport{
		Width: windowWidth, Height: windowHeight,
		WindowWidth: windowWidth, WindowHeight: windowHeight,
	}
	if windowWidth <= 0 || windowHeight <= 0 {
		viewport.Width, viewport.Height = 0, 0
		return viewport
	}
	switch preference.mode {
	case gfx.ViewportFixedWidth:
		viewport.Width = preference.size
		viewport.Height = float32(math.Round(float64(preference.size * windowHeight / windowWidth)))
	case gfx.ViewportFixedHeight:
		viewport.Height = preference.size
		viewport.Width = float32(math.Round(float64(preference.size * windowWidth / windowHeight)))
	case gfx.ViewportFit:
		if windowWidth/windowHeight >= preference.width/preference.height {
			viewport.Height = preference.height
			viewport.Width = float32(math.Round(float64(preference.height * windowWidth / windowHeight)))
		} else {
			viewport.Width = preference.width
			viewport.Height = float32(math.Round(float64(preference.width * windowHeight / windowWidth)))
		}
	case gfx.ViewportCover:
		if windowWidth/windowHeight >= preference.width/preference.height {
			viewport.Width = preference.width
			viewport.Height = float32(math.Round(float64(preference.width * windowHeight / windowWidth)))
		} else {
			viewport.Height = preference.height
			viewport.Width = float32(math.Round(float64(preference.height * windowWidth / windowHeight)))
		}
	}
	return viewport
}
