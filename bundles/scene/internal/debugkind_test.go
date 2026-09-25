package internal

import (
	"slices"
	"strings"
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"

	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// The debug shapes' two Systems, judged the way every drawable here is: by
// what reaches the backend once a shape Component is on a real Entity in a
// real world. debuggeometry_test.go judges the geometry they bake.

// debugSpawnCmd spawns one placed Entity carrying the debug shapes that are
// set.
type debugSpawnCmd kernel.Command[debugSpawn, spawnResponse]

type debugSpawn struct {
	Place   m.Transform
	Box     *DebugBox
	Sphere  *DebugSphere
	Plane   *DebugPlane
	Line    *DebugLine
	WireBox *DebugWireBox
}

func debugSpawnCmdImpl(registrar *kernel.Registrar) func() (kernel.Lock, kernel.Execute[debugSpawn, spawnResponse]) {
	return ecs.ToExecute[debugSpawn, spawnResponse](registrar, func(
		request debugSpawn,
		spawn *ecs.Spawn[placed],
		boxes *ecs.Set[DebugBox],
		spheres *ecs.Set[DebugSphere],
		planes *ecs.Set[DebugPlane],
		lines *ecs.Set[DebugLine],
		wireBoxes *ecs.Set[DebugWireBox],
		answer *ecs.Resp[spawnResponse],
	) {
		e := spawn.New(placed{Place: request.Place})
		if request.Box != nil {
			boxes.UpdateFor(e, *request.Box)
		}
		if request.Sphere != nil {
			spheres.UpdateFor(e, *request.Sphere)
		}
		if request.Plane != nil {
			planes.UpdateFor(e, *request.Plane)
		}
		if request.Line != nil {
			lines.UpdateFor(e, *request.Line)
		}
		if request.WireBox != nil {
			wireBoxes.UpdateFor(e, *request.WireBox)
		}
		answer.Set(spawnResponse{First: e})
	})
}

// debugEditCmd writes one Entity's DebugBox, or takes it away. The five kinds
// share the two Systems' code, so the box stands for all of them.
type debugEditCmd kernel.Command[debugEdit, struct{}]

type debugEdit struct {
	Entity ecs.Entity
	Box    DebugBox
	Remove bool
	// DropMesh takes the Mesh the shape added away, as a caller that should
	// not have would.
	DropMesh bool
}

func debugEditCmdImpl(registrar *kernel.Registrar) func() (kernel.Lock, kernel.Execute[debugEdit, struct{}]) {
	return ecs.ToExecute[debugEdit, struct{}](registrar, func(
		request debugEdit,
		boxes *ecs.Set[DebugBox],
		removeBoxes *ecs.Remove[DebugBox],
		removeMeshes *ecs.Remove[Mesh],
	) {
		switch {
		case request.DropMesh:
			removeMeshes.From(request.Entity)
		case request.Remove:
			removeBoxes.From(request.Entity)
		default:
			boxes.UpdateFor(request.Entity, request.Box)
		}
	})
}

// debugStateCmd reads back what a shape added to its Entity, and whether the
// mesh its Mesh names is still in model's table.
type debugStateCmd kernel.Command[ecs.Entity, debugState]

type debugState struct {
	Mesh                            Mesh
	HasMesh, HasParams, HasMaterial bool
	Resident                        bool
}

func debugStateCmdImpl(registrar *kernel.Registrar) func() (kernel.Lock, kernel.Execute[ecs.Entity, debugState]) {
	return ecs.ToExecute[ecs.Entity, debugState](registrar, func(
		e ecs.Entity,
		meshes *ecs.Get[Mesh],
		params *ecs.Get[Params],
		materials *ecs.Get[Material],
		lookup *ecs.Read[*model.Lookup],
		answer *ecs.Resp[debugState],
	) {
		var state debugState
		state.Mesh, state.HasMesh = meshes.Of(e)
		_, state.HasParams = params.Of(e)
		_, state.HasMaterial = materials.Of(e)
		answer.Set(state)
	})
}

// debugResidentCmd reports whether a mesh ref is still in model's table.
type debugResidentCmd kernel.Command[model.MeshRef, bool]

func debugResidentCmdImpl(registrar *kernel.Registrar) func() (kernel.Lock, kernel.Execute[model.MeshRef, bool]) {
	return ecs.ToExecute[model.MeshRef, bool](registrar, func(
		ref model.MeshRef, lookup *ecs.Read[*model.Lookup], answer *ecs.Resp[bool],
	) {
		_, ok := lookup.Get().Mesh(ref)
		answer.Set(ok)
	})
}

// debugRadiusCmd reports the bounding radius model holds for a mesh, which
// its bake and every rebake recompute from the vertices.
type debugRadiusCmd kernel.Command[model.MeshRef, float32]

func debugRadiusCmdImpl(registrar *kernel.Registrar) func() (kernel.Lock, kernel.Execute[model.MeshRef, float32]) {
	return ecs.ToExecute[model.MeshRef, float32](registrar, func(
		ref model.MeshRef, lookup *ecs.Read[*model.Lookup], answer *ecs.Resp[float32],
	) {
		record, _ := lookup.Get().Mesh(ref)
		answer.Set(record.Bounds.Radius)
	})
}

func registerDebugTestCommands(registrar *kernel.Registrar) {
	registrar.HandleCommand[debugSpawnCmd](debugSpawnCmdImpl(registrar))
	registrar.HandleCommand[debugEditCmd](debugEditCmdImpl(registrar))
	registrar.HandleCommand[debugStateCmd](debugStateCmdImpl(registrar))
	registrar.HandleCommand[debugResidentCmd](debugResidentCmdImpl(registrar))
	registrar.HandleCommand[debugRadiusCmd](debugRadiusCmdImpl(registrar))
}

func (h *harness) meshRadius(t testing.TB, ref model.MeshRef) float32 {
	t.Helper()
	return h.kernel.ExecuteCommand[debugRadiusCmd](ref)
}

func (h *harness) spawnDebug(t testing.TB, request debugSpawn) ecs.Entity {
	t.Helper()
	return h.kernel.ExecuteCommand[debugSpawnCmd](request).First
}

func (h *harness) editDebug(t testing.TB, request debugEdit) {
	t.Helper()
	h.kernel.ExecuteCommand[debugEditCmd](request)
}

func (h *harness) debugState(t testing.TB, e ecs.Entity) debugState {
	t.Helper()
	state := h.kernel.ExecuteCommand[debugStateCmd](e)
	if state.HasMesh {
		state.Resident = h.kernel.ExecuteCommand[debugResidentCmd](state.Mesh.Ref)
	}
	return state
}

func (h *harness) resident(t testing.TB, ref model.MeshRef) bool {
	t.Helper()
	return h.kernel.ExecuteCommand[debugResidentCmd](ref)
}

// isDebug admits the draws of the debug shader.
func isDebug(d drawnInstance) bool {
	return strings.Contains(d.shader, "return scenePbrMaterial.baseColorFactor;")
}

// Each of the five shapes, spawned with a Transform, bakes and records one
// draw through the debug shader at its Entity, in its colour, with the
// geometry its builder makes.
func TestEachDebugShapeDrawsOneInstanceInItsColour(t *testing.T) {
	type shape struct {
		name    string
		request debugSpawn
		indices int
	}
	colour := m.Color{R: 0.25, G: 0.5, B: 0.75, A: 1}
	shapes := []shape{
		{"box", debugSpawn{Box: &DebugBox{Size: m.Vec3{X: 1, Y: 1, Z: 1}, Color: colour}}, 36},
		{"sphere", debugSpawn{Sphere: &DebugSphere{Radius: 1, Color: colour}}, 352 * 3},
		{"plane", debugSpawn{Plane: &DebugPlane{Normal: m.Vec3{Z: 1}, Size: m.Vec2{X: 1, Y: 1}, Color: colour}}, 6},
		{"line", debugSpawn{Line: &DebugLine{To: m.Vec3{X: 1}, Width: 0.1, Color: colour}}, 36},
		{"wire box", debugSpawn{WireBox: &DebugWireBox{Size: m.Vec3{X: 1, Y: 1, Z: 1}, Width: 0.1, Color: colour}}, 12 * 36},
	}
	for _, s := range shapes {
		t.Run(s.name, func(t *testing.T) {
			h := newDrawingHarness(t, 256)
			place := m.Vec3{X: 3, Y: -2}
			s.request.Place = m.Transform{Position: place}
			h.spawnDebug(t, s.request)
			h.frameUntil(t, "the "+s.name+" to draw", func() bool { return len(where(h.drawn(), isDebug)) > 0 })
			h.noErrors(t)
			drawn := where(h.drawn(), isDebug)
			if len(drawn) != 1 {
				t.Fatalf("the %s drew %d instances, want 1", s.name, len(drawn))
			}
			d := drawn[0]
			if !nearVec3(d.position(), place) {
				t.Errorf("the %s stands at %v, want %v", s.name, d.position(), place)
			}
			if d.count != s.indices || !d.indexed {
				t.Errorf("the %s drew %d indices (indexed %v), want %d", s.name, d.count, d.indexed, s.indices)
			}
			if got := d.param("baseColorFactor"); got != (m.Vec4{X: 0.25, Y: 0.5, Z: 0.75, W: 1}) {
				t.Errorf("the %s drew in %v, want its Color", s.name, got)
			}
			if d.state.Blend != gfx.BlendOpaque {
				t.Errorf("an opaque %s drew blended", s.name)
			}
		})
	}
}

// The debug shader declares the pass's bindings and the material's uniform
// block, and nothing a light or a texture would need: no light reaches it.
func TestTheDebugShaderReadsNoLightAndNoTexture(t *testing.T) {
	h := newDrawingHarness(t, 256)
	h.spawnDebug(t, debugSpawn{Box: &DebugBox{Size: m.Vec3{X: 1, Y: 1, Z: 1}, Color: m.White}})
	h.frameUntil(t, "the box to draw", func() bool { return len(where(h.drawn(), isDebug)) > 0 })
	h.noErrors(t)
	source := where(h.drawn(), isDebug)[0].shader
	bindings, _ := preludeReflection(t, source)
	for _, binding := range bindings {
		if strings.Contains(binding, "Texture") || strings.Contains(binding, "Sampler") {
			t.Errorf("the debug shader declares %s", binding)
		}
	}
	if !slices.ContainsFunc(bindings, func(b string) bool { return strings.HasPrefix(b, "scenePbrMaterial other 1/0") }) {
		t.Errorf("the debug shader declares %v, want the material block at 1/0", bindings)
	}
	fragment := source[strings.Index(source, "@fragment"):]
	for _, lit := range []string{"sceneShadeSurface", "sceneSun", "sceneAmbient", "sceneLightSample", "scenePbrFragment"} {
		if strings.Contains(fragment, lit) {
			t.Errorf("the debug fragment stage calls %s", lit)
		}
	}
}

// A geometry edit keeps the ref and rebakes in place; a colour edit repaints
// without rebaking; an alpha below 1 blends, and back at 1 it is opaque again.
func TestEditingADebugShapeRebakesInPlaceAndRepaints(t *testing.T) {
	h := newDrawingHarness(t, 256)
	box := DebugBox{Size: m.Vec3{X: 1, Y: 1, Z: 1}, Color: m.Color{R: 1, A: 1}}
	e := h.spawnDebug(t, debugSpawn{Box: &box})
	h.frameUntil(t, "the box to draw", func() bool { return len(where(h.drawn(), isDebug)) > 0 })
	baked := h.debugState(t, e).Mesh.Ref

	box.Size = m.Vec3{X: 4, Y: 4, Z: 4}
	h.editDebug(t, debugEdit{Entity: e, Box: box})
	h.frame(t)
	h.frame(t)
	if ref := h.debugState(t, e).Mesh.Ref; ref != baked {
		t.Errorf("a geometry edit rebaked to a new ref %v, want %v kept", ref, baked)
	}
	// The unit box's bounding sphere has radius sqrt(3)/2; the edit's is four
	// times that.
	if radius := h.meshRadius(t, baked); radius < 3.4 || radius > 3.5 {
		t.Errorf("after the edit the mesh's bounds have radius %v, want 2·sqrt(3)", radius)
	}

	box.Color = m.Color{G: 1, A: 0.5}
	h.editDebug(t, debugEdit{Entity: e, Box: box})
	h.frameUntil(t, "the repaint", func() bool {
		drawn := where(h.drawn(), isDebug)
		return len(drawn) == 1 && drawn[0].param("baseColorFactor") == m.Vec4{Y: 1, W: 0.5}
	})
	if d := where(h.drawn(), isDebug)[0]; d.state.Blend == gfx.BlendOpaque {
		t.Error("a translucent shape drew opaque")
	}
	if ref := h.debugState(t, e).Mesh.Ref; ref != baked {
		t.Errorf("a colour edit rebaked to %v", ref)
	}

	box.Color.A = 1
	h.editDebug(t, debugEdit{Entity: e, Box: box})
	h.frameUntil(t, "the shape to turn opaque", func() bool {
		drawn := where(h.drawn(), isDebug)
		return len(drawn) == 1 && drawn[0].state.Blend == gfx.BlendOpaque
	})
	h.noErrors(t)
}

// Removing a shape releases its mesh and takes away the three Components it
// added; despawning its Entity releases the mesh too.
func TestRemovingOrDespawningADebugShapeReleasesItsMesh(t *testing.T) {
	h := newDrawingHarness(t, 256)
	box := DebugBox{Size: m.Vec3{X: 1, Y: 1, Z: 1}, Color: m.White}
	removed := h.spawnDebug(t, debugSpawn{Box: &box, Place: m.Transform{Position: m.Vec3{X: -2}}})
	despawned := h.spawnDebug(t, debugSpawn{Box: &box, Place: m.Transform{Position: m.Vec3{X: 2}}})
	h.frameUntil(t, "both boxes to draw", func() bool { return len(where(h.drawn(), isDebug)) == 2 })
	removedRef, despawnedRef := h.debugState(t, removed).Mesh.Ref, h.debugState(t, despawned).Mesh.Ref

	h.editDebug(t, debugEdit{Entity: removed, Remove: true})
	h.despawn(t, despawned)
	h.frameUntil(t, "both boxes to go", func() bool { return len(where(h.drawn(), isDebug)) == 0 })

	state := h.debugState(t, removed)
	if state.HasMesh || state.HasParams || state.HasMaterial {
		t.Errorf("a removed shape left Mesh %v, Params %v, Material %v", state.HasMesh, state.HasParams, state.HasMaterial)
	}
	if h.resident(t, removedRef) {
		t.Error("a removed shape's mesh is still in model's table")
	}
	if h.resident(t, despawnedRef) {
		t.Error("a despawned shape's mesh is still in model's table")
	}
	h.noErrors(t)
}

// A shape whose Mesh a caller took away is baked again, and its old mesh
// released rather than leaked.
func TestADebugShapeWhoseMeshWasTakenIsBakedAgain(t *testing.T) {
	h := newDrawingHarness(t, 256)
	e := h.spawnDebug(t, debugSpawn{Box: &DebugBox{Size: m.Vec3{X: 1, Y: 1, Z: 1}, Color: m.White}})
	h.frameUntil(t, "the box to draw", func() bool { return len(where(h.drawn(), isDebug)) > 0 })
	first := h.debugState(t, e).Mesh.Ref

	h.editDebug(t, debugEdit{Entity: e, DropMesh: true})
	h.frame(t)
	state := h.debugState(t, e)
	if !state.HasMesh || !state.Resident || state.Mesh.Ref == first {
		t.Errorf("after its Mesh was taken the shape holds %+v, want a new resident mesh", state)
	}
	if h.resident(t, first) {
		t.Error("the first mesh was not released")
	}
	h.noErrors(t)
}

// A shape with nothing to draw gets no Mesh and reports nothing, and becomes
// drawable the tick its geometry does; a drawable one that becomes
// degenerate gives its mesh back.
func TestADegenerateDebugShapeDrawsNothingAndReportsNothing(t *testing.T) {
	h := newDrawingHarness(t, 256)
	box := DebugBox{Color: m.White}
	e := h.spawnDebug(t, debugSpawn{Box: &box})
	for range 3 {
		h.frame(t)
	}
	if state := h.debugState(t, e); state.HasMesh {
		t.Error("a box of no size was given a Mesh")
	}
	if len(where(h.drawn(), isDebug)) != 0 {
		t.Error("a box of no size drew")
	}

	box.Size = m.Vec3{X: 1, Y: 1, Z: 1}
	h.editDebug(t, debugEdit{Entity: e, Box: box})
	h.frameUntil(t, "the box to draw once it has a size", func() bool { return len(where(h.drawn(), isDebug)) == 1 })
	ref := h.debugState(t, e).Mesh.Ref

	box.Size = m.Vec3{}
	h.editDebug(t, debugEdit{Entity: e, Box: box})
	h.frameUntil(t, "the box to stop drawing", func() bool { return len(where(h.drawn(), isDebug)) == 0 })
	if state := h.debugState(t, e); state.HasMesh {
		t.Error("a box that lost its size kept its Mesh")
	}
	if h.resident(t, ref) {
		t.Error("a box that lost its size kept its mesh")
	}
	h.noErrors(t)
}

// A shape's Layers reach its Mesh, so a camera whose CullMask leaves them out
// does not draw it.
func TestADebugShapesLayersReachTheCull(t *testing.T) {
	h := newCameralessHarness(t, 256)
	h.spawn(t, spawnRequest{Place: defaultEye, Camera: &Camera{
		FovY: 1.0472, Near: 0.1, Far: 200, CullMask: Layer(1),
	}})
	seen := DebugBox{Size: m.Vec3{X: 1, Y: 1, Z: 1}, Color: m.White, Layers: Layer(1)}
	hidden := DebugBox{Size: m.Vec3{X: 1, Y: 1, Z: 1}, Color: m.White, Layers: Layer(2)}
	h.spawnDebug(t, debugSpawn{Box: &seen, Place: m.Transform{Position: m.Vec3{X: -2}}})
	hiddenEntity := h.spawnDebug(t, debugSpawn{Box: &hidden, Place: m.Transform{Position: m.Vec3{X: 2}}})
	h.frameUntil(t, "the box on the camera's layer to draw", func() bool { return len(where(h.drawn(), isDebug)) > 0 })
	for range 2 {
		h.frame(t)
	}
	drawn := where(h.drawn(), isDebug)
	if len(drawn) != 1 || !nearVec3(drawn[0].position(), m.Vec3{X: -2}) {
		t.Errorf("the camera drew %d debug instances, want the one on its layer", len(drawn))
	}
	if state := h.debugState(t, hiddenEntity); state.Mesh.Layers != Layer(2) {
		t.Errorf("the hidden box's Mesh has layers %v, want its own", state.Mesh.Layers)
	}
	h.noErrors(t)
}
