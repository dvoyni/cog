package internal

import (
	"strings"
	"testing"

	"github.com/dvoyni/cog/bundles/ecsscene"
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// A Material overlays what the file provides rather than replacing it. Each of
// these reads back what a draw was made of, primitive by primitive: the shader
// in effect, the state, and the uniforms gfx packed from its params by name -
// the file's numbers among them.

// paintedDraws finds the painted model's two primitives standing at x, by the
// vertex count that tells the triangle from the quad.
func paintedDraws(t *testing.T, h *harness, x float32) (red, green drawnInstance) {
	t.Helper()
	drawn := where(h.drawn(), at(m.Vec3{X: x}))
	triangles, quads := where(drawn, ofTriangle), where(drawn, ofQuad)
	if len(triangles) != 1 || len(quads) != 1 {
		t.Fatalf("the painted model at x=%v drew %d triangles and %d quads, want one of each",
			x, len(triangles), len(quads))
	}
	return triangles[0], quads[0]
}

// The tracer: a tag naming only a shader and a param of its own, over a model
// of two materials. Each primitive draws under the tag's shader and with its
// param, and keeps its own material's state and numbers - the red triangle
// blended, the green quad double-sided - which is what the old replacement
// lost to one white paint shared by both.
func TestAMaterialOverlaysEachPrimitivesOwnMaterial(t *testing.T) {
	h := newDrawingHarness(t, 256)
	painted := &ecsscene.Model{Ref: model.ModelRef{Path: paintedModel}}
	h.spawn(t, spawnRequest{Place: m.At(-3, 0, 0), Model: painted, Material: &ecsscene.Material{Tags: m.NewList(
		ecsscene.MaterialTag{Shader: gfx.ShaderWithText("fade"), Params: m.NewList(gfx.FloatParam("a", 7))},
	)}})
	h.spawn(t, spawnRequest{Place: m.At(3, 0, 0), Model: painted})

	h.frameUntil(t, "the painted model to become resident", func() bool {
		return len(where(h.drawn(), at(m.Vec3{X: 3}))) == 2
	})

	red, green := paintedDraws(t, h, -3)
	fileRed, fileGreen := paintedDraws(t, h, 3)
	for _, c := range []struct {
		name       string
		got, file  drawnInstance
		state      gfx.MaterialState
		baseColour m.Vec4
	}{
		{"red triangle", red, fileRed, model.PbrState(model.AlphaBlend, false), m.Vec4{X: 1, W: 1}},
		{"green quad", green, fileGreen, model.PbrState(model.AlphaOpaque, true), m.Vec4{Y: 1, W: 1}},
	} {
		if shader := strings.TrimSpace(c.got.shader); shader != "fade" {
			t.Errorf("the %s drew with %.40q, want the tag's shader", c.name, shader)
		}
		if c.got.param("a").X != 7 {
			t.Errorf("the %s bound a = %v, want the tag's 7", c.name, c.got.param("a").X)
		}
		if c.got.state != c.state || c.file.state != c.state {
			t.Errorf("the %s drew with state %+v under the tag and %+v without, want the file's %+v",
				c.name, c.got.state, c.file.state, c.state)
		}
		if got := c.got.param("baseColorFactor"); got != c.baseColour {
			t.Errorf("the %s drew with baseColorFactor %v, want the file's %v", c.name, got, c.baseColour)
		}
	}
	h.noErrors(t)
}

// What a tag does set wins: its state over the file's, and its params over the
// file's by name, the Params Component over both. Two tags of one Material
// each resolve their own params, so the shadow pass's tint is not the forward
// pass's.
func TestATagsStateAndParamsWinOverTheFile(t *testing.T) {
	h := newCameralessHarness(t, 256)
	h.spawn(t, spawnRequest{Place: defaultEye, Camera: &ecsscene.Camera{
		FovY: 1.0472, Near: 0.1, Far: 200, Passes: m.NewList(
			ecsscene.Pass{ClearDepth: m.Some[float32](1)},
			ecsscene.Pass{Tag: "shadow", Order: 1},
		),
	}})
	blue := gfx.ColorParam("baseColorFactor", m.Color{B: 1, A: 1})
	h.spawn(t, spawnRequest{
		Place: m.At(0, 0, 0), Model: &ecsscene.Model{Ref: model.ModelRef{Path: paintedModel}},
		Material: &ecsscene.Material{Tags: m.NewList(
			ecsscene.MaterialTag{Shader: gfx.ShaderWithText("fade"), State: gfx.StateOpaque3D(), Params: m.NewList(blue)},
			ecsscene.MaterialTag{Tag: "shadow", Shader: gfx.ShaderWithText("shadow")},
		)},
		Params: &ecsscene.Params{Values: m.NewList(gfx.FloatParam("metallicFactor", 0.25))},
	})

	h.frameUntil(t, "the painted model to become resident", func() bool { return len(h.drawn()) == 4 })

	for _, instance := range where(h.drawn(), ofTriangle) {
		metallic := instance.param("metallicFactor").X
		switch instance.pass.Label {
		case "scene.camera0.forward":
			if instance.state != gfx.StateOpaque3D() {
				t.Errorf("the forward tag drew with %+v, want its own opaque state over the file's blend", instance.state)
			}
			if got := instance.param("baseColorFactor"); got != (m.Vec4{Z: 1, W: 1}) {
				t.Errorf("the forward tag drew with baseColorFactor %v, want its own blue", got)
			}
		case "scene.camera0.shadow":
			if instance.state != model.PbrState(model.AlphaBlend, false) {
				t.Errorf("the shadow tag drew with %+v, want the file's blend", instance.state)
			}
			if got := instance.param("baseColorFactor"); got != (m.Vec4{X: 1, W: 1}) {
				t.Errorf("the shadow tag drew with baseColorFactor %v, want the file's red", got)
			}
		default:
			t.Errorf("a draw landed in pass %q", instance.pass.Label)
		}
		if metallic != 0.25 {
			t.Errorf("the %s draw has metallicFactor %v, want the Params Component's 0.25",
				instance.pass.Label, metallic)
		}
	}
	h.noErrors(t)
}

// skinAware is a caller's shader that declares the skin buffers only under
// SCENE_SKIN, as a shader over VertexStagePath does. The stand-in reflection
// narrows a module naming sceneInstances to the bindings its text names, so
// which of group 2 the draw bound is which variant it was compiled under.
const skinAware = "sceneFrame sceneInstances sceneAnim sceneMeshes\n" +
	"//#if SCENE_SKIN\nscenePoses sceneSkinJoints\n//#endif\n"

// The variant is cog's job whatever shader is in effect: a caller's shader
// over the animated crate is compiled with SCENE_SKIN and gets the pose
// buffers, and the same shader over the static crate is compiled without it
// and declares nothing it would not be given.
func TestTheVariantIsAddedToACallersShader(t *testing.T) {
	h := newDrawingHarness(t, 256)
	material := &ecsscene.Material{Tags: m.NewList(ecsscene.MaterialTag{Shader: gfx.ShaderWithText(skinAware)})}
	h.spawn(t, spawnRequest{Place: m.At(-3, 0, 0), Model: &ecsscene.Model{Ref: model.ModelRef{Path: animatedModel}},
		Material: material, Animation: &ecsscene.Animation{Plays: [model.MaxClipPlays]model.ClipPlay{{Clip: "Walk", Weight: 1}}}})
	h.spawn(t, spawnRequest{Place: m.At(3, 0, 0), Model: crateModelComponent(), Material: material})

	h.frameUntil(t, "both crates to become resident", func() bool { return len(h.drawn()) == 2 })

	for x, skinned := range map[float32]bool{-3: true, 3: false} {
		drawn := where(h.drawn(), at(m.Vec3{X: x}))
		if len(drawn) != 1 {
			t.Fatalf("the crate at x=%v drew %d instances, want 1", x, len(drawn))
		}
		_, poses := drawn[0].buffers["scenePoses"]
		_, joints := drawn[0].buffers["sceneSkinJoints"]
		if poses != skinned || joints != skinned {
			t.Errorf("the crate at x=%v bound poses %v and joints %v, want %v: the variant is its geometry's",
				x, poses, joints, skinned)
		}
	}
	h.noErrors(t)
}

// The default scene shader is what every draw uses when nothing it names sets
// one: a Mesh and a Model with no Material, and a tag that sets no shader. Its
// params ride on every draw under the Entity's own, so a scene-wide binding
// needs nothing per Entity, an Entity's Params still win, and a tag with a
// shader of its own gets them too.
func TestTheDefaultSceneShaderFeedsEveryDrawThatNamesNone(t *testing.T) {
	h := newDrawingHarness(t, 256)
	h.kernel.ExecuteCommand[defaultShaderCmd](model.SceneShaderDescr{
		Source: gfx.ShaderWithText("sight"),
		Params: []gfx.ParameterDescr{gfx.FloatParam("fade", 0.5)},
	})
	ref := h.bake(t)
	h.spawn(t, spawnRequest{Place: m.At(-6, 0, 0), Mesh: &ecsscene.Mesh{Ref: ref}})
	h.spawn(t, spawnRequest{Place: m.At(-3, 0, 0), Model: crateModelComponent()})
	h.spawn(t, spawnRequest{Place: m.At(0, 0, 0), Model: crateModelComponent(),
		Material: &ecsscene.Material{Tags: m.NewList(ecsscene.MaterialTag{Params: m.NewList(gfx.FloatParam("a", 2))})}})
	h.spawn(t, spawnRequest{Place: m.At(3, 0, 0), Model: crateModelComponent(),
		Params: &ecsscene.Params{Values: m.NewList(gfx.FloatParam("fade", 0.25))}})
	h.spawn(t, spawnRequest{Place: m.At(6, 0, 0), Model: crateModelComponent(),
		Material: &ecsscene.Material{Tags: m.NewList(ecsscene.MaterialTag{Shader: gfx.ShaderWithText("outline")})}})

	h.frameUntil(t, "the crates to become resident", func() bool { return len(h.drawn()) == 5 })

	for _, c := range []struct {
		x      float32
		shader string
		fade   float32
	}{{-6, "sight", 0.5}, {-3, "sight", 0.5}, {0, "sight", 0.5}, {3, "sight", 0.25}, {6, "outline", 0.5}} {
		drawn := where(h.drawn(), at(m.Vec3{X: c.x}))
		if len(drawn) != 1 {
			t.Fatalf("the Entity at x=%v drew %d instances, want 1", c.x, len(drawn))
		}
		if shader := strings.TrimSpace(drawn[0].shader); shader != c.shader {
			t.Errorf("the Entity at x=%v drew with %.40q, want %q", c.x, shader, c.shader)
		}
		if got := drawn[0].param("fade").X; got != c.fade {
			t.Errorf("the Entity at x=%v bound fade %v, want %v", c.x, got, c.fade)
		}
	}
	if got := where(h.drawn(), at(m.Vec3{}))[0].param("a").X; got != 2 {
		t.Errorf("the shaderless tag bound a = %v, want its own 2 beside the default's", got)
	}

	// Unsetting it is the bundled PBR again, from the next frame, with the
	// models already resident: nothing was baked into them.
	h.kernel.ExecuteCommand[defaultShaderCmd](model.SceneShaderDescr{})
	h.frame(t)
	for _, x := range []float32{-6, -3} {
		drawn := where(h.drawn(), at(m.Vec3{X: x}))
		if len(drawn) != 1 || !strings.Contains(drawn[0].shader, "sceneInstances") {
			t.Errorf("the Entity at x=%v did not go back to the bundled PBR", x)
		}
	}
	h.noErrors(t)
}

// overlayParams is the merge every resolution is made of: later params replace
// earlier ones of the same name in place, and a new name is appended, so the
// file's order survives and nothing is bound twice.
func TestOverlayParamsReplacesByNameAndAppendsTheRest(t *testing.T) {
	arena := []gfx.ParameterDescr{gfx.FloatParam("before", 9)}
	start := len(arena)
	arena = append(arena, gfx.FloatParam("a", 1), gfx.FloatParam("b", 2))
	arena = overlayParams(arena, start, []gfx.ParameterDescr{gfx.FloatParam("b", 3), gfx.FloatParam("c", 4)})
	arena = overlayParams(arena, start, []gfx.ParameterDescr{gfx.FloatParam("a", 5)})

	var got []string
	for _, param := range arena[start:] {
		value, _ := param.FloatValue()
		got = append(got, param.Name()+"="+string(rune('0'+int(value))))
	}
	if want := "a=5 b=3 c=4"; strings.Join(got, " ") != want {
		t.Errorf("the overlay is %v, want %s", got, want)
	}
	if value, _ := arena[0].FloatValue(); arena[0].Name() != "before" || value != 9 {
		t.Errorf("the overlay reached outside its window: %v", arena[0].Name())
	}
}
