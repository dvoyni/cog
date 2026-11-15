package rendertest

import (
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gfx/recording"
	"github.com/dvoyni/cog/platform"
	"github.com/dvoyni/cog/render"
)

// viewTarget is what a gfx view drawn into or bound is in render's terms: a
// window's framebuffer, or one of render's textures at one mip level and
// layer, seen through descriptor.
type viewTarget struct {
	window          platform.Window
	texture         render.Texture
	mipLevel, layer uint32
	descriptor      gfx.TextureViewDescriptor
}

// makeViewTargets answers what each view the recording's calls made or were
// given is in render's terms: the framebuffer views its acquires were given,
// by window, and the views of the textures render made, by each texture's
// handle in textures, with the mip level and layer each view starts at and
// its descriptor.
func makeViewTargets(calls []recording.Call, textures map[gfx.Texture]render.Texture) map[gfx.TextureView]viewTarget {
	viewTargets := map[gfx.TextureView]viewTarget{}
	for _, call := range calls {
		switch call.Kind {
		case recording.CallKindAcquireFramebuffer:
			arguments := call.Arguments.(recording.AcquireFramebufferArguments)
			viewTargets[arguments.View] = viewTarget{window: arguments.Window}
		case recording.CallKindCreateTextureView:
			arguments := call.Arguments.(recording.CreateTextureViewArguments)
			if texture, ok := textures[arguments.Texture]; ok {
				viewTargets[arguments.View] = viewTarget{texture: texture, mipLevel: arguments.Descriptor.BaseMipLevel, layer: arguments.Descriptor.BaseArrayLayer, descriptor: arguments.Descriptor}
			}
		}
	}
	return viewTargets
}
