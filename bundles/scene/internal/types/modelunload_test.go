package types

import (
	"testing"

	"github.com/dvoyni/cog/extensions/gfx"
)

// modelPath is the path the unload tests name.
const modelPath = "models/prop.glb"

// The generation counter is the whole of the ghost rule: an unload while a load
// is in flight has to make the completing load throw its result away, because
// the entry it would install into belongs to nobody.
func TestAnUnloadWhileLoadingDiscardsTheCompletingLoad(t *testing.T) {
	lookup := NewSizedLookup(Config{})
	lookup.models = map[string]*ModelEntry{modelPath: {State: ModelLoading, Generation: 1}}
	lookup.unloadModels = append(lookup.unloadModels, modelPath)
	lookup.applyUnloads(func(gfx.TextureDescr) {})

	var reported []error
	lookup.installModel(func(err error) { reported = append(reported, err) },
		modelPath, 1, &LoadedModel{}, nil, &gfx.ResourceQueue{})

	if entry := lookup.models[modelPath]; entry.State == ModelResident {
		t.Fatal("the completing load installed into an entry the unload had already retired")
	}
	if len(reported) != 0 {
		t.Fatalf("reported %v, want a silent discard", reported)
	}
}

// UnloadModel does not cascade to textures, because with no refcount it cannot
// know whether another resident model shares one by path. The wart is deliberate
// and asserted so nobody quietly fixes it into a use-after-free.
func TestUnloadModelDoesNotFreeTextures(t *testing.T) {
	lookup := NewSizedLookup(Config{})
	lookup.models = map[string]*ModelEntry{modelPath: {State: ModelResident, Generation: 1}}
	lookup.textures = map[textureKey]gfx.TextureDescr{{path: modelPath, image: 0}: {}}
	lookup.unloadModels = append(lookup.unloadModels, modelPath)

	freed := 0
	lookup.applyUnloads(func(gfx.TextureDescr) { freed++ })
	if freed != 0 || len(lookup.textures) != 1 {
		t.Fatalf("freed %d textures and %d remain; want the cache untouched", freed, len(lookup.textures))
	}

	// UnloadTexture is the lever that does free one, and it frees every
	// colour-space variant the path baked, because the path is the whole of
	// what a caller can name.
	lookup.textures[textureKey{path: modelPath, image: 0, srgb: true}] = gfx.TextureDescr{}
	lookup.unloadTextures = append(lookup.unloadTextures, modelPath)
	lookup.applyUnloads(func(gfx.TextureDescr) { freed++ })
	if freed != 2 || len(lookup.textures) != 0 {
		t.Fatalf("freed %d and %d remain; want both variants gone", freed, len(lookup.textures))
	}
}
