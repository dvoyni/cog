package rendertest

import (
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gfx/recording"
	"github.com/dvoyni/cog/platform"
	"github.com/dvoyni/cog/render"
	"github.com/dvoyni/cog/render/internal/handle"
)

// viewTarget is what a gfx view drawn into is in render's terms: a window's
// framebuffer, or one of render's textures at one mip level and layer.
type viewTarget struct {
	window          platform.Window
	texture         render.Texture
	mipLevel, layer uint32
}

// makeViewTargets answers what each view the recording's calls made or were
// given is in render's terms: the framebuffer views its acquires were given,
// by window, and the views of the textures render made, by the handle each
// texture's label names, with the mip level and layer each view starts at.
func makeViewTargets(calls []recording.Call) map[gfx.TextureView]viewTarget {
	textures := map[gfx.Texture]render.Texture{}
	viewTargets := map[gfx.TextureView]viewTarget{}
	for _, call := range calls {
		switch call.Kind {
		case recording.CallKindAcquireFramebuffer:
			arguments := call.Arguments.(recording.AcquireFramebufferArguments)
			viewTargets[arguments.View] = viewTarget{window: arguments.Window}
		case recording.CallKindCreateTexture:
			arguments := call.Arguments.(recording.CreateTextureArguments)
			if texture, ok := handle.ParseLabel[render.Texture](arguments.Descriptor.Label); ok {
				textures[arguments.Texture] = texture
			}
		case recording.CallKindCreateTextureView:
			arguments := call.Arguments.(recording.CreateTextureViewArguments)
			if texture, ok := textures[arguments.Texture]; ok {
				viewTargets[arguments.View] = viewTarget{texture: texture, mipLevel: arguments.Descriptor.BaseMipLevel, layer: arguments.Descriptor.BaseArrayLayer}
			}
		}
	}
	return viewTargets
}
