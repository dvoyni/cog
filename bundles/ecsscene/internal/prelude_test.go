package internal

import (
	"fmt"
	"strings"
	"testing"

	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
	"github.com/gogpu/naga"
	"github.com/gogpu/naga/ir"
	"github.com/gogpu/naga/wgsl"
)

// preludeMaterialSource is a custom material written against model's published
// sources and nothing else: it reads the storage vertex through VertexDecodePath
// and lights through PbrPath, which brings FramePath with it. It declares no
// binding of its own, so everything it binds is what the prelude costs. scene's
// tests draw the same text.
const preludeMaterialSource = "//#include " + model.VertexDecodePath + "\n" +
	"//#include " + model.PbrPath + "\n" + `
struct PreludeVaryings {
    @builtin(position) clip: vec4<f32>,
    @location(0) world: vec3<f32>,
    @location(1) normal: vec3<f32>,
};

@vertex
fn vs_main(@location(0) position: vec3<f32>, @location(1) normal: vec2<f32>) -> PreludeVaryings {
    var out: PreludeVaryings;
    out.clip = sceneFrame.viewProjection * vec4<f32>(position, 1.0);
    out.world = position;
    out.normal = sceneDecodeNormal(normal);
    return out;
}

// preludeMaterialAlbedo marks this module among the ones a frame compiles.
fn preludeMaterialAlbedo() -> vec3<f32> {
    return vec3<f32>(0.8, 0.3, 0.2);
}

@fragment
fn fs_main(in: PreludeVaryings) -> @location(0) vec4<f32> {
    let s = SceneSurface(in.world, normalize(in.normal), preludeMaterialAlbedo(), 0.0, 0.7, 1.0);
    return vec4<f32>(sceneShadeSurface(s), 1.0);
}
`

// A custom material that includes PbrPath draws under ecsscene exactly as it
// does under scene: the includes resolve through the mount model contributes,
// the draw reaches the backend with the pass's sceneFrame bound, and what the
// module declares is sceneFrame alone, carrying the light array at
// model.MaxLights.
func TestACustomMaterialIncludingThePreludeDrawsUnderECSScene(t *testing.T) {
	h := newDrawingHarness(t, 256)
	ref := h.bake(t)
	material := &Material{Tags: m.NewList(MaterialTag{
		Shader: gfx.ShaderWithText(preludeMaterialSource), State: gfx.StateOpaque3D(),
	})}
	h.spawn(t, spawnRequest{Mesh: &Mesh{Ref: ref}, Material: material})

	isPrelude := func(d drawnInstance) bool { return strings.Contains(d.shader, "fn preludeMaterialAlbedo") }
	h.frameUntil(t, "the custom material to draw", func() bool { return len(where(h.drawn(), isPrelude)) > 0 })
	h.noErrors(t)

	drawn := where(h.drawn(), isPrelude)
	if len(drawn) != 1 {
		t.Fatalf("the custom material drew %d instances, want 1", len(drawn))
	}
	if len(drawn[0].frame) == 0 {
		t.Error("the custom-material draw bound no sceneFrame")
	}
	bindings, lights := preludeReflection(t, drawn[0].shader)
	if len(bindings) != 1 || bindings[0] != "sceneFrame storage 0/0" {
		t.Errorf("the custom material declares %v, want sceneFrame storage 0/0 alone", bindings)
	}
	if lights != model.MaxLights {
		t.Errorf("its sceneFrame carries %d lights, want model.MaxLights = %d", lights, model.MaxLights)
	}
}

// preludeReflection lowers one module through naga, the front end gogpu
// reflects with, and reports every binding it declares as "name space
// group/binding" with sceneFrame's light count beside them.
func preludeReflection(t *testing.T, module string) (bindings []string, lights int) {
	t.Helper()
	parsed, err := naga.Parse(module)
	if err != nil {
		t.Fatalf("parse the custom material: %v", err)
	}
	lowered, err := wgsl.Lower(parsed)
	if err != nil {
		t.Fatalf("lower the custom material: %v", err)
	}
	lights = -1
	for _, global := range lowered.GlobalVariables {
		if global.Binding == nil {
			continue
		}
		space := "other"
		if global.Space == ir.SpaceStorage {
			space = "storage"
		}
		bindings = append(bindings, fmt.Sprintf("%s %s %d/%d",
			global.Name, space, global.Binding.Group, global.Binding.Binding))
		frame, ok := lowered.Types[global.Type].Inner.(ir.StructType)
		if global.Name != "sceneFrame" || !ok {
			continue
		}
		for _, member := range frame.Members {
			if array, ok := lowered.Types[member.Type].Inner.(ir.ArrayType); ok &&
				member.Name == "lights" && array.Size.Constant != nil {
				lights = int(*array.Size.Constant)
			}
		}
	}
	return bindings, lights
}
