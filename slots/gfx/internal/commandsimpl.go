package internal

import (
	"math"

	"github.com/dvoyni/cog/slots/gfx/internal/types"

	"github.com/dvoyni/cog/kernel"
)

// presentCmdImpl swaps the recorded OpQueue into the ready slot (latest-wins).
func (p *plugin) presentCmdImpl() (kernel.Lock, kernel.Execute[PresentRequest, PresentResponse]) {
	var write kernel.Write[*OpQueue]
	var ready kernel.Write[*readyList]
	return func(access kernel.ResourceAccess) {
			write = access.GetWrite[*OpQueue]()
			ready = access.GetWrite[*readyList]()
		}, func(kernel.Kernel, PresentRequest) PresentResponse {
			present(write, ready)
			return PresentResponse{}
		}
}

// acquireCmdImpl advances the internal read queue, reporting whether it moved.
func (p *plugin) acquireCmdImpl() (kernel.Lock, kernel.Execute[AcquireRequest, AcquireResponse]) {
	var read kernel.Write[*readList]
	var ready kernel.Write[*readyList]
	return func(access kernel.ResourceAccess) {
			read = access.GetWrite[*readList]()
			ready = access.GetWrite[*readyList]()
		}, func(kernel.Kernel, AcquireRequest) AcquireResponse {
			return AcquireResponse{Advanced: acquire(read, ready)}
		}
}

func (p *plugin) releaseCachedResourceCmdImpl() (kernel.Lock, kernel.Execute[ReleaseCachedResourceRequest, ReleaseCachedResourceResponse]) {
	var resources kernel.Write[*ResourceQueue]
	return func(access kernel.ResourceAccess) {
			resources = access.GetWrite[*ResourceQueue]()
		}, func(_ kernel.Kernel, request ReleaseCachedResourceRequest) ReleaseCachedResourceResponse {
			ResourceQueueReleaseCachedResource(resources.Get(), request.Path)
			return ReleaseCachedResourceResponse{}
		}
}

func (p *plugin) freeCachedResourcesCmdImpl() (kernel.Lock, kernel.Execute[FreeCachedResourcesRequest, FreeCachedResourcesResponse]) {
	var resources kernel.Write[*ResourceQueue]
	return func(access kernel.ResourceAccess) {
			resources = access.GetWrite[*ResourceQueue]()
		}, func(kernel.Kernel, FreeCachedResourcesRequest) FreeCachedResourcesResponse {
			ResourceQueueFreeCachedResources(resources.Get())
			return FreeCachedResourcesResponse{}
		}
}

// armCaptureCmdImpl installs the frame's one capture request and hands back
// the channel its stills arrive on, plus the window size the caller cannot
// read for itself. The Viewport read is the only lock it needs: the capture
// slot is plugin-owned and carries its own.
func (p *plugin) armCaptureCmdImpl() (kernel.Lock, kernel.Execute[ArmCaptureRequest, ArmCaptureResponse]) {
	var viewport kernel.Read[*types.Viewport]
	return func(access kernel.ResourceAccess) {
			viewport = access.GetRead[*types.Viewport]()
		}, func(_ kernel.Kernel, request ArmCaptureRequest) ArmCaptureResponse {
			live, err := p.captures.arm(request)
			if err != nil {
				return ArmCaptureResponse{Err: err}
			}
			return ArmCaptureResponse{Done: live.done, Viewport: *viewport.Get()}
		}
}

// armFrameCmdImpl installs the frame's one snapshot request and hands back
// the channel the result arrives on, plus the viewport the caller cannot read
// for itself. The Viewport read is the only lock it needs: the snapshot slot
// is plugin-owned and carries its own.
func (p *plugin) armFrameCmdImpl() (kernel.Lock, kernel.Execute[ArmFrameRequest, ArmFrameResponse]) {
	var viewport kernel.Read[*types.Viewport]
	return func(access kernel.ResourceAccess) {
			viewport = access.GetRead[*types.Viewport]()
		}, func(_ kernel.Kernel, request ArmFrameRequest) ArmFrameResponse {
			live, err := p.snapshots.arm(request)
			if err != nil {
				return ArmFrameResponse{Err: err}
			}
			return ArmFrameResponse{Done: live.done, Viewport: *viewport.Get()}
		}
}

func setViewportCmdImpl() (kernel.Lock, kernel.Execute[SetViewportRequest, SetViewportResponse]) {
	var preference kernel.Read[*desiredViewport]
	var current kernel.Write[*types.Viewport]
	return func(access kernel.ResourceAccess) {
			preference = access.GetRead[*desiredViewport]()
			current = access.GetWrite[*types.Viewport]()
		}, func(_ kernel.Kernel, request SetViewportRequest) SetViewportResponse {
			viewport := resolveViewport(request.Width, request.Height, *preference.Get())
			viewport.FramebufferWidth = request.FramebufferWidth
			viewport.FramebufferHeight = request.FramebufferHeight
			current.Set(&viewport)
			return SetViewportResponse{Viewport: viewport}
		}
}

func setDesiredViewportCmdImpl() (kernel.Lock, kernel.Execute[SetDesiredViewportRequest, SetDesiredViewportResponse]) {
	var stored kernel.Write[*desiredViewport]
	var current kernel.Write[*types.Viewport]
	return func(access kernel.ResourceAccess) {
			stored = access.GetWrite[*desiredViewport]()
			current = access.GetWrite[*types.Viewport]()
		}, func(_ kernel.Kernel, request SetDesiredViewportRequest) SetDesiredViewportResponse {
			preference := desiredViewport{
				mode: request.Mode, width: request.Width, height: request.Height, size: request.Size,
			}
			valid := request.Mode == types.ViewportWindow ||
				((request.Mode == types.ViewportFixedWidth || request.Mode == types.ViewportFixedHeight) && request.Size > 0) ||
				((request.Mode == types.ViewportFit || request.Mode == types.ViewportCover) && request.Width > 0 && request.Height > 0)
			if !valid {
				preference = desiredViewport{}
			}
			stored.Set(&preference)
			viewport := resolveViewport(current.Get().WindowWidth, current.Get().WindowHeight, preference)
			viewport.FramebufferWidth = current.Get().FramebufferWidth
			viewport.FramebufferHeight = current.Get().FramebufferHeight
			current.Set(&viewport)
			return SetDesiredViewportResponse{Viewport: viewport}
		}
}

func resolveViewport(windowWidth, windowHeight float32, preference desiredViewport) types.Viewport {
	viewport := types.Viewport{
		Width: windowWidth, Height: windowHeight,
		WindowWidth: windowWidth, WindowHeight: windowHeight,
	}
	if windowWidth <= 0 || windowHeight <= 0 {
		viewport.Width, viewport.Height = 0, 0
		return viewport
	}
	switch preference.mode {
	case types.ViewportFixedWidth:
		viewport.Width = preference.size
		viewport.Height = float32(math.Round(float64(preference.size * windowHeight / windowWidth)))
	case types.ViewportFixedHeight:
		viewport.Height = preference.size
		viewport.Width = float32(math.Round(float64(preference.size * windowWidth / windowHeight)))
	case types.ViewportFit:
		if windowWidth/windowHeight >= preference.width/preference.height {
			viewport.Height = preference.height
			viewport.Width = float32(math.Round(float64(preference.height * windowWidth / windowHeight)))
		} else {
			viewport.Width = preference.width
			viewport.Height = float32(math.Round(float64(preference.width * windowHeight / windowWidth)))
		}
	case types.ViewportCover:
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
