package rendertest_test

import (
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gfx/recording"
	"github.com/dvoyni/cog/render"
	"github.com/dvoyni/cog/render/internal/handle"
)

// assetCall is one of gfx's calls on a texture or buffer render made, read in
// render's terms: its make, a write, or its release.
type assetCall[H comparable] struct {
	kind  recording.CallKind
	asset H
	// index is the call's among every call recorded, and arguments its own.
	index     int
	arguments any
	// released is whether the call released the asset.
	released bool
	// presentCount is how many presents came before the call, and
	// betweenFrames whether it came after a present with no acquire of a
	// framebuffer between them.
	presentCount  int
	betweenFrames bool
}

// assetCallSequence is the calls on the textures or buffers render made, in
// the order gfx recorded them.
type assetCallSequence[H comparable] []assetCall[H]

// readTextureCalls answers every call among calls that made, wrote or
// released a texture render made, in order, each naming the handle the
// texture's label named.
func readTextureCalls(calls []recording.Call) assetCallSequence[render.Texture] {
	return readAssetCalls[render.Texture](calls, func(call recording.Call) (any, string, bool) {
		switch call.Kind {
		case recording.CallKindCreateTexture:
			arguments := call.Arguments.(recording.CreateTextureArguments)
			return arguments.Texture, arguments.Descriptor.Label, true
		case recording.CallKindWriteTexture:
			return call.Arguments.(recording.WriteTextureArguments).Destination.Texture, "", true
		case recording.CallKindReleaseTexture:
			return call.Arguments.(gfx.Texture), "", true
		}
		return nil, "", false
	}, recording.CallKindReleaseTexture)
}

// readBufferCalls answers every call among calls that made, wrote or
// released a buffer render made, in order, each naming the handle the
// buffer's label named.
func readBufferCalls(calls []recording.Call) assetCallSequence[render.Buffer] {
	return readAssetCalls[render.Buffer](calls, func(call recording.Call) (any, string, bool) {
		switch call.Kind {
		case recording.CallKindCreateBuffer:
			arguments := call.Arguments.(recording.CreateBufferArguments)
			return arguments.Buffer, arguments.Descriptor.Label, true
		case recording.CallKindWriteBuffer:
			return call.Arguments.(recording.WriteBufferArguments).Buffer, "", true
		case recording.CallKindReleaseBuffer:
			return call.Arguments.(gfx.Buffer), "", true
		}
		return nil, "", false
	}, recording.CallKindReleaseBuffer)
}

// readAssetCalls answers every call among calls on a gfx object render made
// for an H, in order, each naming the handle the object's label named:
// readObject answers the object a call is on, with its label for its make,
// false for a call on none, and releaseKind is the kind of call releasing
// one.
func readAssetCalls[H handle.Handle](calls []recording.Call, readObject func(call recording.Call) (object any, label string, ok bool), releaseKind recording.CallKind) assetCallSequence[H] {
	assets := map[any]H{}
	var assetCalls assetCallSequence[H]
	var presentCount int
	var lastFrameCallKind recording.CallKind
	for index, call := range calls {
		switch call.Kind {
		case recording.CallKindAcquireFramebuffer:
			lastFrameCallKind = call.Kind
			continue
		case recording.CallKindPresent:
			lastFrameCallKind = call.Kind
			presentCount++
			continue
		}
		object, label, ok := readObject(call)
		if !ok {
			continue
		}
		if asset, ok := handle.ParseLabel[H](label); ok {
			assets[object] = asset
		}
		asset, ok := assets[object]
		if !ok {
			continue
		}
		released := call.Kind == releaseKind
		assetCalls = append(assetCalls, assetCall[H]{
			kind:          call.Kind,
			asset:         asset,
			index:         index,
			arguments:     call.Arguments,
			released:      released,
			presentCount:  presentCount,
			betweenFrames: lastFrameCallKind == recording.CallKindPresent,
		})
		if released {
			delete(assets, object)
		}
	}
	return assetCalls
}

// find answers those of s's calls of kind on asset.
func (s assetCallSequence[H]) find(kind recording.CallKind, asset H) []assetCall[H] {
	var found []assetCall[H]
	for _, call := range s {
		if call.kind == kind && call.asset == asset {
			found = append(found, call)
		}
	}
	return found
}

// findCreated answers the arguments, an A, the gfx object behind asset was
// made with among s's calls; false when it was not made once.
func (s assetCallSequence[H]) findCreated[A any](asset H) (A, bool) {
	var created []A
	for _, call := range s {
		if arguments, ok := call.arguments.(A); ok && call.asset == asset {
			created = append(created, arguments)
		}
	}
	if len(created) != 1 {
		var none A
		return none, false
	}
	return created[0], true
}

// findFreeingFrame answers the index of the frame whose end released asset
// among s's calls: how many presents came before its release, less one;
// false when it was not released once, between a present and the next
// frame's acquire.
func (s assetCallSequence[H]) findFreeingFrame(asset H) (int, bool) {
	var releases []assetCall[H]
	for _, call := range s {
		if call.released && call.asset == asset {
			releases = append(releases, call)
		}
	}
	if len(releases) != 1 {
		return 0, false
	}
	return releases[0].presentCount - 1, releases[0].betweenFrames
}
