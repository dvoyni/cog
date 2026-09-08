package scene

import (
	"errors"
	"math"
	"testing"
	"unsafe"

	"github.com/dvoyni/cog/m"
	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
)

// testSampleRate is the rate the bake tests convert at. It is the default, so
// the frame counts below are the ones a real load produces.
const testSampleRate = 60

// convertTest converts a document at the default sample rate.
func convertTest(t *testing.T, doc *gltf.Document) *loadedModel {
	t.Helper()
	model, err := convertDocument(doc, "m.glb", nil, testSampleRate)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	return model
}

// The shader reads these records by offset, so their sizes are contract, not
// implementation. A record that grows by a padding word the WGSL side does not
// have reads every field after it from the wrong place - silently, because
// nothing in the pipeline compares the two layouts.
func TestAnimationRecordSizesMatchTheShaderContract(t *testing.T) {
	for _, want := range []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"scenePose", unsafe.Sizeof(scenePose{}), 48},
		{"sceneSkinJoint", unsafe.Sizeof(sceneSkinJoint{}), 112},
		{"scenePlayRecord", unsafe.Sizeof(scenePlayRecord{}), 16},
		{"sceneAnimHeader", unsafe.Sizeof(sceneAnimHeader{}), 32},
	} {
		if want.got != want.want {
			t.Errorf("%s is %d bytes, want %d", want.name, want.got, want.want)
		}
	}
	// 112 is exactly seven vec4s with no tail padding, which is the whole
	// reason the record is explicit columns rather than mat4x3 and mat3x3.
	if unsafe.Sizeof(sceneSkinJoint{})%16 != 0 {
		t.Error("sceneSkinJoint must be a whole number of vec4s")
	}
}

// A file with no skins and no animation bakes nothing at all. That is what puts
// every one of its draws on the variant with no pose bindings rather than charging a static prop a
// per-vertex pose fetch for a guaranteed identity.
func TestBakeAnimationIsEmptyWithoutSkinsOrClips(t *testing.T) {
	doc := testDoc()
	mesh := triangleMesh(doc, nil)
	doc.Nodes = []*gltf.Node{{Name: "prop", Mesh: gltf.Index(mesh)}}
	sceneOf(doc, 0)
	model := convertTest(t, doc)
	if model.animation.jointCount != 0 {
		t.Errorf("jointCount = %d, want none", model.animation.jointCount)
	}
	if len(model.animation.poses) != 0 || len(model.animation.clips) != 0 {
		t.Errorf("poses = %d and clips = %d, want none of either",
			len(model.animation.poses), len(model.animation.clips))
	}
	if model.primitives[0].skinned {
		t.Error("a prop no clip touches draws unskinned")
	}
}

// skinnedDoc builds a two-joint skin: a root bone at the origin and a child
// bone one unit up, with a mesh bound entirely to the child.
func skinnedDoc() *gltf.Document {
	doc := testDoc()
	positions := [][3]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}}
	doc.Meshes = append(doc.Meshes, &gltf.Mesh{Name: "body", Primitives: []*gltf.Primitive{{
		Attributes: gltf.PrimitiveAttributes{
			gltf.POSITION:  modeler.WritePosition(doc, positions),
			gltf.JOINTS_0:  modeler.WriteJoints(doc, [][4]uint16{{1}, {1}, {1}}),
			gltf.WEIGHTS_0: modeler.WriteWeights(doc, [][4]float32{{1}, {1}, {1}}),
		},
	}}})
	doc.Skins = append(doc.Skins, &gltf.Skin{
		Name:   "rig",
		Joints: []int{1, 2},
		// The tip bone binds one unit up, so its inverse bind translates one
		// unit down - which is a value the assertions below can read directly
		// off the packed record.
		InverseBindMatrices: gltf.Index(modeler.WriteInverseBindMatrices(doc, [][4][4]float32{
			inverseBind(m.NewMat4()), inverseBind(m.Translation4(0, -1, 0)),
		})),
	})
	doc.Nodes = []*gltf.Node{
		{Name: "body", Mesh: gltf.Index(0), Skin: gltf.Index(0), Translation: [3]float64{7, 0, 0}},
		{Name: "root", Children: []int{2}},
		{Name: "tip", Translation: [3]float64{0, 1, 0}},
	}
	sceneOf(doc, 0, 1)
	return doc
}

// inverseBind takes a matrix in cog's own column-major layout and returns what
// the modeler needs in order to serialise it, which is its transpose: glTF's
// buffer is column-major, but the modeler indexes a [4][4]float32 as
// [row][column].
//
// Spelling the transpose out here, once, is the point. A fixture built the way
// the loader reads cancels the loader's mistakes - the round trip agrees with
// itself whichever way both halves are wrong - and a real rig then loads with
// its skeleton inside out while every test passes. Writing the fixture from a
// matrix the test can read at a glance is what makes the convention assertable
// rather than merely self-consistent.
func inverseBind(matrix m.Mat4) [4][4]float32 {
	var value [4][4]float32
	for column := range 4 {
		for row := range 4 {
			value[row][column] = matrix[column*4+row]
		}
	}
	return value
}

// The skin's own JOINTS_0 indexes its joints array; the model's numbering is
// what makes a pose row addressable with no per-skin offset. So the remap has
// to happen at load, in the vertex buffer.
func TestBakeAnimationRemapsJointsIntoTheModelSpace(t *testing.T) {
	doc := skinnedDoc()
	// A second skin over the same bones in the opposite order is what makes
	// the remap observable: without it the identity would pass.
	doc.Skins = append(doc.Skins, &gltf.Skin{Name: "reversed", Joints: []int{2, 1}})
	model := convertTest(t, doc)
	if model.animation.jointCount != 2 {
		t.Fatalf("jointCount = %d, want the skin's two bones", model.animation.jointCount)
	}
	// Skin 0's slot 1 is node 2, which is the second joint the space claimed.
	geometry := &model.geometries[model.primitives[0].geometry]
	for i, vertex := range geometry.vertices {
		if vertex.Joints[0] != 1 {
			t.Errorf("vertex %d binds joint %d, want the model index of the skin's slot 1", i, vertex.Joints[0])
		}
		if vertex.Weights.X != 1 {
			t.Errorf("vertex %d weighs %v, want a whole influence", i, vertex.Weights.X)
		}
	}
	if !model.primitives[0].skinned {
		t.Error("a primitive with weights under a skinned node is skinned")
	}
	// A skinned node's own transform is the glTF specification's to ignore -
	// the joints resolve against the scene root - so the placement is identity
	// rather than the node's seven units along X.
	if got := model.primitives[0].local; got != m.NewMat4() {
		t.Errorf("skinned placement = %v, want the identity", got)
	}
}

// Row 0 is the rest frame: the authored hierarchy resolved once. Without it a
// draw with no plays has nothing to place its geometry, because a skinned
// node's own transform is discarded and a degenerate node's lives in the pose
// buffer - the model would collapse to the origin.
func TestBakeAnimationWritesTheRestFrameAtRowZero(t *testing.T) {
	model := convertTest(t, skinnedDoc())
	poses := model.animation.poses
	if len(poses) < 2 {
		t.Fatalf("poses = %d rows of records, want at least the rest frame", len(poses))
	}
	if got := poses[0].Translation; got != (m.Vec4{}) {
		t.Errorf("root joint rests at %v, want the origin", got)
	}
	if got := poses[1].Translation; got != (m.Vec4{Y: 1}) {
		t.Errorf("tip joint rests at %v, want one unit up", got)
	}
	if got := poses[1].Scale; got != (m.Vec4{X: 1, Y: 1, Z: 1}) {
		t.Errorf("rest scale = %v, want unit", got)
	}
}

// The inverse bind and the normal matrix interleave into one record, indexed by
// the same joint, so both halves land adjacent for all four influences.
//
// The translation assertion is also the layout assertion. glTF stores a matrix
// column-major and the modeler hands it over as [row][column], so a loader that
// skips the transpose puts the translation in the w of the first three columns
// - where an affine matrix has zeroes - and every rotation comes out inverted.
func TestBakeAnimationInterleavesTheInverseBindAndItsNormalMatrix(t *testing.T) {
	model := convertTest(t, skinnedDoc())
	joints := model.animation.joints
	if len(joints) != 2 {
		t.Fatalf("joint records = %d, want one per bone", len(joints))
	}
	if got := joints[1].InverseBind3; got != (m.Vec4{Y: -1, W: 1}) {
		t.Errorf("tip inverse bind translation = %v, want minus one unit up", got)
	}
	// A rigid inverse bind is its own normal matrix, and its determinant is
	// positive, so the handedness sign rides through unchanged.
	if got := joints[1].Normal0; got != (m.Vec4{X: 1, W: 1}) {
		t.Errorf("tip normal matrix column 0 = %v, want the identity with a positive handedness", got)
	}
}

// A mirrored bind pose flips tangent handedness, and the shader must not derive
// that per vertex per influence when the load can precompute it.
func TestSkinJointRecordCarriesTheHandednessOfTheInverseBind(t *testing.T) {
	mirrored := skinJointRecord(m.Scaling4(-1, 1, 1))
	if mirrored.Normal0.W != -1 {
		t.Errorf("mirrored handedness = %v, want -1", mirrored.Normal0.W)
	}
	if skinJointRecord(m.NewMat4()).Normal0.W != 1 {
		t.Error("an identity bind is right-handed")
	}
}

// rotationClip adds an animation that spins one node about Z over a second,
// and returns the document.
func rotationClip(doc *gltf.Document, name string, node int, times []float32, values [][4]float32) {
	sampler := &gltf.AnimationSampler{
		Input:  modeler.WriteAccessor(doc, gltf.TargetNone, times),
		Output: modeler.WriteAccessor(doc, gltf.TargetNone, values),
	}
	doc.Animations = append(doc.Animations, &gltf.Animation{
		Name:     name,
		Samplers: []*gltf.AnimationSampler{sampler},
		Channels: []*gltf.AnimationChannel{{
			Sampler: 0,
			Target:  gltf.AnimationChannelTarget{Node: gltf.Index(node), Path: gltf.TRSRotation},
		}},
	})
}

// The grid covers [0, duration] whole, so a one-second clip at 60 Hz is 61
// frames: sixty intervals plus the endpoint. Rows lay out after the rest frame,
// which is what makes the play record's two rows enough on their own.
func TestBakeAnimationLaysClipsOutAfterTheRestFrame(t *testing.T) {
	doc := skinnedDoc()
	rotationClip(doc, "spin", 2, []float32{0, 1}, [][4]float32{{0, 0, 0, 1}, {0, 0, 1, 0}})
	rotationClip(doc, "half", 2, []float32{0, 0.5}, [][4]float32{{0, 0, 0, 1}, {0, 0, 1, 0}})
	model := convertTest(t, doc)
	clips := model.animation.clips
	if len(clips) != 2 {
		t.Fatalf("clips = %d, want two", len(clips))
	}
	if clips[0].name != "spin" || clips[0].frames != 61 {
		t.Errorf("clip 0 = %q with %d frames, want spin with 61", clips[0].name, clips[0].frames)
	}
	if clips[0].base != model.animation.jointCount {
		t.Errorf("clip 0 starts at row-record %d, want the record after the rest frame", clips[0].base)
	}
	if clips[1].frames != 31 {
		t.Errorf("clip 1 = %d frames, want 31 for half a second", clips[1].frames)
	}
	if want := (1 + 61) * model.animation.jointCount; clips[1].base != want {
		t.Errorf("clip 1 starts at %d, want %d", clips[1].base, want)
	}
	if want := (1 + 61 + 31) * model.animation.jointCount; len(model.animation.poses) != want {
		t.Errorf("poses = %d records, want %d", len(model.animation.poses), want)
	}
}

// A single-keyframe clip and a zero-duration clip each bake to one frame, and
// neither is an error: a pose that never changes is still a pose, and a play
// on it is a legal way to hold a character still.
func TestBakeAnimationGivesADegenerateClipOneFrame(t *testing.T) {
	for _, sample := range []struct {
		name   string
		times  []float32
		values [][4]float32
	}{
		{"single", []float32{0}, [][4]float32{{0, 0, 0, 1}}},
		{"zero-length", []float32{0, 0}, [][4]float32{{0, 0, 0, 1}, {0, 0, 1, 0}}},
	} {
		t.Run(sample.name, func(t *testing.T) {
			doc := skinnedDoc()
			rotationClip(doc, sample.name, 2, sample.times, sample.values)
			model := convertTest(t, doc)
			if len(model.animation.clips) != 1 {
				t.Fatalf("clips = %d, want one", len(model.animation.clips))
			}
			if got := model.animation.clips[0].frames; got != 1 {
				t.Errorf("frames = %d, want one", got)
			}
		})
	}
}

// The frame lerp in the shader has no runtime sign check, so the bake owes it
// quaternions that never cross the hemisphere within a clip. A half-turn per
// frame is what makes an unfixed chain visibly flip.
func TestBakeAnimationFixesQuaternionHemisphereWithinAClip(t *testing.T) {
	doc := skinnedDoc()
	// Four keys a half-turn apart about Z. Sampled naively, consecutive frames
	// land on opposite hemispheres and the lerp takes the long way round.
	half := float32(math.Sqrt2 / 2)
	rotationClip(doc, "flip", 2, []float32{0, 0.25, 0.5, 0.75}, [][4]float32{
		{0, 0, 0, 1}, {0, 0, half, half}, {0, 0, 1, 0}, {0, 0, half, -half},
	})
	model := convertTest(t, doc)
	clip := model.animation.clips[0]
	joints := model.animation.jointCount
	for frame := 1; frame < clip.frames; frame++ {
		previous := model.animation.poses[clip.base+(frame-1)*joints+1].Rotation
		current := model.animation.poses[clip.base+frame*joints+1].Rotation
		if dotQuat(previous, current) < 0 {
			t.Fatalf("frames %d and %d sit on opposite hemispheres", frame-1, frame)
		}
	}
}

// Rigid node animation - wheels, propellers, doors - is ordinary glTF, so a
// node a clip steers becomes a degenerate single-joint skin rather than a
// second animation mechanism with a per-frame CPU hierarchy walk.
func TestBakeAnimationGivesAnAnimatedMeshNodeADegenerateJoint(t *testing.T) {
	doc := testDoc()
	mesh := triangleMesh(doc, nil)
	doc.Nodes = []*gltf.Node{
		{Name: "wheel", Mesh: gltf.Index(mesh), Translation: [3]float64{3, 0, 0}},
	}
	sceneOf(doc, 0)
	rotationClip(doc, "spin", 0, []float32{0, 1}, [][4]float32{{0, 0, 0, 1}, {0, 0, 1, 0}})
	model := convertTest(t, doc)
	if model.animation.jointCount != 1 {
		t.Fatalf("jointCount = %d, want the one degenerate joint", model.animation.jointCount)
	}
	if !model.primitives[0].skinned {
		t.Error("an animated mesh node draws through its joint")
	}
	// Its transform lives in the pose buffer now, so the flattened placement
	// is the identity and the rest row carries the three units along X.
	if got := model.primitives[0].local; got != m.NewMat4() {
		t.Errorf("placement = %v, want the identity", got)
	}
	if got := model.animation.poses[0].Translation; got != (m.Vec4{X: 3}) {
		t.Errorf("rest pose = %v, want the node's authored place", got)
	}
	geometry := &model.geometries[model.primitives[0].geometry]
	if got := geometry.vertices[0]; got.Joints != [4]uint16{0} || got.Weights != (m.Vec4{X: 1}) {
		t.Errorf("degenerate binding = joints %v weights %v, want joint 0 at weight 1",
			got.Joints, got.Weights)
	}
	// A degenerate joint has no inverse bind to premultiply, which is one of
	// the two reasons the pose record holds globalJoint alone.
	if got := model.animation.joints[0]; got != skinJointRecord(m.NewMat4()) {
		t.Errorf("degenerate joint record = %v, want the identity", got)
	}
}

// The rule keys on TRS channels only. A node whose clip touches nothing but its
// morph weights creates no joint, which is what lets a morph-only model load
// with an empty pose buffer.
func TestBakeAnimationCreatesNoJointForAWeightsOnlyChannel(t *testing.T) {
	doc := testDoc()
	mesh := triangleMesh(doc, nil)
	doc.Nodes = []*gltf.Node{{Name: "face", Mesh: gltf.Index(mesh)}}
	sceneOf(doc, 0)
	sampler := &gltf.AnimationSampler{
		Input:  modeler.WriteAccessor(doc, gltf.TargetNone, []float32{0, 1}),
		Output: modeler.WriteAccessor(doc, gltf.TargetNone, []float32{0, 1}),
	}
	doc.Animations = append(doc.Animations, &gltf.Animation{
		Name:     "smile",
		Samplers: []*gltf.AnimationSampler{sampler},
		Channels: []*gltf.AnimationChannel{{
			Sampler: 0,
			Target:  gltf.AnimationChannelTarget{Node: gltf.Index(0), Path: gltf.TRSWeights},
		}},
	})
	model := convertTest(t, doc)
	if model.animation.jointCount != 0 {
		t.Errorf("jointCount = %d, want none: weights reshape a mesh and leave the node where it was",
			model.animation.jointCount)
	}
	if model.primitives[0].skinned {
		t.Error("a morph-only node is not skinned")
	}
}

// A static prop bolted to a moving bone moves with it. The joint rule is about
// whether a node's world transform varies, and that is inherited - so a mesh
// node under an animated parent needs a joint of its own even though no channel
// names it.
func TestBakeAnimationGivesAJointToAMeshUnderAnAnimatedParent(t *testing.T) {
	doc := testDoc()
	mesh := triangleMesh(doc, nil)
	doc.Nodes = []*gltf.Node{
		{Name: "turret", Children: []int{1}},
		{Name: "barrel", Mesh: gltf.Index(mesh), Translation: [3]float64{0, 0, 2}},
	}
	sceneOf(doc, 0)
	rotationClip(doc, "traverse", 0, []float32{0, 1}, [][4]float32{{0, 0, 0, 1}, {0, 0, 1, 0}})
	model := convertTest(t, doc)
	if !model.primitives[0].skinned {
		t.Fatal("a mesh under an animated parent draws through a joint, or it freezes while its parent moves")
	}
	barrel := jointNamed(t, model, "barrel")
	if got := model.animation.poses[barrel].Translation; got != (m.Vec4{Z: 2}) {
		t.Errorf("barrel rest pose = %v, want its authored place under the turret", got)
	}
}

// jointNamed finds a joint by the node it follows, because the numbering is an
// allocation order rather than a contract.
func jointNamed(t *testing.T, model *loadedModel, name string) int {
	t.Helper()
	for joint, joined := range model.animation.jointNames {
		if joined == name {
			return joint
		}
	}
	t.Fatalf("no joint follows node %q; the model has %v", name, model.animation.jointNames)
	return 0
}

// A scale keyframe of zero is ordinary - three of the Khronos
// InterpolationTest's nine cubes do it - and a TRS record holds it exactly. It
// must not be treated as unrepresentable, because falling back to the identity
// would pop an object animated down to nothing back to full size.
func TestBakeAnimationHoldsAScaleThatReachesZero(t *testing.T) {
	doc := testDoc()
	mesh := triangleMesh(doc, nil)
	doc.Nodes = []*gltf.Node{{Name: "shrink", Mesh: gltf.Index(mesh)}}
	sceneOf(doc, 0)
	sampler := &gltf.AnimationSampler{
		Input:  modeler.WriteAccessor(doc, gltf.TargetNone, []float32{0, 1}),
		Output: modeler.WriteAccessor(doc, gltf.TargetNone, [][3]float32{{1, 1, 1}, {0, 0, 0}}),
	}
	doc.Animations = append(doc.Animations, &gltf.Animation{
		Name:     "shrink",
		Samplers: []*gltf.AnimationSampler{sampler},
		Channels: []*gltf.AnimationChannel{{
			Sampler: 0,
			Target:  gltf.AnimationChannelTarget{Node: gltf.Index(0), Path: gltf.TRSScale},
		}},
	})
	model := convertTest(t, doc)
	clip := model.animation.clips[0]
	if got := model.animation.poses[clip.row(clip.frames-1, 1)].Scale; got != (m.Vec4{}) {
		t.Errorf("the last frame scales by %v, want zero", got)
	}
	for _, err := range model.reports {
		if _, ok := err.(ErrModelPoseApproximated); ok {
			t.Errorf("reported %v; a zero scale is exactly representable", err)
		}
	}
}

// STEP holds its value to the next key. Resampling it at the grid rate is
// exactly what a 30 Hz grid degrades visibly and why the rate is 60.
func TestAnimCurveHonoursStepInterpolation(t *testing.T) {
	curve := &animCurve{
		times:  []float32{0, 1},
		values: []attrValue{{0, 0, 0, 1}, {0, 0, 1, 0}},
		mode:   gltf.InterpolationStep,
	}
	if got := curve.sample(0.9); got != (m.Vec4{W: 1}) {
		t.Errorf("STEP at 0.9 = %v, want the first key held", got)
	}
	if got := curve.sample(1); got != (m.Vec4{Z: 1}) {
		t.Errorf("STEP at the last key = %v, want the last key", got)
	}
}

// CUBICSPLINE stores three values per key - in tangent, value, out tangent -
// and its tangents are per unit of the segment's own parameter, so both scale
// by the segment's length in seconds.
func TestAnimCurveEvaluatesCubicSplineSegments(t *testing.T) {
	curve := &animCurve{
		times: []float32{0, 2},
		// Per key: in tangent, value, out tangent. Key 0 leaves at a slope of
		// one and key 1 arrives flat, which is what makes the midpoint differ
		// from the linear answer at all.
		values: []attrValue{
			{0, 0, 0, 0}, {0, 0, 0, 0}, {1, 0, 0, 0},
			{0, 0, 0, 0}, {1, 0, 0, 0}, {0, 0, 0, 0},
		},
		mode: gltf.InterpolationCubicSpline,
	}
	// At the endpoints a Hermite segment is its keyframe values exactly, which
	// is the property a wrong tangent scale still preserves - so the midpoint
	// is what the assertion is really about.
	if got := curve.sample(0).X; got != 0 {
		t.Errorf("start = %v, want the first key", got)
	}
	if got := curve.sample(2).X; got != 1 {
		t.Errorf("end = %v, want the last key", got)
	}
	// 0.125*(1*2) + 0.5*1 - 0.125*(0*2) = 0.75, where plain linear
	// interpolation between the same two values would give 0.5.
	if got := curve.sample(1).X; math.Abs(float64(got)-0.75) > 1e-5 {
		t.Errorf("midpoint = %v, want 0.75", got)
	}
}

// A clip's channels override the node's authored transform one component at a
// time, so a rotation-only clip leaves the node's own translation in place.
func TestBakeAnimationKeepsUnanimatedComponentsOfAnAnimatedNode(t *testing.T) {
	doc := testDoc()
	mesh := triangleMesh(doc, nil)
	doc.Nodes = []*gltf.Node{
		{Name: "door", Mesh: gltf.Index(mesh), Translation: [3]float64{0, 4, 0}},
	}
	sceneOf(doc, 0)
	rotationClip(doc, "open", 0, []float32{0, 1}, [][4]float32{{0, 0, 0, 1}, {0, 0, 1, 0}})
	model := convertTest(t, doc)
	clip := model.animation.clips[0]
	for _, frame := range []int{0, clip.frames / 2, clip.frames - 1} {
		got := model.animation.poses[clip.base+frame].Translation
		if got != (m.Vec4{Y: 4}) {
			t.Errorf("frame %d sits at %v, want the authored four units up", frame, got)
		}
	}
}

// Weights that do not sum to one are a drift real files carry, and unnormalised
// linear blend skinning shrinks or inflates the mesh. Normalising at load is
// one pass over data already in cache and removes a divide from the per-vertex
// path.
func TestBakeAnimationNormalisesVertexWeights(t *testing.T) {
	doc := skinnedDoc()
	doc.Meshes[0].Primitives[0].Attributes[gltf.WEIGHTS_0] =
		modeler.WriteWeights(doc, [][4]float32{{3, 1, 0, 0}, {1, 1, 0, 0}, {2, 0, 0, 0}})
	model := convertTest(t, doc)
	geometry := &model.geometries[model.primitives[0].geometry]
	for i, vertex := range geometry.vertices {
		total := vertex.Weights.X + vertex.Weights.Y + vertex.Weights.Z + vertex.Weights.W
		if math.Abs(float64(total)-1) > 1e-6 {
			t.Errorf("vertex %d weights sum to %v, want one", i, total)
		}
	}
	if got := geometry.vertices[0].Weights.X; math.Abs(float64(got)-0.75) > 1e-6 {
		t.Errorf("vertex 0 keeps its ratio as %v, want 0.75", got)
	}
}

// Shear is what TRS cannot hold, and the load bakes the decomposition anyway: a
// slightly wrong elbow beats a missing character. The report is what keeps the
// approximation from being silent.
func TestBakeAnimationReportsAnUnrepresentablePose(t *testing.T) {
	doc := testDoc()
	mesh := triangleMesh(doc, nil)
	doc.Nodes = []*gltf.Node{
		// A non-uniformly scaled parent with a rotated child is the ordinary
		// way a real file produces shear.
		{Name: "squashed", Children: []int{1}, Scale: [3]float64{3, 1, 1}},
		{Name: "bone", Mesh: gltf.Index(mesh), Rotation: [4]float64{0, 0, 0.3826834, 0.9238795}},
	}
	sceneOf(doc, 0)
	rotationClip(doc, "wobble", 0, []float32{0, 1}, [][4]float32{{0, 0, 0, 1}, {0, 0, 1, 0}})
	model := convertTest(t, doc)
	var approximated ErrModelPoseApproximated
	found := false
	for _, err := range model.reports {
		if errors.As(err, &approximated) {
			found = true
		}
	}
	if !found {
		t.Fatalf("reports = %v, want one approximated pose", model.reports)
	}
	// Reported once per model, however many joints and frames carry it.
	count := 0
	for _, err := range model.reports {
		if errors.As(err, &approximated) {
			count++
		}
	}
	if count != 1 {
		t.Errorf("reported %d times, want once per model", count)
	}
}
