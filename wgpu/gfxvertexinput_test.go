package wgpu

import (
	"os"
	"testing"

	"github.com/dvoyni/cog/canvas"
	cgfx "github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/scene"
	"github.com/dvoyni/cog/storage"
)

// bundledShader flattens one of the engine's own root shaders off disk. wgpu is
// the only tree with a WGSL front end, so the reflection half of the check can
// only be exercised here.
func bundledShader(t *testing.T, mount storage.MountId, dir, path string, opts ...cgfx.ShaderOption) string {
	t.Helper()
	filesystem := storage.NewFileSystem(mount, os.DirFS(dir))
	text, _, err := cgfx.FlattenShader(filesystem, cgfx.ShaderWithResource(path, opts...))
	if err != nil {
		t.Fatalf("flatten %s: %v", path, err)
	}
	return text
}

// The declared vertex inputs are the shader half of a contract whose other half
// is a Go layout, and nothing but this reflection can see them. What is pinned
// here is that the reflection finds them at all, and finds the right ones: the
// no-skin variant declares six, the skinned one eight, and the fragment stage's
// own @locations - it declares six interpolants of its own - are not vertex
// inputs and must not appear.
func TestBundledSceneShaderReflectsItsVertexStageInputs(t *testing.T) {
	for _, variant := range []struct {
		name  string
		opts  []cgfx.ShaderOption
		count int
	}{
		{"no skin", sceneVariant(), 6},
		{"skinned", sceneVariant("SCENE_SKIN"), 8},
		{"everything", everyFeature(), 8},
	} {
		t.Run(variant.name, func(t *testing.T) {
			layout, err := reflectShaderLayout(bundledSceneShader(t, variant.opts...))
			if err != nil {
				t.Fatalf("reflect the bundled scene shader: %v", err)
			}
			want := []cgfx.ShaderVertexInput{
				{Name: "position", Location: 0, Kind: cgfx.VertexScalarFloat, Count: 3},
				// The two encoded attributes: the normal is oct32 in a
				// two-component unorm and the tangent is one word of oct 15/15
				// plus handedness, so what the stage declares is a vec2<f32>
				// and a u32 and vertexdecode.wgsl makes directions of them.
				{Name: "normal", Location: 1, Kind: cgfx.VertexScalarFloat, Count: 2},
				{Name: "tangent", Location: 2, Kind: cgfx.VertexScalarUint, Count: 1},
				{Name: "uv0", Location: 3, Kind: cgfx.VertexScalarFloat, Count: 2},
				{Name: "uv1", Location: 4, Kind: cgfx.VertexScalarFloat, Count: 2},
				{Name: "color", Location: 5, Kind: cgfx.VertexScalarFloat, Count: 4},
				{Name: "joints", Location: 6, Kind: cgfx.VertexScalarUint, Count: 4},
				{Name: "weights", Location: 7, Kind: cgfx.VertexScalarFloat, Count: 4},
			}[:variant.count]
			if len(layout.VertexInputs) != len(want) {
				t.Fatalf("reflected %d vertex inputs, want %d: %+v",
					len(layout.VertexInputs), len(want), layout.VertexInputs)
			}
			for i, got := range layout.VertexInputs {
				if got != want[i] {
					t.Errorf("vertex input %d = %+v, want %+v", i, got, want[i])
				}
			}
		})
	}
}

// The whole point of the check is that the engine's own pairs pass it. Both
// halves are the real ones: the shader is flattened and reflected here, and the
// layout is the Go value the plugin actually binds. The Go import is the
// dependency this file otherwise avoids, and it is what makes the pair real
// rather than a copy of it that can drift.
func TestEveryBundledShaderAndLayoutPairPassesTheVertexInterfaceCheck(t *testing.T) {
	// The quad is canvas's own, declared at canvas/plugin.go where the sprite
	// mesh is built: one vec2 corner, instanced.
	quad := []cgfx.VertexAttr{cgfx.Attr(0, cgfx.Float32x2)}
	for _, pair := range []struct {
		name   string
		source string
		attrs  []cgfx.VertexAttr
	}{
		{"scene", bundledSceneShader(t), scene.Vertex{}.VertexLayout()},
		{"scene skinned", bundledSceneShader(t, everyFeature()...), scene.Vertex{}.VertexLayout()},
		{"canvas triangles", canvasShader(t, "builtin/canvas/triangles.wgsl"), canvas.Vertex{}.VertexLayout()},
		{"canvas texture", canvasShader(t, "builtin/canvas/texture.wgsl"), canvas.Vertex{}.VertexLayout()},
		{"canvas sprite", canvasShader(t, "builtin/canvas/sprite.wgsl"), quad},
		{"canvas halo", canvasShader(t, "builtin/canvas/halo.wgsl"), quad},
	} {
		t.Run(pair.name, func(t *testing.T) {
			layout, err := reflectShaderLayout(pair.source)
			if err != nil {
				t.Fatalf("reflect: %v", err)
			}
			if len(layout.VertexInputs) == 0 {
				t.Fatalf("no vertex inputs reflected; the check would pass vacuously")
			}
			if err := cgfx.CheckVertexInterface(pair.name, layout, pair.attrs); err != nil {
				t.Fatalf("the bundled pair is refused: %v", err)
			}
		})
	}
}

func canvasShader(t *testing.T, path string) string {
	t.Helper()
	return bundledShader(t, "canvas", "../canvas", path)
}
