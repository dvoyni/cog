package types

import (
	"testing"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/gfx"
)

// modelPath is the path the unload tests name.
const modelPath = "models/prop.glb"

// reportingKernel hands back a kernel whose handler collects what the lookup
// reports. The engine is never run: report-once needs the engine only for the
// table it keys and the handler it calls, both of which exist from New.
func reportingKernel() (kernel.Kernel, *[]error) {
	var reported []error
	engine := kernel.New(nil).Handler(func(err error) bool {
		reported = append(reported, err)
		return false
	})
	return engine.Executioner().Kernel, &reported
}

// UnloadModel does not cascade to textures, because with no refcount it cannot
// know whether another loaded model shares one by path. The wart is deliberate
// and asserted so nobody quietly fixes it into a use-after-free.
//
// UnloadTexture is the lever that does free one, and it frees every
// colour-space variant the path baked, because the path is the whole of what a
// caller can name. That is why it is the verb that carries the device: the
// entries here are GPU textures and releasing one needs the queue at the call.
func TestUnloadModelDoesNotFreeTextures(t *testing.T) {
	lookup := NewSizedLookup(Config{})
	lookup.textures = map[textureKey]gfx.TextureDescr{
		{path: modelPath, image: 0}:             {},
		{path: modelPath, image: 0, srgb: true}: {},
	}
	k, _ := reportingKernel()

	NewLookupAccess(k, lookup).UnloadModel(modelPath)
	if len(lookup.textures) != 2 {
		t.Fatalf("%d textures remain after UnloadModel, want the cache untouched",
			len(lookup.textures))
	}

	NewLookupDeviceAccess(k, lookup, nil, &gfx.ResourceQueue{}).UnloadTexture(modelPath)
	if len(lookup.textures) != 0 {
		t.Fatalf("%d textures remain after UnloadTexture, want both variants gone",
			len(lookup.textures))
	}
}
