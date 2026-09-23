package internal

import (
	"bytes"
	"testing"
	"testing/fstest"
	"time"

	"github.com/dvoyni/cog/bundles/ecsscene"
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
)

// The files the drawing tests load. Each is built rather than read, because
// this package has no testdata and the point is that the path a Model
// Component holds is a file scene really loads.
const (
	propsModel    = "models/props.glb"
	animatedModel = "models/animated.glb"
	paintedModel  = "models/painted.glb"
)

// triangle is the crate's one triangle, and quad the barrel's two: the vertex
// count a draw reaches the backend with is what tells the two meshes apart.
var (
	triangle = [][3]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}}
	quad     = [][3]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}, {1, 0, 0}, {1, 1, 0}, {0, 1, 0}}
)

// meshOf adds one mesh of unindexed positions to doc and returns its index.
func meshOf(doc *gltf.Document, name string, positions [][3]float32) int {
	doc.Meshes = append(doc.Meshes, &gltf.Mesh{
		Name:       name,
		Primitives: []*gltf.Primitive{{Attributes: gltf.PrimitiveAttributes{gltf.POSITION: modeler.WritePosition(doc, positions)}}},
	})
	return len(doc.Meshes) - 1
}

// encodeGLB writes doc as a binary glTF file.
func encodeGLB(t testing.TB, doc *gltf.Document) []byte {
	t.Helper()
	var buffer bytes.Buffer
	if err := gltf.NewEncoder(&buffer).Encode(doc); err != nil {
		t.Fatalf("encoding the model: %v", err)
	}
	return buffer.Bytes()
}

// crateGLB is the smallest drawable file: one triangle, one node, one scene.
func crateGLB(t testing.TB) []byte {
	t.Helper()
	doc := &gltf.Document{Asset: gltf.Asset{Version: "2.0"}}
	meshOf(doc, "crate", triangle)
	doc.Nodes = []*gltf.Node{{Name: "crate", Mesh: gltf.Index(0)}}
	doc.Scenes = []*gltf.Scene{{Name: "scene", Nodes: []int{0}}}
	doc.Scene = gltf.Index(0)
	return encodeGLB(t, doc)
}

// propsGLB is a file a Scene and a Node selector each change the answer for:
// its default scene holds the barrel alone, and the scene named "props" holds
// the crate and the barrel. Only {props, crate} draws the triangle and nothing
// else.
func propsGLB(t testing.TB) []byte {
	t.Helper()
	doc := &gltf.Document{Asset: gltf.Asset{Version: "2.0"}}
	meshOf(doc, "crate", triangle)
	meshOf(doc, "barrel", quad)
	doc.Nodes = []*gltf.Node{
		{Name: "crate", Mesh: gltf.Index(0)},
		{Name: "barrel", Mesh: gltf.Index(1)},
	}
	doc.Scenes = []*gltf.Scene{
		{Name: "scene", Nodes: []int{1}},
		{Name: "props", Nodes: []int{0, 1}},
	}
	doc.Scene = gltf.Index(0)
	return encodeGLB(t, doc)
}

// animatedGLB is the crate on a node two clips spin, which is a single-joint
// skin: a draw of it that plays anything packs a sceneAnim block, one play
// record per clip it resolved.
func animatedGLB(t testing.TB) []byte {
	t.Helper()
	doc := &gltf.Document{Asset: gltf.Asset{Version: "2.0"}}
	meshOf(doc, "crate", triangle)
	doc.Nodes = []*gltf.Node{{Name: "crate", Mesh: gltf.Index(0)}}
	doc.Scenes = []*gltf.Scene{{Name: "scene", Nodes: []int{0}}}
	doc.Scene = gltf.Index(0)
	for _, clip := range []struct {
		name string
		end  [4]float32
	}{{"Walk", [4]float32{0, 0, 1, 0}}, {"Idle", [4]float32{1, 0, 0, 0}}} {
		sampler := &gltf.AnimationSampler{
			Input:  modeler.WriteAccessor(doc, gltf.TargetNone, []float32{0, 1}),
			Output: modeler.WriteAccessor(doc, gltf.TargetNone, [][4]float32{{0, 0, 0, 1}, clip.end}),
		}
		doc.Animations = append(doc.Animations, &gltf.Animation{
			Name:     clip.name,
			Samplers: []*gltf.AnimationSampler{sampler},
			Channels: []*gltf.AnimationChannel{{
				Sampler: 0,
				Target:  gltf.AnimationChannelTarget{Node: gltf.Index(0), Path: gltf.TRSRotation},
			}},
		})
	}
	return encodeGLB(t, doc)
}

// paintedGLB is one node holding two primitives under two materials: the
// triangle red and blended, the quad green and double-sided. What a draw of
// either keeps of its own material is what tells the two apart.
func paintedGLB(t testing.TB) []byte {
	t.Helper()
	doc := &gltf.Document{Asset: gltf.Asset{Version: "2.0"}}
	doc.Materials = []*gltf.Material{
		{Name: "red", AlphaMode: gltf.AlphaBlend,
			PBRMetallicRoughness: &gltf.PBRMetallicRoughness{BaseColorFactor: &[4]float64{1, 0, 0, 1}}},
		{Name: "green", DoubleSided: true,
			PBRMetallicRoughness: &gltf.PBRMetallicRoughness{BaseColorFactor: &[4]float64{0, 1, 0, 1}}},
	}
	doc.Meshes = []*gltf.Mesh{{Name: "painted", Primitives: []*gltf.Primitive{
		{Attributes: gltf.PrimitiveAttributes{gltf.POSITION: modeler.WritePosition(doc, triangle)}, Material: gltf.Index(0)},
		{Attributes: gltf.PrimitiveAttributes{gltf.POSITION: modeler.WritePosition(doc, quad)}, Material: gltf.Index(1)},
	}}}
	doc.Nodes = []*gltf.Node{{Name: "painted", Mesh: gltf.Index(0)}}
	doc.Scenes = []*gltf.Scene{{Name: "scene", Nodes: []int{0}}}
	doc.Scene = gltf.Index(0)
	return encodeGLB(t, doc)
}

// crateModelComponent is a Model naming the crate file's whole default scene.
func crateModelComponent() *ecsscene.Model {
	return &ecsscene.Model{Ref: model.ModelRef{Path: crateModel}}
}

// defaultEye is where the harness camera stands, looking at the origin.
var defaultEye = m.LookAt(m.Vec3{Z: 30}, m.Vec3{}, m.Vec3{Y: 1})

// newDrawingHarness is the harness with a recording backend, a viewport, every
// model file on disk and one camera looking at the origin: the whole engine,
// able to decide and render a frame.
func newDrawingHarness(t testing.TB, ids uint32) *harness {
	t.Helper()
	h := newCameralessHarness(t, ids)
	h.spawn(t, spawnRequest{Place: defaultEye, Camera: &ecsscene.Camera{FovY: 1.0472, Near: 0.1, Far: 200}})
	return h
}

// newCameralessHarness is newDrawingHarness for a test that places its own
// cameras.
func newCameralessHarness(t testing.TB, ids uint32) *harness {
	t.Helper()
	files := fstest.MapFS{
		crateModel:    &fstest.MapFile{Data: crateGLB(t)},
		propsModel:    &fstest.MapFile{Data: propsGLB(t)},
		animatedModel: &fstest.MapFile{Data: animatedGLB(t)},
		paintedModel:  &fstest.MapFile{Data: paintedGLB(t)},
	}
	h := newHarnessWith(t, files, ids, &testBackend{})
	h.kernel.ExecuteCommand[gfx.SetViewportCmd](gfx.SetViewportRequest{
		Width: 800, Height: 600, FramebufferWidth: 1600, FramebufferHeight: 1200,
	})
	return h
}

// frameUntil runs frames until ready. What takes frames is the very first
// ones, before the viewport and the backend are up, and a model's residency.
func (h *harness) frameUntil(t testing.TB, what string, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		h.frame(t)
		if ready() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s; errors so far: %v", what, h.errs.snapshot())
		}
		time.Sleep(time.Millisecond)
	}
}

// drawn is every instance the last frame drew.
func (h *harness) drawn() []drawnInstance { return h.backend.instances() }

// where narrows instances to those one predicate admits.
func where(instances []drawnInstance, admit func(drawnInstance) bool) []drawnInstance {
	var out []drawnInstance
	for _, instance := range instances {
		if admit(instance) {
			out = append(out, instance)
		}
	}
	return out
}

// at admits the instances standing at one place, which is how a test finds
// the Entity it placed there.
func at(position m.Vec3) func(drawnInstance) bool {
	return func(d drawnInstance) bool { return nearVec3(d.position(), position) }
}

// ofTriangle admits the draws of a three-vertex mesh, and ofQuad the draws of
// the barrel's six.
func ofTriangle(d drawnInstance) bool { return d.count == 3 }
func ofQuad(d drawnInstance) bool     { return d.count == 6 }

// inPass admits the instances drawn into the pass one camera emitted for one
// tag, which the recording System labels the pass with, in scene's spelling.
func inPass(label string) func(drawnInstance) bool {
	return func(d drawnInstance) bool { return d.pass.Label == label }
}

// positions lists where instances stand, for a failure message.
func positions(instances []drawnInstance) []m.Vec3 {
	out := []m.Vec3{}
	for _, instance := range instances {
		out = append(out, instance.position())
	}
	return out
}

// noErrors fails the test on anything the engine reported: a draw gfx dropped
// is a report, and a test that asserts what was drawn must see every drop.
func (h *harness) noErrors(t testing.TB) {
	t.Helper()
	for _, err := range h.errs.snapshot() {
		t.Errorf("the frame reported %v", err)
	}
}

// TestDrawableEntitiesBecomeInstancesInAPass is the end-to-end: Components on a
// real world, through the binding's load System, which loads the file a Model
// names through model, and its recording System, which packs the Entities into
// a pass that reaches the backend.
//
// Nothing between the Component and the instance was written for this test.
func TestDrawableEntitiesBecomeInstancesInAPass(t *testing.T) {
	h := newDrawingHarness(t, 256)
	h.spawn(t, spawnRequest{Count: 3, Step: 2, Model: crateModelComponent()})

	h.frameUntil(t, "the crate to become resident", func() bool {
		return len(where(h.drawn(), ofTriangle)) == 3
	})

	crates := where(h.drawn(), ofTriangle)
	for _, x := range []float32{0, 2, 4} {
		if got := where(crates, at(m.Vec3{X: x})); len(got) != 1 {
			t.Errorf("the crate at x=%v drew %d instances, want 1; the crates stand at %v", x, len(got), positions(crates))
		}
	}
	h.noErrors(t)
}

// TestADespawnedDrawableStopsDrawing is the other end of the lifecycle, and it
// needs nothing from the ECS: there are no lifecycle hooks, the Components are
// the source of truth, and a frame records what is there when it runs.
func TestADespawnedDrawableStopsDrawing(t *testing.T) {
	h := newDrawingHarness(t, 256)
	first := h.spawn(t, spawnRequest{Count: 2, Step: 2, Model: crateModelComponent()})

	h.frameUntil(t, "the crate to become resident", func() bool {
		return len(where(h.drawn(), ofTriangle)) == 2
	})

	h.despawn(t, first)
	h.frame(t)

	crates := where(h.drawn(), ofTriangle)
	if len(crates) != 1 || !at(m.Vec3{X: 2})(crates[0]) {
		t.Fatalf("after the despawn the crate drew at %v, want only the survivor at x=2", positions(crates))
	}
}

// TestAPresentMaterialWithNoTagsDrawsNothing is presence meaning what scene's
// empty material means: a material serving no pass. Only the two Entities with
// no Material draw - the bundled PBR for the mesh and the file's own material
// for the model - and the two with an empty one draw nowhere.
func TestAPresentMaterialWithNoTagsDrawsNothing(t *testing.T) {
	h := newDrawingHarness(t, 256)
	ref := h.bake(t)
	h.spawn(t, spawnRequest{Place: m.At(-2, 0, 0), Mesh: &ecsscene.Mesh{Ref: ref, NeverCull: true}})
	h.spawn(t, spawnRequest{Place: m.At(-4, 0, 0), Mesh: &ecsscene.Mesh{Ref: ref, NeverCull: true}, Material: &ecsscene.Material{}})
	h.spawn(t, spawnRequest{Place: m.At(2, 0, 0), Model: crateModelComponent()})
	h.spawn(t, spawnRequest{Place: m.At(4, 0, 0), Model: crateModelComponent(), Material: &ecsscene.Material{}})

	h.frameUntil(t, "the crate to become resident", func() bool {
		return len(where(h.drawn(), at(m.Vec3{X: 2}))) > 0
	})

	drawn := h.drawn()
	for _, x := range []float32{-2, 2} {
		if got := where(drawn, at(m.Vec3{X: x})); len(got) != 1 {
			t.Errorf("the Entity with no Material at x=%v drew %d instances, want 1", x, len(got))
		}
	}
	for _, x := range []float32{-4, 4} {
		if got := where(drawn, at(m.Vec3{X: x})); len(got) != 0 {
			t.Errorf("the Entity with an empty Material at x=%v drew %d instances, want none", x, len(got))
		}
	}
	h.noErrors(t)
}
