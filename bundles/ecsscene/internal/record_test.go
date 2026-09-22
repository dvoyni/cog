package internal

import (
	"encoding/binary"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecsscene"
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// Every test here asserts on what the recording backend received: the records
// scene packed for an instance, the pass it landed in, and the pipeline and
// parameters it drew with. Which Entity a draw belongs to is read off where
// its instance stands, so no test depends on how scene batched the frame.

// TestAModelEntityRecordsWhereItStandsOnItsLayers is the tracer bullet: an
// Entity with a Transform and a Model naming a glTF path is drawn, with nothing
// registered in advance — no manifest, no hash. Its instance stands where its
// Transform says, and its layers are the ones the camera's cull mask reads: a
// crate on a layer the camera does not draw is not drawn.
func TestAModelEntityRecordsWhereItStandsOnItsLayers(t *testing.T) {
	h := newCameralessHarness(t, 256)
	h.spawn(t, spawnRequest{Place: defaultEye, Camera: &ecsscene.Camera{
		FovY: 1.0472, Near: 0.1, Far: 200, CullMask: ecsscene.Layer(3),
	}})
	place := m.Transform{
		Position: m.Vec3{X: 1, Y: 2, Z: 3},
		Rotation: m.Quat{Y: 1},
		Scale:    m.Vec3{X: 2, Y: 1, Z: 0.5},
	}
	h.spawn(t, spawnRequest{Place: place, Model: &ecsscene.Model{
		Ref: model.ModelRef{Path: crateModel}, Layers: ecsscene.Layer(3),
	}})
	h.spawn(t, spawnRequest{Place: m.At(-5, 0, 0), Model: &ecsscene.Model{
		Ref: model.ModelRef{Path: crateModel}, Layers: ecsscene.Layer(5),
	}})

	h.frameUntil(t, "the crate to become resident", func() bool {
		return len(where(h.drawn(), ofTriangle)) > 0
	})

	crates := where(h.drawn(), at(place.Position))
	if len(crates) != 1 {
		t.Fatalf("the crate at %v drew %d instances, want 1; the frame drew at %v",
			place.Position, len(crates), positions(h.drawn()))
	}
	if want := place.Mat4(); !nearMat4(crates[0].world, want) {
		t.Errorf("the instance stands at\n%v\nwant\n%v", crates[0].world, want)
	}
	if hidden := where(h.drawn(), at(m.Vec3{X: -5})); len(hidden) != 0 {
		t.Errorf("the crate on a layer the camera does not draw drew %d instances", len(hidden))
	}
	h.noErrors(t)
}

// TestAModelsSceneAndNodeSelectorsReachTheDraw is the rest of scene.ModelRef:
// the Component holds scene's own reference, so a draw of one node inside a
// file is the same Component with two more strings set. The file's default
// scene is the barrel alone and its "props" scene the crate and the barrel, so
// only both selectors together draw the crate's triangle and nothing else.
func TestAModelsSceneAndNodeSelectorsReachTheDraw(t *testing.T) {
	h := newDrawingHarness(t, 256)
	selected, whole := m.At(3, 0, 0), m.At(-5, 0, 0)
	h.spawn(t, spawnRequest{Place: selected, Model: &ecsscene.Model{
		Ref: model.ModelRef{Path: propsModel, Scene: "props", Node: "crate"},
	}})
	h.spawn(t, spawnRequest{Place: whole, Model: &ecsscene.Model{Ref: model.ModelRef{Path: propsModel}}})

	h.frameUntil(t, "the props to become resident", func() bool {
		return len(where(h.drawn(), at(selected.Position))) > 0
	})

	drawn := where(h.drawn(), at(selected.Position))
	if got := len(where(drawn, ofTriangle)); got != 1 {
		t.Errorf("the selected node drew %d instances of the crate's triangle, want 1", got)
	}
	if got := len(where(drawn, ofQuad)); got != 0 {
		t.Errorf("the selected node drew %d instances of the barrel, which is outside the selection", got)
	}
	// The unselected draw of the same file is the control: the barrel is
	// what its default scene holds, which is what shows ofQuad finds it.
	control := where(h.drawn(), at(whole.Position))
	if len(where(control, ofQuad)) != 1 || len(where(control, ofTriangle)) != 0 {
		t.Errorf("the file's default scene drew %d barrels and %d crates, want the barrel alone",
			len(where(control, ofQuad)), len(where(control, ofTriangle)))
	}
	h.noErrors(t)
}

// playRecord is one play of a sceneAnim block: the two pose rows the play
// blends between and their folded weights.
type playRecord struct {
	row0, row1 uint32
	w0, w1     float32
}

// playsOf decodes the play records of an instance's sceneAnim block.
func playsOf(t *testing.T, instance drawnInstance) []playRecord {
	t.Helper()
	if instance.animOffset == sceneNoAnim || len(instance.anim) < animHeaderSize {
		t.Fatalf("the instance at %v carries no sceneAnim block", instance.position())
	}
	count := int(binary.LittleEndian.Uint32(instance.anim))
	if len(instance.anim) < animHeaderSize+count*playRecordSize {
		t.Fatalf("the sceneAnim block claims %d plays and holds %d bytes", count, len(instance.anim))
	}
	plays := make([]playRecord, count)
	for i := range plays {
		at := animHeaderSize + i*playRecordSize
		plays[i] = playRecord{
			row0: binary.LittleEndian.Uint32(instance.anim[at:]),
			row1: binary.LittleEndian.Uint32(instance.anim[at+4:]),
			w0:   floatAt(instance.anim, at+8),
			w1:   floatAt(instance.anim, at+12),
		}
	}
	return plays
}

// TestAnimationSkipsEmptySlots is the fixed array meeting scene's slice: an
// empty clip name is an unused slot wherever it sits, and the plays that
// remain reach the GPU in slot order with the time, loop and weight the game
// wrote.
//
// The sceneAnim block is what carries them. A play is two pose rows of one clip
// and two weights, so the time shows as how far a play's row sits past the
// same clip played at time zero - scene samples at 60 frames a second - and
// the weight as the play's share of the normalised total.
func TestAnimationSkipsEmptySlots(t *testing.T) {
	h := newDrawingHarness(t, 256)
	animated := &ecsscene.Model{Ref: model.ModelRef{Path: animatedModel}}
	// Walk loops, so 1.25 seconds into a one-second clip is a quarter in;
	// Idle does not, so two seconds in holds its last frame.
	gapped := [model.MaxClipPlays]model.ClipPlay{
		{Clip: "Walk", Time: 1.25, Loop: true, Weight: 1},
		{},
		{Clip: "Idle", Time: 2, Weight: 0.5},
	}
	dense := [model.MaxClipPlays]model.ClipPlay{gapped[0], gapped[2]}
	atZero := [model.MaxClipPlays]model.ClipPlay{{Clip: "Walk", Weight: 1}, {Clip: "Idle", Weight: 1}}
	h.spawn(t, spawnRequest{Place: m.At(0, 0, 0), Model: animated, Animation: &ecsscene.Animation{Plays: gapped}})
	h.spawn(t, spawnRequest{Place: m.At(3, 0, 0), Model: animated, Animation: &ecsscene.Animation{Plays: dense}})
	h.spawn(t, spawnRequest{Place: m.At(6, 0, 0), Model: animated, Animation: &ecsscene.Animation{Plays: atZero}})
	h.spawn(t, spawnRequest{Place: m.At(9, 0, 0), Model: animated})

	h.frameUntil(t, "the animated model to become resident", func() bool {
		return len(where(h.drawn(), ofTriangle)) == 4
	})

	one := func(x float32) drawnInstance {
		found := where(h.drawn(), at(m.Vec3{X: x}))
		if len(found) != 1 {
			t.Fatalf("the Entity at x=%v drew %d instances, want 1", x, len(found))
		}
		return found[0]
	}
	plays, reference := playsOf(t, one(0)), playsOf(t, one(6))
	if len(plays) != 2 || len(reference) != 2 {
		t.Fatalf("the gapped Entity packed %d plays and the reference %d, want 2 and 2", len(plays), len(reference))
	}
	if got := plays[0].row0 - reference[0].row0; got != 15 {
		t.Errorf("the first play sits %d rows into Walk, want 15: a quarter second of a looped clip", got)
	}
	if got := plays[1].row0 - reference[1].row0; got != 60 {
		t.Errorf("the second play sits %d rows into Idle, want 60: the clamped end of a one-second clip", got)
	}
	if got := plays[0].w0 + plays[0].w1; math.Abs(float64(got)-2.0/3) > 1e-5 {
		t.Errorf("Walk weighs %v of the blend, want 2/3", got)
	}
	if got := plays[1].w0 + plays[1].w1; math.Abs(float64(got)-1.0/3) > 1e-5 {
		t.Errorf("Idle weighs %v of the blend, want 1/3", got)
	}
	if !reflect.DeepEqual(plays, playsOf(t, one(3))) {
		t.Errorf("the gapped slots packed %+v, and the same plays in adjacent slots %+v", plays, playsOf(t, one(3)))
	}
	if still := one(9); still.animOffset != sceneNoAnim {
		t.Errorf("the Entity with no Animation carries sceneAnim block %d, want the rest pose", still.animOffset)
	}
	h.noErrors(t)
}

// TestParamsReachAMeshsParamsAndAModelsOverrides is one Component meaning the
// one thing scene means by a per-draw parameter on each kind of draw: a mesh
// binds it beside its material, which is where gfx packs it into the draw's
// uniform block, and a model merges it by name over the file's own material
// record.
func TestParamsReachAMeshsParamsAndAModelsOverrides(t *testing.T) {
	h := newDrawingHarness(t, 256)
	ref := h.bake(t)
	tint := gfx.ColorParam("baseColorFactor", m.Color{R: 1, A: 1})
	fade := gfx.FloatParam("fade", 0.5)
	h.spawn(t, spawnRequest{
		Place:  m.At(-3, 0, 0),
		Mesh:   &ecsscene.Mesh{Ref: ref},
		Params: &ecsscene.Params{Values: m.NewList(tint, fade)},
	})
	h.spawn(t, spawnRequest{
		Place:  m.At(3, 0, 0),
		Model:  &ecsscene.Model{Ref: model.ModelRef{Path: crateModel}},
		Params: &ecsscene.Params{Values: m.NewList(tint)},
	})

	h.frameUntil(t, "the crate to become resident", func() bool {
		return len(where(h.drawn(), at(m.Vec3{X: 3}))) > 0
	})

	meshes := where(h.drawn(), at(m.Vec3{X: -3}))
	if len(meshes) != 1 {
		t.Fatalf("the mesh drew %d instances, want 1", len(meshes))
	}
	if got := meshes[0].param("baseColorFactor"); got != (m.Vec4{X: 1, W: 1}) {
		t.Errorf("the mesh draw bound baseColorFactor %v, want the tint", got)
	}
	if got := meshes[0].param("fade"); got.X != 0.5 {
		t.Errorf("the mesh draw bound fade %v, want 0.5", got.X)
	}
	models := where(h.drawn(), at(m.Vec3{X: 3}))
	if len(models) != 1 {
		t.Fatalf("the model drew %d instances, want 1", len(models))
	}
	if len(models[0].material) < 16 {
		t.Fatalf("the model draw bound a %d-byte material record", len(models[0].material))
	}
	if got := vec4At(models[0].material, 0); got != (m.Vec4{X: 1, W: 1}) {
		t.Errorf("the model's material record has baseColorFactor %v, want the tint over the file's white", got)
	}
	if got := models[0].param("fade"); got != (m.Vec4{}) {
		t.Errorf("the model draw bound fade %v, which only the mesh's Params named", got)
	}
	h.noErrors(t)
}

// passView is what one draw drew with in one pass: the shader, the state and
// the parameters its tag named.
type passView struct {
	Shader string
	State  gfx.MaterialState
	A, B   float32
	C, D   float32
}

func viewOf(instance drawnInstance) passView {
	return passView{
		Shader: strings.TrimSpace(instance.shader), State: instance.state,
		A: instance.param("a").X, B: instance.param("b").X,
		C: instance.param("c").X, D: instance.param("d").X,
	}
}

// TestAMaterialsTagsEachKeepTheirOwnParams is the scratch rule that has a
// reason: gfx keeps the params slice a descriptor is built around, so a tag
// rebuilt over params another tag of the same draw still points at would bind
// that other tag's values. Every tag here carries a different count, and a
// second Entity with another material follows in the same frame. The camera
// has a pass for each tag, so each tag's pipeline and parameters are what
// that pass drew the Entity with.
func TestAMaterialsTagsEachKeepTheirOwnParams(t *testing.T) {
	h := newCameralessHarness(t, 256)
	h.spawn(t, spawnRequest{Place: defaultEye, Camera: &ecsscene.Camera{
		FovY: 1.0472, Near: 0.1, Far: 200, Passes: m.NewList(
			ecsscene.Pass{ClearDepth: m.Some[float32](1)},
			ecsscene.Pass{Tag: "shadow", Order: 1},
			ecsscene.Pass{Tag: "outline", Order: 2},
		),
	}})
	ref := h.bake(t)
	forward := gfx.ShaderWithText("forward")
	shadow := gfx.ShaderWithText("shadow")
	h.spawn(t, spawnRequest{Place: m.At(-3, 0, 0), Mesh: &ecsscene.Mesh{Ref: ref}, Material: &ecsscene.Material{Tags: m.NewList(
		ecsscene.MaterialTag{Shader: forward, State: gfx.StateOpaque3D(), Params: m.NewList(
			gfx.FloatParam("a", 1), gfx.FloatParam("b", 2))},
		ecsscene.MaterialTag{Tag: "shadow", Shader: shadow, State: gfx.StateTransparent3D(), Params: m.NewList(
			gfx.FloatParam("c", 3))},
		ecsscene.MaterialTag{Tag: "outline", Shader: forward},
	)}})
	h.spawn(t, spawnRequest{Place: m.At(3, 0, 0), Mesh: &ecsscene.Mesh{Ref: ref, Layers: ecsscene.Layer(1)}, Material: &ecsscene.Material{Tags: m.NewList(
		ecsscene.MaterialTag{Shader: shadow, Params: m.NewList(gfx.FloatParam("d", 4))},
	)}})

	h.frameUntil(t, "the frame to draw", func() bool { return len(h.drawn()) > 0 })

	views := func(x float32) map[string]passView {
		out := map[string]passView{}
		for _, instance := range where(h.drawn(), at(m.Vec3{X: x})) {
			out[instance.pass.Label] = viewOf(instance)
		}
		return out
	}
	// The tag with no state names none, which reaches gfx as the zero state:
	// what the pipeline was built with is what the second Entity's only tag
	// reads back as too.
	want := map[string]passView{
		"scene.camera0.forward": {Shader: "forward", State: gfx.StateOpaque3D(), A: 1, B: 2},
		"scene.camera0.shadow":  {Shader: "shadow", State: gfx.StateTransparent3D(), C: 3},
		"scene.camera0.outline": {Shader: "forward", State: gfx.MaterialState{}},
	}
	if got := views(-3); !reflect.DeepEqual(got, want) {
		t.Errorf("the three-tag material drew as\n%+v\nwant\n%+v", got, want)
	}
	wantOther := map[string]passView{"scene.camera0.forward": {Shader: "shadow", State: gfx.MaterialState{}, D: 4}}
	if got := views(3); !reflect.DeepEqual(got, wantOther) {
		t.Errorf("the second Entity's material drew as %+v, want %+v", got, wantOther)
	}
	h.noErrors(t)
}

// TestAnAbsentMaterialIsNoMaterial is scene's nil on both kinds of draw: the
// bundled PBR for a mesh and the file's own materials for a model. An Entity
// with a Material beside it in the same frame shows the scratch does not leak
// one draw's material into the next.
func TestAnAbsentMaterialIsNoMaterial(t *testing.T) {
	h := newDrawingHarness(t, 256)
	ref := h.bake(t)
	material := &ecsscene.Material{Tags: m.NewList(ecsscene.MaterialTag{Shader: gfx.ShaderWithText("flat")})}
	h.spawn(t, spawnRequest{Place: m.At(-3, 0, 0), Mesh: &ecsscene.Mesh{Ref: ref, Layers: ecsscene.Layer(1)}, Material: material})
	h.spawn(t, spawnRequest{Place: m.At(-1, 0, 0), Mesh: &ecsscene.Mesh{Ref: ref}})
	h.spawn(t, spawnRequest{Place: m.At(1, 0, 0), Model: crateModelComponent(), Material: material})
	h.spawn(t, spawnRequest{Place: m.At(3, 0, 0), Model: crateModelComponent()})

	h.frameUntil(t, "the crate to become resident", func() bool {
		return len(where(h.drawn(), at(m.Vec3{X: 3}))) > 0
	})

	for x, flat := range map[float32]bool{-3: true, -1: false, 1: true, 3: false} {
		drawn := where(h.drawn(), at(m.Vec3{X: x}))
		if len(drawn) != 1 {
			t.Errorf("the Entity at x=%v drew %d instances, want 1", x, len(drawn))
			continue
		}
		shader := strings.TrimSpace(drawn[0].shader)
		if got := shader == "flat"; got != flat {
			t.Errorf("the Entity at x=%v drew with shader %.40q; with a Material: %v", x, shader, flat)
		}
		if !flat && !strings.Contains(shader, "sceneInstances") {
			t.Errorf("the Entity at x=%v has no Material and drew with %.40q, not the bundled shader", x, shader)
		}
	}
	h.noErrors(t)
}

// light is one packed light of a sceneFrame block.
type light struct {
	position, direction, color m.Vec3
	invRange4                  float32
	spotScale, spotOffset      float32
}

// lightsOf decodes the lights a pass's sceneFrame block carries.
func lightsOf(t *testing.T, frame []byte) []light {
	t.Helper()
	if len(frame) < frameLightsAt {
		t.Fatalf("the sceneFrame block is %d bytes", len(frame))
	}
	count := int(binary.LittleEndian.Uint32(frame[frameLightCountAt:]))
	lights := make([]light, count)
	for i := range lights {
		at := frameLightsAt + i*lightRecordSize
		vec3 := func(offset int) m.Vec3 {
			v := vec4At(frame, at+offset)
			return m.Vec3{X: v.X, Y: v.Y, Z: v.Z}
		}
		lights[i] = light{
			position: vec3(0), invRange4: floatAt(frame, at+12),
			direction: vec3(16), spotScale: floatAt(frame, at+28),
			color: vec3(32), spotOffset: floatAt(frame, at+44),
		}
	}
	return lights
}

// TestALightIsPlacedAndAimedByItsTransform is the one place the binding
// computes anything: a spot's direction is its Transform's rotation applied to
// -Z, which is the way m.LookAt faces, so a light placed with LookAt shines
// at what it looks at. An unrotated spot shines down -Z, and a point light
// takes its position alone. A light is among a pass.s lights only when its
// layers are ones the camera draws.
func TestALightIsPlacedAndAimedByItsTransform(t *testing.T) {
	h := newCameralessHarness(t, 256)
	h.spawn(t, spawnRequest{Place: defaultEye, Camera: &ecsscene.Camera{
		FovY: 1.0472, Near: 0.1, Far: 200, CullMask: ecsscene.Layer(2),
	}})
	// A second camera draws layer 3 alone, which is what tells a light the
	// binding left on layer 2 from one it dropped to zero, which is every layer.
	h.spawn(t, spawnRequest{Place: defaultEye, Camera: &ecsscene.Camera{
		ID: 1, FovY: 1.0472, Near: 0.1, Far: 200, CullMask: ecsscene.Layer(3),
	}})
	ref := h.bake(t)
	h.spawn(t, spawnRequest{Mesh: &ecsscene.Mesh{Ref: ref, NeverCull: true}})
	eye, target := m.Vec3{Y: 5}, m.Vec3{X: 3, Y: 5, Z: 4}
	h.spawn(t, spawnRequest{
		Place: m.LookAt(eye, target, m.Vec3{Y: 1}),
		Light: &ecsscene.Light{
			Descr: model.LightDescr{
				Kind: model.LightSpot, Color: m.Color{R: 1, G: 0.5, A: 1}, Intensity: 2, Range: 10,
				InnerCone: 0.1, OuterCone: 0.4,
			},
			Layers: ecsscene.Layer(2),
		},
	})
	h.spawn(t, spawnRequest{Place: m.Transform{Position: m.Vec3{Z: 7}}, Light: &ecsscene.Light{Descr: model.LightDescr{Kind: model.LightSpot}}})
	h.spawn(t, spawnRequest{Place: m.Transform{Position: m.Vec3{X: -2}}, Light: &ecsscene.Light{Descr: model.LightDescr{Range: 3}}})
	h.spawn(t, spawnRequest{Place: m.Transform{Position: m.Vec3{X: 2}}, Light: &ecsscene.Light{Layers: ecsscene.Layer(3)}})

	h.frameUntil(t, "the frame to draw", func() bool { return len(h.drawn()) > 0 })

	other := where(h.drawn(), inPass("scene.camera1.forward"))
	if len(other) == 0 {
		t.Fatal("the layer-3 camera drew nothing")
	}
	for _, got := range lightsOf(t, other[0].frame) {
		if got.position == eye {
			t.Errorf("the layer-3 camera carries the spot on layer 2: %+v", got)
		}
	}
	layer2 := where(h.drawn(), inPass("scene.camera0.forward"))
	if len(layer2) == 0 {
		t.Fatal("the layer-2 camera drew nothing")
	}
	lights := lightsOf(t, layer2[0].frame)
	if len(lights) != 3 {
		t.Fatalf("the pass carries %d lights, want the 3 on layers the camera draws: %+v", len(lights), lights)
	}
	cone := func(inner, outer float64) (scale, offset float32) {
		scale = float32(1 / (math.Cos(inner) - math.Cos(outer)))
		return scale, -float32(math.Cos(outer)) * scale
	}
	for _, got := range lights {
		switch got.position {
		case eye:
			if !nearVec3(got.direction, m.Vec3{X: 0.6, Z: 0.8}) {
				t.Errorf("the aimed spot shines along %v, want (0.6, 0, 0.8)", got.direction)
			}
			scale, offset := cone(0.1, 0.4)
			if got.color != (m.Vec3{X: 2, Y: 1}) || !nearFloat(got.invRange4, 1e-4) ||
				!nearFloat(got.spotScale, scale) || !nearFloat(got.spotOffset, offset) {
				t.Errorf("the aimed spot reached the GPU as %+v, want colour (2, 1, 0), 1/range^4 1e-4 and cone %v, %v",
					got, scale, offset)
			}
		case m.Vec3{Z: 7}:
			if !nearVec3(got.direction, m.Vec3{Z: -1}) {
				t.Errorf("the unrotated spot shines along %v, want -Z", got.direction)
			}
			if got.spotScale == 0 {
				t.Errorf("the unrotated spot reached the GPU with no cone: %+v", got)
			}
		case m.Vec3{X: -2}:
			if got.direction != (m.Vec3{}) || got.spotScale != 0 || got.spotOffset != 1 || !nearFloat(got.invRange4, 1.0/81) {
				t.Errorf("the point light reached the GPU as %+v, want no cone and 1/range^4 of range 3", got)
			}
		default:
			t.Errorf("a light stands at %v", got.position)
		}
	}
	h.noErrors(t)
}

func nearVec3(a, b m.Vec3) bool {
	const epsilon = 1e-5
	d := a.Sub(b)
	return d.X*d.X+d.Y*d.Y+d.Z*d.Z < epsilon*epsilon
}

func nearFloat(a, b float32) bool {
	return math.Abs(float64(a-b)) <= 1e-5*math.Max(1, math.Abs(float64(b)))
}

func nearMat4(a, b m.Mat4) bool {
	for i := range a {
		if !nearFloat(a[i], b[i]) {
			return false
		}
	}
	return true
}

// The sceneFrame block's layout, past the three matrices: the eye, the view
// direction, the sun and the two ambients, one vec4 each.
const (
	frameViewAt          = 0
	frameProjectionAt    = 64
	frameCameraAt        = 192
	frameSunDirectionAt  = 224
	frameSunColorAt      = 240
	frameAmbientSkyAt    = 256
	frameAmbientGroundAt = 272
)

// TestACameraRecordsItsPassesWithTheirClears is the List of scene.Pass: each
// pass's clears are m.Maybe values, so "clear to this colour" and "preserve"
// are both a value a Component can hold. The camera is placed by its
// Transform, its lens and lighting reach its passes' sceneFrame blocks, its
// cull mask decides what they draw, and a Camera whose Passes are empty gets
// scene's default pass.
func TestACameraRecordsItsPassesWithTheirClears(t *testing.T) {
	h := newCameralessHarness(t, 256)
	place := m.LookAt(m.Vec3{Y: 3, Z: 10}, m.Vec3{}, m.Vec3{Y: 1})
	h.spawn(t, spawnRequest{Place: place, Camera: &ecsscene.Camera{
		ID: -2, Projection: ecsscene.Perspective, FovY: 1, Near: 0.1, Far: 100,
		CullMask: ecsscene.Layer(4), SunDirection: m.Vec3{Y: -1}, SunColor: m.White, SunIntensity: 3,
		AmbientSky: m.Color{B: 1, A: 1}, AmbientGround: m.Color{G: 1, A: 1}, AmbientIntensity: 0.5,
		Passes: m.NewList(
			ecsscene.Pass{ClearColor: m.Some(m.Color{B: 0.25, A: 1}), ClearDepth: m.Some[float32](1)},
			ecsscene.Pass{Tag: "overlay", Order: 1},
		),
	}})
	obliquePlace := m.At(0, 0, 20)
	h.spawn(t, spawnRequest{Place: obliquePlace, Camera: &ecsscene.Camera{
		ID: 5, Projection: ecsscene.Oblique, Height: 20, Shear: 0.5, Near: -50, Far: 50,
	}})
	// Two meshes every pass has a tag for, one on the layer the first camera
	// draws and one off it.
	ref := h.bake(t)
	material := &ecsscene.Material{Tags: m.NewList(
		ecsscene.MaterialTag{Shader: gfx.ShaderWithText("flat")},
		ecsscene.MaterialTag{Tag: "overlay", Shader: gfx.ShaderWithText("flat")},
	)}
	h.spawn(t, spawnRequest{Place: m.At(-1, 0, 0), Mesh: &ecsscene.Mesh{Ref: ref, Layers: ecsscene.Layer(4), NeverCull: true}, Material: material})
	h.spawn(t, spawnRequest{Place: m.At(1, 0, 0), Mesh: &ecsscene.Mesh{Ref: ref, Layers: ecsscene.Layer(1), NeverCull: true}, Material: material})

	h.frameUntil(t, "the frame to draw", func() bool { return len(h.drawn()) > 0 })

	const forward, overlay, oblique = "scene.camera-2.forward", "scene.camera-2.overlay", "scene.camera5.forward"
	passes := map[string]int{}
	for i, pass := range h.backend.passes() {
		passes[pass.desc.Label] = i
	}
	first, second, third := passes[forward], passes[overlay], passes[oblique]
	if len(passes) != 3 || !(first < second && second < third) {
		t.Fatalf("the frame's passes are %v, want %q, %q and %q in that order", passes, forward, overlay, oblique)
	}
	descs := h.backend.passes()
	if d := descs[first].desc; d.Load != gfx.LoadClear || d.Clear != (m.Color{B: 0.25, A: 1}) ||
		d.DepthLoad != gfx.LoadClear || d.DepthClear != 1 {
		t.Errorf("the first pass reached the GPU as %+v, want colour cleared to the Component's and depth to 1", d)
	}
	if d := descs[second].desc; d.Load == gfx.LoadClear || d.DepthLoad == gfx.LoadClear {
		t.Errorf("the second pass clears, and its Component said preserve: %+v", d)
	}
	if d := descs[third].desc; d.Load == gfx.LoadClear || d.DepthLoad != gfx.LoadClear || d.DepthClear != 1 {
		t.Errorf("the pass-less camera's default pass reached the GPU as %+v, want colour kept and depth cleared", d)
	}

	for label, want := range map[string][]float32{forward: {-1}, overlay: {-1}, oblique: {-1, 1}} {
		drawn := where(h.drawn(), inPass(label))
		for _, x := range want {
			if len(where(drawn, at(m.Vec3{X: x}))) != 1 {
				t.Errorf("%s drew at %v, want the mesh at x=%v", label, positions(drawn), x)
			}
		}
		if len(drawn) != len(want) {
			t.Errorf("%s drew at %v, want only x=%v", label, positions(drawn), want)
		}
	}

	aspect := float32(1600) / 1200
	view, _ := place.Mat4().Inverse()
	frame := where(h.drawn(), inPass(forward))[0].frame
	expectMat4(t, "view", mat4At(frame, frameViewAt), view)
	expectMat4(t, "projection", mat4At(frame, frameProjectionAt), m.Perspective4(1, aspect, 0.1, 100))
	for what, want := range map[string]struct {
		at   int
		want m.Vec4
	}{
		"eye":           {frameCameraAt, m.Vec4{Y: 3, Z: 10, W: 1}},
		"sun direction": {frameSunDirectionAt, m.Vec4{Y: -1}},
		"sun colour":    {frameSunColorAt, m.Vec4{X: 3, Y: 3, Z: 3}},
		"sky":           {frameAmbientSkyAt, m.Vec4{Z: 0.5}},
		"ground":        {frameAmbientGroundAt, m.Vec4{Y: 0.5}},
	} {
		if got := vec4At(frame, want.at); got != want.want {
			t.Errorf("the %s reached the GPU as %v, want %v", what, got, want.want)
		}
	}
	obliqueFrame := where(h.drawn(), inPass(oblique))[0].frame
	expectMat4(t, "oblique projection", mat4At(obliqueFrame, frameProjectionAt),
		m.Oblique4(-10*aspect, 10*aspect, -10, 10, -50, 50, 0.5))
	if got := vec4At(obliqueFrame, frameCameraAt); got != (m.Vec4{Z: 20, W: 1}) {
		t.Errorf("the oblique eye reached the GPU as %v, want (0, 0, 20)", got)
	}
	h.noErrors(t)
}

func expectMat4(t *testing.T, what string, got, want m.Mat4) {
	t.Helper()
	if !nearMat4(got, want) {
		t.Errorf("the %s reached the GPU as\n%v\nwant\n%v", what, got, want)
	}
}

// TestAnEntityWithNoTransformIsNotRecorded holds for every kind: a Transform
// is required, so a Model, Mesh, Light or Camera on an Entity with nowhere to
// stand is not a draw at the origin but no draw at all.
func TestAnEntityWithNoTransformIsNotRecorded(t *testing.T) {
	h := newDrawingHarness(t, 256)
	ref := h.bake(t)
	h.spawn(t, spawnRequest{Unplaced: true,
		Model: crateModelComponent(), Mesh: &ecsscene.Mesh{Ref: ref, NeverCull: true},
		Light: &ecsscene.Light{}, Camera: &ecsscene.Camera{ID: 1, FovY: 1, Near: 1, Far: 2},
	})
	h.spawn(t, spawnRequest{Place: m.At(5, 0, 0), Model: crateModelComponent()})

	h.frameUntil(t, "the crate to become resident", func() bool {
		return len(where(h.drawn(), at(m.Vec3{X: 5}))) > 0
	})

	if drawn := h.drawn(); len(drawn) != 1 {
		t.Errorf("the frame drew at %v, want only the placed crate", positions(drawn))
	}
	for _, pass := range h.backend.passes() {
		if strings.HasPrefix(pass.desc.Label, "scene.camera1.") {
			t.Errorf("the unplaced camera emitted pass %q", pass.desc.Label)
		}
	}
	if lights := lightsOf(t, h.drawn()[0].frame); len(lights) != 0 {
		t.Errorf("the pass carries the unplaced light: %+v", lights)
	}
	h.noErrors(t)
}

// TestADrawIsInTheFlushOfTheTickThatRecordedItWithNoOrderingDeclared is the
// ordering claim. scene subscribes its flush Last, so a recording System that
// declares nothing is in the ordinary phase and already runs before it. The
// behavioural half is one frame: an Entity spawned into a world that already
// draws is drawn by the very next frame, which is only true if its tick's
// flush saw it. The rest asserts the edge the engine derived without either
// side declaring it.
func TestADrawIsInTheFlushOfTheTickThatRecordedItWithNoOrderingDeclared(t *testing.T) {
	h := newDrawingHarness(t, 256)
	h.spawn(t, spawnRequest{Model: crateModelComponent()})
	h.frameUntil(t, "the crate to become resident", func() bool {
		return len(where(h.drawn(), ofTriangle)) > 0
	})
	h.spawn(t, spawnRequest{Place: m.At(4, 0, 0), Model: crateModelComponent()})
	h.frame(t)
	if drawn := where(h.drawn(), at(m.Vec3{X: 4})); len(drawn) != 1 {
		t.Fatalf("the frame after the spawn drew the new crate %d times, want the one recorded in its tick", len(drawn))
	}

	description := h.engine.Describe()
	var recorder, flush *kernel.SubscriptionDescription
	for i := range description.Subscriptions {
		switch description.Subscriptions[i].Type {
		case reflect.TypeFor[ecsscene.RecordOnUpdate]():
			recorder = &description.Subscriptions[i]
		case reflect.TypeFor[scene.FlushOnUpdate]():
			flush = &description.Subscriptions[i]
		}
	}
	if recorder == nil || flush == nil {
		t.Fatalf("the description lists %d subscriptions and not both halves", len(description.Subscriptions))
	}
	if recorder.Phase != "ordinary" {
		t.Errorf("the recording System is in phase %q, want the ordinary one it never asked to leave", recorder.Phase)
	}
	if flush.Phase != "last" {
		t.Errorf("scene's flush is in phase %q, want last: the ordering claim rests on it", flush.Phase)
	}
	if !containsType(flush.DependsOn, reflect.TypeFor[ecsscene.RecordOnUpdate]()) {
		t.Errorf("scene's flush does not wait for the recording System; it depends on %v", flush.DependsOn)
	}
	if containsType(recorder.DependsOn, reflect.TypeFor[scene.FlushOnUpdate]()) {
		t.Error("the recording System waits for scene's flush, which is the wrong way round")
	}
}

// TestTheRecordingSystemsLockSetIsItsSignature is the other thing the signature
// is: a declaration. Every Store it reads, its scratch and scene's queue, none
// of them named in a Lock func — and the scratch is in the lock set because it
// is a resource rather than something a closure captured.
func TestTheRecordingSystemsLockSetIsItsSignature(t *testing.T) {
	h := newHarness(t)
	for _, sub := range h.engine.Describe().Subscriptions {
		if sub.Type != reflect.TypeFor[ecsscene.RecordOnUpdate]() {
			continue
		}
		for _, want := range []reflect.Type{
			reflect.TypeFor[*ecs.Entities](),
			reflect.TypeFor[*ecs.Store[m.Transform]](),
			reflect.TypeFor[*ecs.Store[ecsscene.Model]](),
			reflect.TypeFor[*ecs.Store[ecsscene.Mesh]](),
			reflect.TypeFor[*ecs.Store[ecsscene.Light]](),
			reflect.TypeFor[*ecs.Store[ecsscene.Camera]](),
			reflect.TypeFor[*ecs.Store[ecsscene.Animation]](),
			reflect.TypeFor[*ecs.Store[ecsscene.Params]](),
			reflect.TypeFor[*ecs.Store[ecsscene.Material]](),
		} {
			if !containsType(sub.Reads, want) {
				t.Errorf("the System's read set %v does not name %v", sub.Reads, want)
			}
		}
		for _, want := range []reflect.Type{reflect.TypeFor[*scratch](), reflect.TypeFor[*scene.OpQueue]()} {
			if !containsType(sub.Writes, want) {
				t.Errorf("the System's write set %v does not name %v", sub.Writes, want)
			}
		}
		if containsType(sub.Writes, reflect.TypeFor[*ecs.Entities]()) {
			t.Error("the System holds the authority for write: recording is not a structural change")
		}
		return
	}
	t.Fatal("the description does not list the recording System")
}

func containsType(types []reflect.Type, want reflect.Type) bool {
	for _, t := range types {
		if t == want {
			return true
		}
	}
	return false
}

// TestEveryComponentRegistersAndMeshAndLightArePointerFree is the storage
// half. Every Component is a registered Store the moment the plugin is
// composed, and the two a game holds by the thousand with no string, List or
// Blob in them keep the ECS's fast path: sized moves, noscan spans and the
// unrolled fill.
func TestEveryComponentRegistersAndMeshAndLightArePointerFree(t *testing.T) {
	h := newHarness(t)
	owned := map[reflect.Type]kernel.PluginName{}
	for _, resource := range h.engine.Describe().Resources {
		owned[resource.Type] = resource.Owner
	}
	for component, store := range map[reflect.Type]reflect.Type{
		reflect.TypeFor[ecsscene.Model]():     reflect.TypeFor[*ecs.Store[ecsscene.Model]](),
		reflect.TypeFor[ecsscene.Mesh]():      reflect.TypeFor[*ecs.Store[ecsscene.Mesh]](),
		reflect.TypeFor[ecsscene.Animation](): reflect.TypeFor[*ecs.Store[ecsscene.Animation]](),
		reflect.TypeFor[ecsscene.Params]():    reflect.TypeFor[*ecs.Store[ecsscene.Params]](),
		reflect.TypeFor[ecsscene.Material]():  reflect.TypeFor[*ecs.Store[ecsscene.Material]](),
		reflect.TypeFor[ecsscene.Light]():     reflect.TypeFor[*ecs.Store[ecsscene.Light]](),
		reflect.TypeFor[ecsscene.Camera]():    reflect.TypeFor[*ecs.Store[ecsscene.Camera]](),
	} {
		if err := ecs.Storable(component); err != nil {
			t.Errorf("%v is not storable: %v", component, err)
		}
		if owner, ok := owned[store]; !ok || owner != ecsscene.Name {
			t.Errorf("%v is registered by %q (registered: %v), want %q", component, owner, ok, ecsscene.Name)
		}
	}
	for _, component := range []reflect.Type{reflect.TypeFor[ecsscene.Mesh](), reflect.TypeFor[ecsscene.Light]()} {
		if err := ecs.PointerFree(component); err != nil {
			t.Errorf("%v is not pointer-free: %v", component, err)
		}
	}
}
