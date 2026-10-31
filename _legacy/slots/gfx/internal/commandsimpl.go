package internal

import (
	"math"

	"github.com/dvoyni/cog/slots/gfx/internal/shader"
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

// compileShaderCmdImpl compiles a descriptor through the bound Backend's
// reflection port. It declares no lock: the adapter is bound at composition and
// read-only after, and everything else the compile touches is the request's.
func (p *plugin) compileShaderCmdImpl() (kernel.Lock, kernel.Execute[CompileShaderRequest, CompileShaderResponse]) {
	return nil, func(_ kernel.Kernel, request CompileShaderRequest) CompileShaderResponse {
		program, err := shader.CompileShader(request.FS, request.Descr, p.backend.Get().ReflectShader)
		return CompileShaderResponse{Program: program, Err: err}
	}
}

func (p *plugin) releaseCachedResourceCmdImpl() (kernel.Lock, kernel.Execute[ReleaseCachedResourceRequest, ReleaseCachedResourceResponse]) {
	var resources kernel.Write[*ResourceQueue]
	return func(access kernel.ResourceAccess) {
			resources = access.GetWrite[*ResourceQueue]()
		}, func(_ kernel.Kernel, request ReleaseCachedResourceRequest) ReleaseCachedResourceResponse {
			resources.Get().releaseCachedResource(request.Path)
			return ReleaseCachedResourceResponse{}
		}
}

func (p *plugin) freeCachedResourcesCmdImpl() (kernel.Lock, kernel.Execute[FreeCachedResourcesRequest, FreeCachedResourcesResponse]) {
	var resources kernel.Write[*ResourceQueue]
	return func(access kernel.ResourceAccess) {
			resources = access.GetWrite[*ResourceQueue]()
		}, func(kernel.Kernel, FreeCachedResourcesRequest) FreeCachedResourcesResponse {
			resources.Get().freeCachedResources()
			return FreeCachedResourcesResponse{}
		}
}

func setViewportCmdImpl() (kernel.Lock, kernel.Execute[SetViewportRequest, SetViewportResponse]) {
	var preference kernel.Read[*types.DesiredViewport]
	var current kernel.Write[*types.Viewport]
	return func(access kernel.ResourceAccess) {
			preference = access.GetRead[*types.DesiredViewport]()
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
	var stored kernel.Write[*types.DesiredViewport]
	var current kernel.Write[*types.Viewport]
	return func(access kernel.ResourceAccess) {
			stored = access.GetWrite[*types.DesiredViewport]()
			current = access.GetWrite[*types.Viewport]()
		}, func(_ kernel.Kernel, request SetDesiredViewportRequest) SetDesiredViewportResponse {
			preference := types.DesiredViewport{
				Mode: request.Mode, Width: request.Width, Height: request.Height, Size: request.Size,
			}
			valid := request.Mode == types.ViewportWindow ||
				((request.Mode == types.ViewportFixedWidth || request.Mode == types.ViewportFixedHeight) && request.Size > 0) ||
				((request.Mode == types.ViewportFit || request.Mode == types.ViewportCover) && request.Width > 0 && request.Height > 0)
			if !valid {
				preference = types.DesiredViewport{}
			}
			stored.Set(&preference)
			viewport := resolveViewport(current.Get().WindowWidth, current.Get().WindowHeight, preference)
			viewport.FramebufferWidth = current.Get().FramebufferWidth
			viewport.FramebufferHeight = current.Get().FramebufferHeight
			current.Set(&viewport)
			return SetDesiredViewportResponse{Viewport: viewport}
		}
}

func resolveViewport(windowWidth, windowHeight float32, preference types.DesiredViewport) types.Viewport {
	viewport := types.Viewport{
		Width: windowWidth, Height: windowHeight,
		WindowWidth: windowWidth, WindowHeight: windowHeight,
	}
	if windowWidth <= 0 || windowHeight <= 0 {
		viewport.Width, viewport.Height = 0, 0
		return viewport
	}
	switch preference.Mode {
	case types.ViewportFixedWidth:
		viewport.Width = preference.Size
		viewport.Height = float32(math.Round(float64(preference.Size * windowHeight / windowWidth)))
	case types.ViewportFixedHeight:
		viewport.Height = preference.Size
		viewport.Width = float32(math.Round(float64(preference.Size * windowWidth / windowHeight)))
	case types.ViewportFit:
		if windowWidth/windowHeight >= preference.Width/preference.Height {
			viewport.Height = preference.Height
			viewport.Width = float32(math.Round(float64(preference.Height * windowWidth / windowHeight)))
		} else {
			viewport.Width = preference.Width
			viewport.Height = float32(math.Round(float64(preference.Width * windowHeight / windowWidth)))
		}
	case types.ViewportCover:
		if windowWidth/windowHeight >= preference.Width/preference.Height {
			viewport.Width = preference.Width
			viewport.Height = float32(math.Round(float64(preference.Width * windowHeight / windowWidth)))
		} else {
			viewport.Height = preference.Height
			viewport.Width = float32(math.Round(float64(preference.Height * windowWidth / windowHeight)))
		}
	}
	return viewport
}
