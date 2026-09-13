package ecsscene

import (
	"reflect"
	"testing"

	"github.com/dvoyni/cog/ecs"
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/m"
	"github.com/dvoyni/cog/scene"
)

// TestAModelEntityRecordsWhereItStandsOnItsLayers is the tracer bullet: an
// Entity with a Transform and a Model naming a glTF path is drawn, with nothing
// registered in advance — no manifest, no hash.
func TestAModelEntityRecordsWhereItStandsOnItsLayers(t *testing.T) {
	h := newHarness(t)
	place := Transform{
		Position: m.Vec3{X: 1, Y: 2, Z: 3},
		Rotation: m.Quat{Y: 1},
		Scale:    m.Vec3{X: 2, Y: 1, Z: 0.5},
	}
	h.spawn(t, spawnRequest{Place: place, Model: &Model{
		Ref: scene.ModelRef{Path: crateModel}, Layers: scene.Layer(3),
	}})

	h.frame(t)

	models := h.ops(t, scene.OpModel)
	if len(models) != 1 {
		t.Fatalf("the frame recorded %d model draws, want 1", len(models))
	}
	op := models[0]
	if op.Path != crateModel {
		t.Errorf("the draw named %q, want %q", op.Path, crateModel)
	}
	if op.Layers != scene.Layer(3) {
		t.Errorf("the draw is on layers %v, want %v", op.Layers, scene.Layer(3))
	}
	want := scene.Transform{
		Position: m.Vec3{X: 1, Y: 2, Z: 3},
		Rotation: m.Quat{Y: 1},
		Scale:    m.Vec3{X: 2, Y: 1, Z: 0.5},
	}
	if op.Model.Transform != want {
		t.Errorf("the draw stands at %+v, want %+v", op.Model.Transform, want)
	}
}

// TestAModelsSceneAndNodeSelectorsReachTheDraw is the rest of scene.ModelRef:
// the Component holds scene's own reference, so a draw of one node inside a
// file is the same Component with two more strings set.
func TestAModelsSceneAndNodeSelectorsReachTheDraw(t *testing.T) {
	h := newHarness(t)
	h.spawn(t, spawnRequest{Model: &Model{
		Ref: scene.ModelRef{Path: "models/props.glb", Scene: "props", Node: "crate"},
	}})

	h.frame(t)

	models := h.ops(t, scene.OpModel)
	if len(models) != 1 {
		t.Fatalf("the frame recorded %d model draws, want 1", len(models))
	}
	got := scene.ModelRef{Path: models[0].Path, Scene: models[0].Model.Scene, Node: models[0].Model.Node}
	want := scene.ModelRef{Path: "models/props.glb", Scene: "props", Node: "crate"}
	if got != want {
		t.Fatalf("the draw selects %+v, want %+v", got, want)
	}
}

// TestAnimationSkipsEmptySlots is the fixed array meeting scene's slice: an
// empty clip name is an unused slot wherever it sits, and the plays that
// remain reach the draw in slot order with the time the game wrote.
func TestAnimationSkipsEmptySlots(t *testing.T) {
	h := newHarness(t)
	h.spawn(t, spawnRequest{
		Model: &Model{Ref: scene.ModelRef{Path: crateModel}},
		Animation: &Animation{Plays: [MaxPlays]scene.ClipPlay{
			{Clip: "Walk", Time: 0.25, Loop: true, Weight: 1},
			{},
			{Clip: "Idle", Time: 2, Weight: 0.5},
		}},
	})
	h.spawn(t, spawnRequest{Step: 1, Model: &Model{Ref: scene.ModelRef{Path: "models/still.glb"}}})

	h.frame(t)

	plays := map[string][]scene.ClipPlay{}
	for _, op := range h.ops(t, scene.OpModel) {
		plays[op.Path] = append([]scene.ClipPlay(nil), op.Model.Plays...)
	}
	want := []scene.ClipPlay{
		{Clip: "Walk", Time: 0.25, Loop: true, Weight: 1},
		{Clip: "Idle", Time: 2, Weight: 0.5},
	}
	if !reflect.DeepEqual(plays[crateModel], want) {
		t.Errorf("the animated draw plays %+v, want %+v", plays[crateModel], want)
	}
	if got, ok := plays["models/still.glb"]; !ok || len(got) != 0 {
		t.Errorf("the Entity with no Animation plays %+v (recorded: %v), want its rest pose", got, ok)
	}
}

// TestParamsReachAMeshsParamsAndAModelsOverrides is one Component meaning the
// one thing scene means by a per-draw parameter on each kind of draw: a mesh
// binds it beside its material, and a model merges it by name over the file's.
func TestParamsReachAMeshsParamsAndAModelsOverrides(t *testing.T) {
	h := newHarness(t)
	ref := h.bake(t)
	tint := gfx.ColorParam("baseColorFactor", m.Color{R: 1, A: 1})
	fade := gfx.FloatParam("fade", 0.5)
	h.spawn(t, spawnRequest{
		Mesh:   &Mesh{Ref: ref},
		Params: &Params{Values: ecs.NewList(tint, fade)},
	})
	h.spawn(t, spawnRequest{
		Model:  &Model{Ref: scene.ModelRef{Path: crateModel}},
		Params: &Params{Values: ecs.NewList(tint)},
	})

	h.frame(t)

	meshes := h.ops(t, scene.OpMesh)
	if len(meshes) != 1 {
		t.Fatalf("the frame recorded %d mesh draws, want 1", len(meshes))
	}
	if got := paramNames(meshes[0].Draw.Params); !reflect.DeepEqual(got, []string{"baseColorFactor", "fade"}) {
		t.Errorf("the mesh draw binds %v", got)
	}
	if value, _ := meshes[0].Draw.Params[1].FloatValue(); value != 0.5 {
		t.Errorf("the mesh's fade is %v, want 0.5", value)
	}
	models := h.ops(t, scene.OpModel)
	if len(models) != 1 {
		t.Fatalf("the frame recorded %d model draws, want 1", len(models))
	}
	overrides := models[0].Model.OverrideParams
	if got := paramNames(overrides); !reflect.DeepEqual(got, []string{"baseColorFactor"}) {
		t.Fatalf("the model draw overrides %v", got)
	}
	if color, _ := overrides[0].ColorValue(); color != (m.Color{R: 1, A: 1}) {
		t.Errorf("the model's tint is %v", color)
	}
}

func paramNames(params []gfx.ParameterDescr) []string {
	names := []string{}
	for _, param := range params {
		names = append(names, param.Name())
	}
	return names
}

// TestAMaterialsTagsEachKeepTheirOwnParams is the scratch rule that has a
// reason: gfx keeps the params slice a descriptor is built around, so a tag
// rebuilt over params another tag of the same draw still points at would bind
// that other tag's values. Every tag here carries a different count, and a
// second Entity with another material follows in the same frame.
func TestAMaterialsTagsEachKeepTheirOwnParams(t *testing.T) {
	h := newHarness(t)
	ref := h.bake(t)
	forward := gfx.ShaderWithText("forward")
	shadow := gfx.ShaderWithText("shadow")
	h.spawn(t, spawnRequest{Mesh: &Mesh{Ref: ref}, Material: &Material{Tags: ecs.NewList(
		MaterialTag{Shader: forward, State: gfx.StateOpaque3D, Params: ecs.NewList(
			gfx.FloatParam("a", 1), gfx.FloatParam("b", 2))},
		MaterialTag{Tag: "shadow", Shader: shadow, State: gfx.StateTransparent3D, Params: ecs.NewList(
			gfx.FloatParam("c", 3))},
		MaterialTag{Tag: "outline", Shader: forward},
	)}})
	h.spawn(t, spawnRequest{Mesh: &Mesh{Ref: ref, Layers: scene.Layer(1)}, Material: &Material{Tags: ecs.NewList(
		MaterialTag{Shader: shadow, Params: ecs.NewList(gfx.FloatParam("d", 4))},
	)}})

	h.frame(t)

	byLayers := map[scene.LayerMask]scene.Material{}
	for _, op := range h.ops(t, scene.OpMesh) {
		byLayers[op.Layers] = op.Draw.Material
	}
	type tagView struct {
		Tag    scene.PassTag
		Shader gfx.ShaderDescr
		State  gfx.MaterialState
		Params map[string]float32
	}
	view := func(material scene.Material) []tagView {
		out := []tagView{}
		for _, entry := range material {
			params := map[string]float32{}
			for _, param := range entry.Descr.Params() {
				params[param.Name()], _ = param.FloatValue()
			}
			out = append(out, tagView{entry.Tag, entry.Descr.Shader(), entry.Descr.State(), params})
		}
		return out
	}
	want := []tagView{
		{"", forward, gfx.StateOpaque3D, map[string]float32{"a": 1, "b": 2}},
		{"shadow", shadow, gfx.StateTransparent3D, map[string]float32{"c": 3}},
		{"outline", forward, gfx.MaterialState{}, map[string]float32{}},
	}
	if got := view(byLayers[0]); !reflect.DeepEqual(got, want) {
		t.Errorf("the three-tag material reached scene as\n%+v\nwant\n%+v", got, want)
	}
	wantOther := []tagView{{"", shadow, gfx.MaterialState{}, map[string]float32{"d": 4}}}
	if got := view(byLayers[scene.Layer(1)]); !reflect.DeepEqual(got, wantOther) {
		t.Errorf("the second Entity's material reached scene as %+v, want %+v", got, wantOther)
	}
}

// TestAnAbsentMaterialIsNoMaterial is scene's nil on both kinds of draw: the
// bundled PBR for a mesh and the file's own materials for a model. An Entity
// with a Material beside it in the same frame shows the scratch does not leak
// one draw's material into the next.
func TestAnAbsentMaterialIsNoMaterial(t *testing.T) {
	h := newHarness(t)
	ref := h.bake(t)
	material := &Material{Tags: ecs.NewList(MaterialTag{Shader: gfx.ShaderWithText("flat")})}
	h.spawn(t, spawnRequest{Mesh: &Mesh{Ref: ref, Layers: scene.Layer(1)}, Material: material})
	h.spawn(t, spawnRequest{Mesh: &Mesh{Ref: ref}})
	h.spawn(t, spawnRequest{Model: &Model{Ref: scene.ModelRef{Path: "models/shaded.glb"}}, Material: material})
	h.spawn(t, spawnRequest{Model: &Model{Ref: scene.ModelRef{Path: crateModel}}})

	h.frame(t)

	meshes, models := h.ops(t, scene.OpMesh), h.ops(t, scene.OpModel)
	if len(meshes) != 2 || len(models) != 2 {
		t.Fatalf("the frame recorded %d meshes and %d models, want 2 and 2", len(meshes), len(models))
	}
	for _, op := range meshes {
		if absent := op.Layers == 0; absent != (op.Draw.Material == nil) {
			t.Errorf("the mesh on layers %v draws with material %v", op.Layers, op.Draw.Material)
		}
	}
	for _, op := range models {
		if absent := op.Path == crateModel; absent != (op.Model.Material == nil) {
			t.Errorf("the model %q draws with material %v", op.Path, op.Model.Material)
		}
	}
}

// TestALightIsPlacedAndAimedByItsTransform is the one place the binding
// computes anything: a spot's direction is its Transform's rotation applied to
// -Z, which is the way scene.LookAt faces, so a light placed with LookAt shines
// at what it looks at. An unrotated spot shines down -Z, and a point light
// takes its position alone.
func TestALightIsPlacedAndAimedByItsTransform(t *testing.T) {
	h := newHarness(t)
	eye, target := m.Vec3{Y: 5}, m.Vec3{X: 3, Y: 5, Z: 4}
	h.spawn(t, spawnRequest{
		Place: Transform(scene.LookAt(eye, target, m.Vec3{Y: 1})),
		Light: &Light{
			Kind: scene.LightSpot, Color: m.Color{R: 1, G: 0.5, A: 1}, Intensity: 2, Range: 10,
			InnerCone: 0.1, OuterCone: 0.4, Layers: scene.Layer(2),
		},
	})
	h.spawn(t, spawnRequest{Place: Transform{Position: m.Vec3{Z: 7}}, Light: &Light{Kind: scene.LightSpot}})
	h.spawn(t, spawnRequest{Place: Transform{Position: m.Vec3{X: -2}}, Light: &Light{Range: 3}})

	h.frame(t)

	spots, points := h.ops(t, scene.OpSpotLight), h.ops(t, scene.OpPointLight)
	if len(spots) != 2 || len(points) != 1 {
		t.Fatalf("the frame recorded %d spot and %d point lights, want 2 and 1", len(spots), len(points))
	}
	for _, op := range spots {
		switch op.Light.Position {
		case eye:
			if !near(op.Light.Direction, m.Vec3{X: 0.6, Z: 0.8}) {
				t.Errorf("the aimed spot shines along %v, want (0.6, 0, 0.8)", op.Light.Direction)
			}
			want := scene.LightDescr{
				Position: eye, Direction: op.Light.Direction, Color: m.Color{R: 1, G: 0.5, A: 1},
				Intensity: 2, Range: 10, InnerCone: 0.1, OuterCone: 0.4, Kind: scene.LightSpot,
			}
			if op.Light != want || op.Layers != scene.Layer(2) {
				t.Errorf("the aimed spot reached scene as %+v on %v, want %+v on %v",
					op.Light, op.Layers, want, scene.Layer(2))
			}
		case m.Vec3{Z: 7}:
			if !near(op.Light.Direction, m.Vec3{Z: -1}) {
				t.Errorf("the unrotated spot shines along %v, want -Z", op.Light.Direction)
			}
		default:
			t.Errorf("a spot light stands at %v", op.Light.Position)
		}
	}
	if points[0].Light.Position != (m.Vec3{X: -2}) || points[0].Light.Range != 3 {
		t.Errorf("the point light reached scene as %+v", points[0].Light)
	}
}

func near(a, b m.Vec3) bool {
	const epsilon = 1e-5
	d := a.Sub(b)
	return d.X*d.X+d.Y*d.Y+d.Z*d.Z < epsilon*epsilon
}

// TestACameraRecordsItsPassesWithTheirClears is the List of scene.Pass: each
// pass's clears are m.Maybe values, so "clear to this colour" and "preserve"
// are both a value a Component can hold. The camera is placed by its
// Transform, and a Camera whose Passes are empty leaves scene its default pass.
func TestACameraRecordsItsPassesWithTheirClears(t *testing.T) {
	h := newHarness(t)
	place := Transform(scene.LookAt(m.Vec3{Y: 3, Z: 10}, m.Vec3{}, m.Vec3{Y: 1}))
	passes := []scene.Pass{
		{ClearColor: m.Some(m.Color{B: 0.25, A: 1}), ClearDepth: m.Some[float32](1)},
		{Tag: "overlay", Order: 1},
	}
	h.spawn(t, spawnRequest{Place: place, Camera: &Camera{
		ID: -2, Projection: scene.Perspective, FovY: 1, Near: 0.1, Far: 100,
		CullMask: scene.Layer(4), SunDirection: m.Vec3{Y: -1}, SunColor: m.White, SunIntensity: 3,
		AmbientSky: m.Color{B: 1, A: 1}, AmbientGround: m.Color{G: 1, A: 1}, AmbientIntensity: 0.5,
		Passes: ecs.ListOf(passes),
	}})
	h.spawn(t, spawnRequest{Camera: &Camera{
		ID: 5, Projection: scene.Oblique, Height: 20, Shear: 0.5, Near: -50, Far: 50,
	}})

	h.frame(t)

	cameras := map[scene.CameraID]scene.CameraDescr{}
	for _, op := range h.ops(t, scene.OpCamera) {
		op.Descr.Passes = append([]scene.Pass(nil), op.Descr.Passes...)
		cameras[op.Camera] = op.Descr
	}
	want := scene.CameraDescr{
		Transform: scene.Transform(place), Projection: scene.Perspective, FovY: 1, Near: 0.1, Far: 100,
		CullMask: scene.Layer(4), SunDirection: m.Vec3{Y: -1}, SunColor: m.White, SunIntensity: 3,
		AmbientSky: m.Color{B: 1, A: 1}, AmbientGround: m.Color{G: 1, A: 1}, AmbientIntensity: 0.5,
		Passes: passes,
	}
	if got, ok := cameras[-2]; !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("the camera reached scene as\n%+v (recorded: %v)\nwant\n%+v", got, ok, want)
	}
	if clear, ok := cameras[-2].Passes[0].ClearColor.Get(); !ok || clear != (m.Color{B: 0.25, A: 1}) {
		t.Errorf("the first pass clears colour to %v (%v), want the Component's", clear, ok)
	}
	if _, ok := cameras[-2].Passes[1].ClearColor.Get(); ok {
		t.Error("the second pass clears colour, and its Component said preserve")
	}
	other, ok := cameras[5]
	if !ok || other.Projection != scene.Oblique || other.Height != 20 || other.Shear != 0.5 ||
		other.Near != -50 || len(other.Passes) != 0 {
		t.Errorf("the pass-less camera reached scene as %+v (recorded: %v)", other, ok)
	}
}

// TestAnEntityWithNoTransformIsNotRecorded holds for every kind: a Transform
// is required, so a Model, Mesh, Light or Camera on an Entity with nowhere to
// stand is not a draw at the origin but no draw at all.
func TestAnEntityWithNoTransformIsNotRecorded(t *testing.T) {
	h := newHarness(t)
	ref := h.bake(t)
	h.spawn(t, spawnRequest{Unplaced: true,
		Model: &Model{Ref: scene.ModelRef{Path: "models/nowhere.glb"}},
		Mesh:  &Mesh{Ref: ref}, Light: &Light{}, Camera: &Camera{ID: 1, FovY: 1, Near: 1, Far: 2},
	})
	h.spawn(t, spawnRequest{Model: &Model{Ref: scene.ModelRef{Path: crateModel}}})

	h.frame(t)

	if models := h.ops(t, scene.OpModel); len(models) != 1 || models[0].Path != crateModel {
		t.Errorf("the frame recorded model draws %v, want only the placed one", models)
	}
	if rest := h.ops(t, scene.OpMesh, scene.OpPointLight, scene.OpSpotLight, scene.OpCamera); len(rest) != 0 {
		t.Errorf("the frame recorded %d unplaced meshes, lights or cameras: %v", len(rest), rest)
	}
}

// TestADrawIsInTheFlushOfTheTickThatRecordedItWithNoOrderingDeclared is the
// ordering claim. scene subscribes its flush Last, so a recording System that
// declares nothing is in the ordinary phase and already runs before it. Every
// test above is the behavioural half — one frame, and the draw is in what that
// frame's flush published — and this one asserts the edge the engine derived
// without either side declaring it.
func TestADrawIsInTheFlushOfTheTickThatRecordedItWithNoOrderingDeclared(t *testing.T) {
	h := newHarness(t)
	h.spawn(t, spawnRequest{Model: &Model{Ref: scene.ModelRef{Path: crateModel}}})
	h.frame(t)
	if models := h.ops(t, scene.OpModel); len(models) != 1 {
		t.Fatalf("the first tick's flush published %d model draws, want the one recorded in it", len(models))
	}

	description := h.engine.Describe()
	var recorder, flush *kernel.SubscriptionDescription
	for i := range description.Subscriptions {
		switch description.Subscriptions[i].Type {
		case reflect.TypeFor[UpdateEventHandler]():
			recorder = &description.Subscriptions[i]
		case reflect.TypeFor[scene.UpdateEventHandler]():
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
	if !containsType(flush.DependsOn, reflect.TypeFor[UpdateEventHandler]()) {
		t.Errorf("scene's flush does not wait for the recording System; it depends on %v", flush.DependsOn)
	}
	if containsType(recorder.DependsOn, reflect.TypeFor[scene.UpdateEventHandler]()) {
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
		if sub.Type != reflect.TypeFor[UpdateEventHandler]() {
			continue
		}
		for _, want := range []reflect.Type{
			reflect.TypeFor[*ecs.Entities](),
			reflect.TypeFor[*ecs.Store[Transform]](),
			reflect.TypeFor[*ecs.Store[Model]](),
			reflect.TypeFor[*ecs.Store[Mesh]](),
			reflect.TypeFor[*ecs.Store[Light]](),
			reflect.TypeFor[*ecs.Store[Camera]](),
			reflect.TypeFor[*ecs.Store[Animation]](),
			reflect.TypeFor[*ecs.Store[Params]](),
			reflect.TypeFor[*ecs.Store[Material]](),
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
		reflect.TypeFor[Transform](): reflect.TypeFor[*ecs.Store[Transform]](),
		reflect.TypeFor[Model]():     reflect.TypeFor[*ecs.Store[Model]](),
		reflect.TypeFor[Mesh]():      reflect.TypeFor[*ecs.Store[Mesh]](),
		reflect.TypeFor[Animation](): reflect.TypeFor[*ecs.Store[Animation]](),
		reflect.TypeFor[Params]():    reflect.TypeFor[*ecs.Store[Params]](),
		reflect.TypeFor[Material]():  reflect.TypeFor[*ecs.Store[Material]](),
		reflect.TypeFor[Light]():     reflect.TypeFor[*ecs.Store[Light]](),
		reflect.TypeFor[Camera]():    reflect.TypeFor[*ecs.Store[Camera]](),
	} {
		if err := ecs.Storable(component); err != nil {
			t.Errorf("%v is not storable: %v", component, err)
		}
		if owner, ok := owned[store]; !ok || owner != Name {
			t.Errorf("%v is registered by %q (registered: %v), want %q", component, owner, ok, Name)
		}
	}
	for _, component := range []reflect.Type{reflect.TypeFor[Mesh](), reflect.TypeFor[Light]()} {
		if err := ecs.PointerFree(component); err != nil {
			t.Errorf("%v is not pointer-free: %v", component, err)
		}
	}
}
