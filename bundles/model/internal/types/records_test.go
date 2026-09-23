package types

import (
	"encoding/binary"
	"math"
	"testing"
	"unsafe"

	"github.com/dvoyni/cog/libs/m"
)

// The sizes a renderer builds binding ranges and record indices from are the
// sizes the shader declares. The offsets inside each record are asserted
// against the WGSL itself in model/internal; these pin the totals a binding
// range is cut at.
func TestEveryRecordSizeIsTheOneTheShaderDeclares(t *testing.T) {
	for _, test := range []struct {
		name      string
		got, want int
	}{
		{"Instance", InstanceSize, 64},
		{"Light", LightSize, 48},
		{"FrameBlock", FrameBlockSize, 304 + MaxLights*48},
		{"SceneMesh", SceneMeshSize, 32},
		{"SceneAnimHeader", SceneAnimHeaderSize, 32},
		{"ScenePlayRecord", ScenePlayRecordSize, 16},
		{"SceneMorphWeight", SceneMorphWeightSize, 8},
		{"AnimHeaderVec4s", AnimHeaderVec4s, 2},
		{"PlayRecordVec4s", PlayRecordVec4s, 1},
	} {
		if test.got != test.want {
			t.Errorf("%s is %d, want %d", test.name, test.got, test.want)
		}
	}
}

// The record stays 64 bytes and both of its spare words are now spent: the node
// joint moved out of every vertex of a plain-bound mesh, and the mesh index
// names the per-mesh record the geometry's UVs decode against. There is nothing
// left, which is the fact this test now records - the next thing that wants
// per-instance data pays 128 bytes or a repack of what is already here.
func TestInstanceRecordIsSixtyFourFullyAllocatedBytes(t *testing.T) {
	var instance Instance
	if size := unsafe.Sizeof(instance); size != 64 {
		t.Fatalf("Instance is %d bytes, want 64", size)
	}
	named := unsafe.Sizeof(instance.World0) + unsafe.Sizeof(instance.World1) +
		unsafe.Sizeof(instance.World2) + unsafe.Sizeof(instance.AnimOffset) +
		unsafe.Sizeof(instance.Flags) + unsafe.Sizeof(instance.Joint) +
		unsafe.Sizeof(instance.Mesh)
	if named != 64 {
		t.Fatalf("Instance's named fields are %d of its 64 bytes, want every one of them", named)
	}
}

func TestPackInstanceWritesTheWorldMatrixAsThreeRows(t *testing.T) {
	transform := m.Transform{Position: m.Vec3{X: 1, Y: 2, Z: 3}}
	instance := PackInstance(transform.Mat4(), InstanceAnim{Offset: SceneNoAnim}, 0)
	want := [3]m.Vec4{
		{X: 1, W: 1},
		{Y: 1, W: 2},
		{Z: 1, W: 3},
	}
	got := [3]m.Vec4{instance.World0, instance.World1, instance.World2}
	if got != want {
		t.Fatalf("world rows are %v, want %v", got, want)
	}
}

// PackInstance's rows are pinned against a matrix whose transpose is a
// different matrix. The test above cannot: a pure translation's basis is the
// identity, and the identity is symmetric, so a record packed as columns would
// pass it unchanged.
//
// The arithmetic here is sceneWorldPosition's, verbatim - the shader dots a
// local vec4 against World0..World2 - so this is the seam where the shader's
// reading of the record and m's own transform are held to the same answer.
func TestPackInstanceRowsTransformLikeTheMatrix(t *testing.T) {
	world := m.RotationY4(0.7).Mul(m.Translation4(1, 2, 3))
	instance := PackInstance(world, InstanceAnim{Offset: SceneNoAnim}, 0)

	point := m.Vec3{X: 0.3, Y: -1.4, Z: 2.6}
	local := m.Vec4{X: point.X, Y: point.Y, Z: point.Z, W: 1}
	got := m.Vec3{
		X: instance.World0.Dot(local),
		Y: instance.World1.Dot(local),
		Z: instance.World2.Dot(local),
	}

	want := world.TransformPoint(point)
	if !near(got.X, want.X) || !near(got.Y, want.Y) || !near(got.Z, want.Z) {
		t.Fatalf("rows transform %v to %v, want %v", point, got, want)
	}
}

// An instance that animates nothing carries SCENE_NOSKIN and SCENE_NO_ANIM,
// which is what skips the whole pose path for a debug line or a procedural
// terrain mesh rather than charging it a per-vertex fetch of an identity.
func TestPackInstanceMarksAnUnskinnedDraw(t *testing.T) {
	instance := PackInstance(m.NewMat4(), InstanceAnim{Offset: SceneNoAnim}, 0)
	if instance.Flags&SceneNoSkin == 0 {
		t.Error("a draw with no skin of its own carries SCENE_NOSKIN")
	}
	if instance.AnimOffset != SceneNoAnim {
		t.Errorf("AnimOffset = %d, want SceneNoAnim", instance.AnimOffset)
	}
	skinned := PackInstance(m.NewMat4(), InstanceAnim{Offset: 7, Skinned: true}, 0)
	if skinned.Flags&SceneNoSkin != 0 {
		t.Error("a skinned draw must not carry SCENE_NOSKIN")
	}
	if skinned.AnimOffset != 7 {
		t.Errorf("AnimOffset = %d, want the block's offset", skinned.AnimOffset)
	}
}

// A plain-bound placement is a skinned draw riding one joint, so it carries
// SCENE_PLAINJOINT and its joint, never SCENE_NOSKIN; an unskinned one names no
// joint even when handed one.
func TestPackInstanceSetsThePlainJointOrNoSkinNeverBoth(t *testing.T) {
	plain := PackInstance(m.NewMat4(), InstanceAnim{Offset: 3, Skinned: true, Plain: true, Joint: 9}, 5)
	if plain.Flags != ScenePlainJoint || plain.Joint != 9 || plain.Mesh != 5 {
		t.Errorf("a plain-bound instance packed flags %#b joint %d mesh %d, want PLAINJOINT, 9 and 5",
			plain.Flags, plain.Joint, plain.Mesh)
	}
	unskinned := PackInstance(m.NewMat4(), InstanceAnim{Offset: SceneNoAnim, Plain: true, Joint: 9}, 0)
	if unskinned.Flags != SceneNoSkin || unskinned.Joint != 0 {
		t.Errorf("an unskinned instance packed flags %#b joint %d, want NOSKIN alone and no joint",
			unskinned.Flags, unskinned.Joint)
	}
}

func TestPackInstanceFlagsNonUniformScaleOnly(t *testing.T) {
	for _, test := range []struct {
		name       string
		matrix     m.Mat4
		nonUniform bool
	}{
		{name: "identity", matrix: m.Translation4(1, 2, 3)},
		{name: "uniform scale", matrix: m.Scaling4(3, 3, 3)},
		{name: "rotated uniform scale", matrix: m.RotationY4(0.7).Mul(m.Scaling4(2, 2, 2))},
		{name: "stretched box", matrix: m.Scaling4(4, 0.05, 0.05), nonUniform: true},
		{name: "flattened", matrix: m.Scaling4(1, 1, 0.2), nonUniform: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			instance := PackInstance(test.matrix, InstanceAnim{Offset: SceneNoAnim}, 0)
			if got := instance.Flags&SceneNonUniform != 0; got != test.nonUniform {
				t.Fatalf("SCENE_NONUNIFORM is %v, want %v", got, test.nonUniform)
			}
		})
	}
}

// A surface with no material is paint, not metal: glTF's defaults with
// metallic 0 and the colour as base. Self-lit paint is black and glows.
func TestPaintIsNonMetallicAndSelfLitPaintGlows(t *testing.T) {
	color := m.NewColorLinear(0.25, 0.5, 0.75, 0.5)
	paint := paintPbrValues(color, false)
	want := defaultPbrValues()
	want.metallicFactor = 0
	want.baseColorFactor = m.Vec4{X: 0.25, Y: 0.5, Z: 0.75, W: 0.5}
	if paint != want {
		t.Errorf("paint is %+v, want %+v", paint, want)
	}
	glow := paintPbrValues(color, true)
	want.baseColorFactor = m.Vec4{W: 0.5}
	want.emissiveFactor = m.Vec4{X: 0.25, Y: 0.5, Z: 0.75}
	if glow != want {
		t.Errorf("self-lit paint is %+v, want %+v", glow, want)
	}
}

// PaintParams is paint as a renderer lays it over white paint on the draw:
// exactly the two factors that differ, by the shader's names.
func TestPaintParamsAreTheTwoFactorsPaintChanges(t *testing.T) {
	color := m.NewColorLinear(0.25, 0.5, 0.75, 0.5)
	for _, selfLit := range []bool{false, true} {
		params := PaintParams(nil, color, selfLit)
		values := paintPbrValues(color, selfLit)
		want := map[string]m.Vec4{"baseColorFactor": values.baseColorFactor, "emissiveFactor": values.emissiveFactor}
		if len(params) != len(want) {
			t.Fatalf("self-lit %v: %d params, want %d", selfLit, len(params), len(want))
		}
		for _, param := range params {
			got, ok := param.VecValue()
			if !ok || got != want[param.Name()] {
				t.Errorf("self-lit %v: %s is %v, want %v", selfLit, param.Name(), got, want[param.Name()])
			}
		}
	}
}

// Intensity is premultiplied into every light colour at pack time: it removes a
// per-fragment multiply and costs nothing.
func TestTheFrameBlockPremultipliesSunAndAmbientIntensity(t *testing.T) {
	lighting := FrameLighting{
		SunDirection:     m.Vec3{Y: -2},
		SunColor:         m.NewColorLinear(1, 0.5, 0.25, 1),
		SunIntensity:     2,
		AmbientSky:       m.NewColorLinear(0.2, 0.4, 0.6, 1),
		AmbientGround:    m.NewColorLinear(0.1, 0.1, 0.1, 1),
		AmbientIntensity: 0.5,
	}
	block := PackFrameLighting(FrameBlock{}, lighting, &LightSelection{})

	if block.SunDirection != (m.Vec4{Y: -1}) {
		t.Errorf("sunDirection is %v, want the normalised direction of travel", block.SunDirection)
	}
	if block.SunColor != (m.Vec4{X: 2, Y: 1, Z: 0.5}) {
		t.Errorf("sunColor is %v, want the colour times intensity 2", block.SunColor)
	}
	if block.AmbientSky != (m.Vec4{X: 0.1, Y: 0.2, Z: 0.3}) {
		t.Errorf("ambientSky is %v, want the colour times intensity 0.5", block.AmbientSky)
	}
	if block.AmbientGround != (m.Vec4{X: 0.05, Y: 0.05, Z: 0.05}) {
		t.Errorf("ambientGround is %v, want the colour times intensity 0.5", block.AmbientGround)
	}
}

// A zero intensity means 1, so the floor of the API - a sun direction and a
// colour - is lit.
func TestAZeroIntensityMeansOne(t *testing.T) {
	block := PackFrameLighting(FrameBlock{}, FrameLighting{
		SunDirection: m.Vec3{Z: -1},
		SunColor:     m.NewColorLinear(1, 1, 1, 1),
		AmbientSky:   m.NewColorLinear(0.5, 0.5, 0.5, 1),
	}, &LightSelection{})
	if block.SunColor != (m.Vec4{X: 1, Y: 1, Z: 1}) {
		t.Errorf("sunColor is %v, want the colour unscaled", block.SunColor)
	}
	if block.AmbientSky != (m.Vec4{X: 0.5, Y: 0.5, Z: 0.5}) {
		t.Errorf("ambientSky is %v, want the colour unscaled", block.AmbientSky)
	}
}

// A camera with no sun has no sun: the radiance is zero rather than a
// normalised nothing pointing somewhere arbitrary.
func TestNoSunDirectionMeansNoSunRadiance(t *testing.T) {
	block := PackFrameLighting(FrameBlock{}, FrameLighting{
		SunColor: m.NewColorLinear(1, 1, 1, 1), SunIntensity: 3,
	}, &LightSelection{})
	if block.SunColor != (m.Vec4{}) {
		t.Errorf("sunColor is %v, want black for a camera with no sun direction", block.SunColor)
	}
	if block.SunDirection != (m.Vec4{}) {
		t.Errorf("sunDirection is %v, want zero", block.SunDirection)
	}
}

// The renderer's view fields pass through untouched, and the lights and their
// count are the selection's: a stale entry past the count is left to the
// count to exclude, the way the shader's loop does.
func TestPackFrameLightingWritesTheSelectionAndKeepsTheView(t *testing.T) {
	view := FrameBlock{
		View:           m.Translation4(1, 2, 3),
		Projection:     m.Scaling4(2, 2, 2),
		ViewProjection: m.RotationY4(0.3),
		CameraPosition: m.Vec4{X: 4, Y: 5, Z: 6, W: 1},
		ViewDirection:  m.Vec4{Z: 1, W: 1},
		LightCount:     99,
	}
	var selection LightSelection
	first, _ := PackLight(LightDescr{Position: m.Vec3{X: 1}, Color: m.NewColorLinear(1, 1, 1, 1)})
	second, _ := PackLight(LightDescr{Position: m.Vec3{X: 2}, Color: m.NewColorLinear(1, 1, 1, 1)})
	selection.Offer(&first, 1)
	selection.Offer(&second, 2)
	block := PackFrameLighting(view, FrameLighting{}, &selection)
	if block.View != view.View || block.Projection != view.Projection ||
		block.ViewProjection != view.ViewProjection || block.CameraPosition != view.CameraPosition ||
		block.ViewDirection != view.ViewDirection {
		t.Error("the view fields did not pass through untouched")
	}
	if block.LightCount != 2 || block.Lights[0] != first || block.Lights[1] != second {
		t.Errorf("the block holds %d lights %v, want the selection's two", block.LightCount, block.Lights[:2])
	}
}

// The block is a two-vec4 header and one vec4 per play, and animOffset counts
// vec4s from the start of the whole arena - not from a per-pass range, because
// sceneInstances is bound per pass and sceneAnim is not.
func TestAppendAnimLaysTheBlockOutInVec4s(t *testing.T) {
	var arena []byte
	arena, empty := AppendAnim(arena, nil, AnimMorph{})
	if empty != SceneNoAnim || len(arena) != 0 {
		t.Errorf("an empty block is at %d and grew the arena to %d bytes, want SceneNoAnim and nothing",
			empty, len(arena))
	}
	arena, first := AppendAnim(arena, []ScenePlayRecord{{BaseRow0: 4, BaseRow1: 6, W0: 0.5, W1: 0.5}}, AnimMorph{})
	if first != 0 {
		t.Errorf("the first block is at %d, want the start of the arena", first)
	}
	arena, second := AppendAnim(arena, []ScenePlayRecord{{}, {}}, AnimMorph{})
	if want := uint32(AnimHeaderVec4s + PlayRecordVec4s); second != want {
		t.Errorf("the second block is at %d, want %d", second, want)
	}
	if got, want := len(arena), (2+1+2+2)*16; got != want {
		t.Errorf("the arena is %d bytes, want %d", got, want)
	}
	// The first block's words are where the shader reads them: the play
	// count, then the play's two rows and two folded weights after the header.
	word := func(at int) uint32 { return binary.LittleEndian.Uint32(arena[at:]) }
	if word(0) != 1 || word(4) != 0 {
		t.Errorf("the first header counts %d plays and %d targets, want 1 and 0", word(0), word(4))
	}
	if word(32) != 4 || word(36) != 6 || math.Float32frombits(word(40)) != 0.5 {
		t.Errorf("the first play reads rows %d and %d, want 4 and 6", word(32), word(36))
	}
}

// The block is a two-vec4 header, one vec4 per play, then the sparse list two
// entries to a vec4 - and the arena stays whole vec4s, because animOffset
// counts them and an odd target count would leave the next block at an offset
// no instance can name.
func TestAppendAnimLaysTheMorphListOutInWholeVec4s(t *testing.T) {
	var arena []byte
	block := AnimMorph{
		Binding: MorphBinding{Base: 7, Stride: 2, Targets: 3},
		Targets: []SceneMorphWeight{{Target: 0, Weight: 1}, {Target: 2, Weight: 0.5}},
	}
	// A morphed draw with no plays still packs a block: playCount and
	// targetCount are independently zero-checkable, with no flags bitfield.
	arena, first := AppendAnim(arena, nil, block)
	if first != 0 {
		t.Fatalf("the first block is at %d, want the start of the arena", first)
	}
	if base, stride := binary.LittleEndian.Uint32(arena[8:]), binary.LittleEndian.Uint32(arena[12:]); base != 7 || stride != 2 {
		t.Errorf("the header's morph words are %d and %d, want the binding's 7 and 2", base, stride)
	}
	// Two entries fill one vec4 exactly, so the second block follows the
	// header plus one.
	arena, second := AppendAnim(arena, nil, block)
	if want := uint32(AnimHeaderVec4s + 1); second != want {
		t.Errorf("the second block is at %d, want %d", second, want)
	}
	// An odd count pads: three entries are two vec4s, the last half empty.
	odd := AnimMorph{
		Binding: block.Binding,
		Targets: append(block.Targets, SceneMorphWeight{Target: 1, Weight: 0.25}),
	}
	arena, third := AppendAnim(arena, nil, odd)
	if want := uint32(2 * (AnimHeaderVec4s + 1)); third != want {
		t.Errorf("the third block is at %d, want %d", third, want)
	}
	// Two blocks of header plus one vec4, then one of header plus two.
	if got, want := len(arena), (2*(AnimHeaderVec4s+1)+AnimHeaderVec4s+2)*16; got != want {
		t.Errorf("the arena is %d bytes, want %d", got, want)
	}
}

// A steady frame allocates nothing: the block is appended into the caller's
// backing, which a renderer keeps across frames.
func TestAppendAnimIntoASizedArenaAllocatesNothing(t *testing.T) {
	if raceEnabled {
		t.Skip("allocation counts are not meaningful under -race")
	}
	plays := []ScenePlayRecord{{BaseRow0: 1}, {BaseRow0: 2}}
	morph := AnimMorph{Targets: []SceneMorphWeight{{Target: 1, Weight: 1}}}
	arena := make([]byte, 0, 4096)
	allocations := testing.AllocsPerRun(50, func() {
		arena, _ = AppendAnim(arena[:0], plays, morph)
	})
	if allocations != 0 {
		t.Fatalf("appending a block allocated %v times", allocations)
	}
}
