package internal

import (
	"errors"
	"strings"
	"testing"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// Every draw names a set. These read which set a drawable holds from the load
// System's scratch, and what it drew from the backend.

// holds sums the holds on scene's own sets.
func holds(sets map[setKey]int) int {
	total := 0
	for _, refs := range sets {
		total += refs
	}
	return total
}

// A crate drawn as its file says draws the set model built at load, so scene
// holds nothing for it; the same crate under a Material draws a set of scene's
// own, one per primitive that draws through it. Both draw.
func TestAFileMaterialDrawsModelsSetAndAnOverrideOneOfScenes(t *testing.T) {
	h := newDrawingHarness(t, 64)
	file := h.spawn(t, spawnRequest{Place: m.At(-3, 0, 0), Model: crateModelComponent()})
	overridden := h.spawn(t, spawnRequest{Place: m.At(3, 0, 0), Model: crateModelComponent(),
		Material: &Material{Tags: m.NewList(MaterialTag{Shader: gfx.ShaderWithText("outline")})}})
	keys := h.keyedModels(t, file, overridden)
	h.frameUntil(t, "both crates to draw", func() bool { return len(h.drawn()) == 2 })

	if held := keys.modelSets[file]; len(held) != 0 {
		t.Errorf("the file's own material holds scene's sets %v, want model's own", held)
	}
	if held := keys.modelSets[overridden]; len(held) != len(keys.models[overridden].keys) {
		t.Errorf("the overridden crate holds %d of scene's sets for %d primitives", len(held), len(keys.models[overridden].keys))
	}
	if got := where(h.drawn(), at(m.Vec3{X: -3})); len(got) != 1 || !strings.Contains(got[0].shader, "sceneInstances") {
		t.Errorf("the file's crate drew %d instances, want one under the bundled shader", len(got))
	}
	if got := where(h.drawn(), at(m.Vec3{X: 3})); len(got) != 1 || strings.TrimSpace(got[0].shader) != "outline" {
		t.Errorf("the overridden crate drew %d instances, want one under its Material's shader", len(got))
	}
	h.noErrors(t)
}

// A set of scene's own is shared by every drawable whose key names it, and
// released with the last of them: the load System holds it once per holder and
// lets it go when the holder is despawned.
func TestScenesSetIsSharedAndReleasedWithItsLastHolder(t *testing.T) {
	h := newDrawingHarness(t, 64)
	ref := h.bake(t)
	material := &Material{Tags: m.NewList(MaterialTag{Shader: gfx.ShaderWithText("outline")})}
	first := h.spawn(t, spawnRequest{Place: m.At(-3, 0, 0), Mesh: &Mesh{Ref: ref}, Material: material})
	second := h.spawn(t, spawnRequest{Place: m.At(3, 0, 0), Mesh: &Mesh{Ref: ref}, Material: material})
	h.frameUntil(t, "both meshes to draw", func() bool { return len(h.drawn()) == 2 })

	if sets := h.keys(t).sets; len(sets) != 1 || holds(sets) != 2 {
		t.Fatalf("two meshes under one Material hold %v, want one set held twice", sets)
	}
	h.despawn(t, first)
	h.frame(t)
	if sets := h.keys(t).sets; len(sets) != 1 || holds(sets) != 1 {
		t.Errorf("after one despawn the sets are %v, want the one set held once", sets)
	}
	if got := len(h.drawn()); got != 1 {
		t.Errorf("the remaining mesh drew %d instances, want 1", got)
	}
	h.despawn(t, second)
	h.frame(t)
	if sets := h.keys(t).sets; len(sets) != 0 {
		t.Errorf("with no holder left the sets are %v, want none", sets)
	}
	h.noErrors(t)
}

// A version carries forward whatever it does not name, so Batches of one
// material that set different bindings must not share a set: two tints of one
// Material each draw their own, and a mesh with no Params and one whose
// Params set only a binding of its own both keep the material's white.
func TestAParamsTintDoesNotLeakIntoAnotherBatch(t *testing.T) {
	h := newDrawingHarness(t, 64)
	ref := h.bake(t)
	material := &Material{Tags: m.NewList(MaterialTag{Shader: gfx.ShaderWithText("outline")})}
	for _, c := range []struct {
		x      float32
		params *Params
	}{
		{-6, tint(red)},
		{-3, nil},
		{0, tint(blue)},
		{3, &Params{Values: m.NewList(gfx.FloatParam("fade", 0.5))}},
		{6, nil},
	} {
		h.spawn(t, spawnRequest{Place: m.At(c.x, 0, 0), Mesh: &Mesh{Ref: ref}, Material: material, Params: c.params})
	}
	h.frameUntil(t, "every mesh to draw", func() bool { return len(h.drawn()) == 5 })

	white := m.Vec4{X: 1, Y: 1, Z: 1, W: 1}
	for _, c := range []struct {
		x      float32
		colour m.Vec4
		fade   float32
	}{
		{-6, m.Vec4{X: 1, W: 1}, 0}, {-3, white, 0}, {0, m.Vec4{Z: 1, W: 1}, 0}, {3, white, 0.5}, {6, white, 0},
	} {
		drawn := where(h.drawn(), at(m.Vec3{X: c.x}))
		if len(drawn) != 1 {
			t.Fatalf("the mesh at x=%v drew %d instances, want 1", c.x, len(drawn))
		}
		if got := drawn[0].param("baseColorFactor"); got != c.colour {
			t.Errorf("the mesh at x=%v drew baseColorFactor %v, want %v", c.x, got, c.colour)
		}
		if got := drawn[0].param("fade").X; got != c.fade {
			t.Errorf("the mesh at x=%v bound fade %v, want %v", c.x, got, c.fade)
		}
	}
	h.noErrors(t)
}

// A Material whose shader does not compile draws nothing, and says so once
// however many frames and drawables name it; everything else draws.
func TestAShaderThatDoesNotCompileDrawsNothingAndIsReportedOnce(t *testing.T) {
	h := newDrawingHarness(t, 64)
	ref := h.bake(t)
	broken := &Material{Tags: m.NewList(MaterialTag{Shader: gfx.ShaderWithResource("shaders/missing.wgsl")})}
	h.spawn(t, spawnRequest{Place: m.At(-3, 0, 0), Mesh: &Mesh{Ref: ref}, Material: broken})
	h.spawn(t, spawnRequest{Place: m.At(0, 0, 0), Model: crateModelComponent(), Material: broken})
	h.spawn(t, spawnRequest{Place: m.At(3, 0, 0), Mesh: &Mesh{Ref: ref}})
	h.frameUntil(t, "the crate to be keyed", func() bool {
		entries := h.keys(t).models
		for _, entry := range entries {
			if len(entry.keys) > 0 {
				return true
			}
		}
		return false
	})
	for range 3 {
		h.frame(t)
	}

	if got := h.drawn(); len(got) != 1 || !nearVec3(got[0].position(), m.Vec3{X: 3}) {
		t.Errorf("the frame drew %v, want only the plain mesh", positions(got))
	}
	reports := 0
	for _, err := range h.errs.snapshot() {
		var unavailable ErrMaterialShaderUnavailable
		if !errors.As(err, &unavailable) {
			t.Errorf("the frame reported %v", err)
			continue
		}
		reports++
	}
	if reports != 1 {
		t.Errorf("the broken shader was reported %d times, want once", reports)
	}
}
