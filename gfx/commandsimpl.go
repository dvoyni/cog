package gfx

import (
	"math"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/kernel"
)

// presentCmd swaps the recorded OpQueue into the ready slot (latest-wins).
func (p *Plugin) presentCmd() (kernel.Lock, kernel.Execute[PresentRequest, PresentResponse]) {
	var write kernel.Write[*OpQueue]
	var ready kernel.Write[*readyList]
	return func(access kernel.ResourceAccess) {
			write = access.GetWrite[*OpQueue]()
			ready = access.GetWrite[*readyList]()
		}, func(kernel.Kernel, PresentRequest) (PresentResponse, error) {
			present(write, ready)
			return PresentResponse{}, nil
		}
}

// acquireCmd advances the internal read queue, reporting whether it moved.
func (p *Plugin) acquireCmd() (kernel.Lock, kernel.Execute[AcquireRequest, AcquireResponse]) {
	var read kernel.Write[*readList]
	var ready kernel.Write[*readyList]
	return func(access kernel.ResourceAccess) {
			read = access.GetWrite[*readList]()
			ready = access.GetWrite[*readyList]()
		}, func(kernel.Kernel, AcquireRequest) (AcquireResponse, error) {
			return AcquireResponse{Advanced: acquire(read, ready)}, nil
		}
}

// setBackendCmd installs the Backend the plugin renders through.
func (p *Plugin) setBackendCmd() (kernel.Lock, kernel.Execute[SetBackendRequest, SetBackendResponse]) {
	var write kernel.Write[*OpQueue]
	var read kernel.Write[*readList]
	var ready kernel.Write[*readyList]
	var resources kernel.Write[*ResourceQueue]
	return func(access kernel.ResourceAccess) {
			write = access.GetWrite[*OpQueue]()
			read = access.GetWrite[*readList]()
			ready = access.GetWrite[*readyList]()
			resources = access.GetWrite[*ResourceQueue]()
		}, func(_ kernel.Kernel, request SetBackendRequest) (SetBackendResponse, error) {
			write.Get().backend = request.Backend
			read.Get().backend = request.Backend
			ready.Get().queue.backend = request.Backend
			resources.Get().backend = request.Backend
			return SetBackendResponse{}, nil
		}
}

func (p *Plugin) releaseCachedResourceCmd() (kernel.Lock, kernel.Execute[ReleaseCachedResourceRequest, ReleaseCachedResourceResponse]) {
	var resources kernel.Write[*ResourceQueue]
	return func(access kernel.ResourceAccess) {
			resources = access.GetWrite[*ResourceQueue]()
		}, func(_ kernel.Kernel, request ReleaseCachedResourceRequest) (ReleaseCachedResourceResponse, error) {
			resources.Get().releaseCachedResource(request.Path)
			return ReleaseCachedResourceResponse{}, nil
		}
}

func (p *Plugin) freeCachedResourcesCmd() (kernel.Lock, kernel.Execute[FreeCachedResourcesRequest, FreeCachedResourcesResponse]) {
	var resources kernel.Write[*ResourceQueue]
	return func(access kernel.ResourceAccess) {
			resources = access.GetWrite[*ResourceQueue]()
		}, func(kernel.Kernel, FreeCachedResourcesRequest) (FreeCachedResourcesResponse, error) {
			resources.Get().freeCachedResources()
			return FreeCachedResourcesResponse{}, nil
		}
}

func setViewportCmd() (kernel.Lock, kernel.Execute[app.SetViewportRequest, app.SetViewportResponse]) {
	var preference kernel.Read[*desiredViewport]
	var current kernel.Write[*app.Viewport]
	return func(access kernel.ResourceAccess) {
			preference = access.GetRead[*desiredViewport]()
			current = access.GetWrite[*app.Viewport]()
		}, func(_ kernel.Kernel, request app.SetViewportRequest) (app.SetViewportResponse, error) {
			viewport := resolveViewport(request.Width, request.Height, *preference.Get())
			viewport.FramebufferWidth = request.FramebufferWidth
			viewport.FramebufferHeight = request.FramebufferHeight
			*current.Get() = viewport
			return app.SetViewportResponse{Viewport: viewport}, nil
		}
}

func setDesiredViewportCmd() (kernel.Lock, kernel.Execute[app.SetDesiredViewportRequest, app.SetDesiredViewportResponse]) {
	var stored kernel.Write[*desiredViewport]
	var current kernel.Write[*app.Viewport]
	return func(access kernel.ResourceAccess) {
			stored = access.GetWrite[*desiredViewport]()
			current = access.GetWrite[*app.Viewport]()
		}, func(_ kernel.Kernel, request app.SetDesiredViewportRequest) (app.SetDesiredViewportResponse, error) {
			preference := desiredViewport{
				mode: request.Mode, width: request.Width, height: request.Height, size: request.Size,
			}
			valid := request.Mode == app.ViewportWindow ||
				((request.Mode == app.ViewportFixedWidth || request.Mode == app.ViewportFixedHeight) && request.Size > 0) ||
				((request.Mode == app.ViewportFit || request.Mode == app.ViewportCover) && request.Width > 0 && request.Height > 0)
			if !valid {
				preference = desiredViewport{}
			}
			*stored.Get() = preference
			viewport := resolveViewport(current.Get().WindowWidth, current.Get().WindowHeight, preference)
			viewport.FramebufferWidth = current.Get().FramebufferWidth
			viewport.FramebufferHeight = current.Get().FramebufferHeight
			*current.Get() = viewport
			return app.SetDesiredViewportResponse{Viewport: viewport}, nil
		}
}

func resolveViewport(windowWidth, windowHeight float32, preference desiredViewport) app.Viewport {
	viewport := app.Viewport{
		Width: windowWidth, Height: windowHeight,
		WindowWidth: windowWidth, WindowHeight: windowHeight,
	}
	if windowWidth <= 0 || windowHeight <= 0 {
		viewport.Width, viewport.Height = 0, 0
		return viewport
	}
	switch preference.mode {
	case app.ViewportFixedWidth:
		viewport.Width = preference.size
		viewport.Height = float32(math.Round(float64(preference.size * windowHeight / windowWidth)))
	case app.ViewportFixedHeight:
		viewport.Height = preference.size
		viewport.Width = float32(math.Round(float64(preference.size * windowWidth / windowHeight)))
	case app.ViewportFit:
		if windowWidth/windowHeight >= preference.width/preference.height {
			viewport.Height = preference.height
			viewport.Width = float32(math.Round(float64(preference.height * windowWidth / windowHeight)))
		} else {
			viewport.Width = preference.width
			viewport.Height = float32(math.Round(float64(preference.width * windowHeight / windowWidth)))
		}
	case app.ViewportCover:
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
