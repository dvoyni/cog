package rendertest_test

import (
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gfx/recording"
	"github.com/dvoyni/cog/render"
	"github.com/dvoyni/cog/render/internal/handle"
)

// textureCall is one of gfx's calls on a texture render made, read in
// render's terms: its make, a write, or its release.
type textureCall struct {
	kind    recording.CallKind
	texture render.Texture
	// index is the call's among every call recorded, and arguments its own.
	index     int
	arguments any
	// presentCount is how many presents came before the call, and
	// betweenFrames whether it came after a present with no acquire of a
	// framebuffer between them.
	presentCount  int
	betweenFrames bool
}

// readTextureCalls answers every call among calls that made, wrote or
// released a texture render made, in order, each naming the handle the
// texture's label named.
func readTextureCalls(calls []recording.Call) []textureCall {
	renderTextures := map[gfx.Texture]render.Texture{}
	var textureCalls []textureCall
	var presentCount int
	var lastFrameCallKind recording.CallKind
	for index, call := range calls {
		var gfxTexture gfx.Texture
		switch call.Kind {
		case recording.CallKindAcquireFramebuffer:
			lastFrameCallKind = call.Kind
			continue
		case recording.CallKindPresent:
			lastFrameCallKind = call.Kind
			presentCount++
			continue
		case recording.CallKindCreateTexture:
			arguments := call.Arguments.(recording.CreateTextureArguments)
			gfxTexture = arguments.Texture
			if texture, ok := handle.ParseLabel[render.Texture](arguments.Descriptor.Label); ok {
				renderTextures[gfxTexture] = texture
			}
		case recording.CallKindWriteTexture:
			gfxTexture = call.Arguments.(recording.WriteTextureArguments).Destination.Texture
		case recording.CallKindReleaseTexture:
			gfxTexture = call.Arguments.(gfx.Texture)
		default:
			continue
		}
		texture, ok := renderTextures[gfxTexture]
		if !ok {
			continue
		}
		textureCalls = append(textureCalls, textureCall{
			kind:          call.Kind,
			texture:       texture,
			index:         index,
			arguments:     call.Arguments,
			presentCount:  presentCount,
			betweenFrames: lastFrameCallKind == recording.CallKindPresent,
		})
		if call.Kind == recording.CallKindReleaseTexture {
			delete(renderTextures, gfxTexture)
		}
	}
	return textureCalls
}

// findTextureCalls answers those of textureCalls of kind on texture.
func findTextureCalls(textureCalls []textureCall, kind recording.CallKind, texture render.Texture) []textureCall {
	var found []textureCall
	for _, call := range textureCalls {
		if call.kind == kind && call.texture == texture {
			found = append(found, call)
		}
	}
	return found
}

// findFreeingFrame answers the index of the frame whose end released
// texture: how many presents came before its release, less one; false when
// it was not released once, between a present and the next frame's acquire.
func findFreeingFrame(textureCalls []textureCall, texture render.Texture) (int, bool) {
	releases := findTextureCalls(textureCalls, recording.CallKindReleaseTexture, texture)
	if len(releases) != 1 {
		return 0, false
	}
	return releases[0].presentCount - 1, releases[0].betweenFrames
}
