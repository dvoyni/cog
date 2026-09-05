package scene

import (
	"errors"
	"testing"
	"unsafe"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/m"
)

func TestInstanceRecordIsSixtyFourBytesWithEightSpare(t *testing.T) {
	if size := unsafe.Sizeof(sceneInstance{}); size != 64 {
		t.Fatalf("sceneInstance is %d bytes, want 64", size)
	}
	if spare := unsafe.Sizeof(sceneInstance{}.Spare); spare != 8 {
		t.Fatalf("sceneInstance has %d spare bytes, want 8", spare)
	}
}

func TestPackInstanceWritesTheWorldMatrixAsThreeRows(t *testing.T) {
	transform := Transform{Position: m.Vec3{X: 1, Y: 2, Z: 3}}
	instance := packInstance(transform.Mat4())
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

func TestPackInstanceSkipsAnimationAndSkinningForABufferBuiltDraw(t *testing.T) {
	instance := packInstance(m.Mat4{})
	if instance.AnimOffset != sceneNoAnim {
		t.Fatalf("animOffset is %d, want SCENE_NO_ANIM (%d)", instance.AnimOffset, sceneNoAnim)
	}
	if instance.Flags&sceneNoSkin == 0 {
		t.Fatalf("flags %#b lack SCENE_NOSKIN", instance.Flags)
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
			instance := packInstance(test.matrix)
			if got := instance.Flags&sceneNonUniform != 0; got != test.nonUniform {
				t.Fatalf("SCENE_NONUNIFORM is %v, want %v", got, test.nonUniform)
			}
		})
	}
}

func TestArenaAlignsEveryRecordToTheStorageAlignment(t *testing.T) {
	var a arena
	first := a.appendRecord(&sceneInstance{})
	second := a.appendRecord(&sceneFrameBlock{})
	if first != 0 {
		t.Fatalf("first record is at %d, want 0", first)
	}
	if second%gfx.StorageAlignment != 0 {
		t.Fatalf("second record is at %d, which is not %d-aligned", second, gfx.StorageAlignment)
	}
	if len(a.bytes()) < second+int(unsafe.Sizeof(sceneFrameBlock{})) {
		t.Fatalf("arena is %d bytes, too short for the record at %d", len(a.bytes()), second)
	}
}

func TestArenaReusesItsBackingAcrossFrames(t *testing.T) {
	var a arena
	a.appendRecord(&sceneFrameBlock{})
	backing := unsafe.SliceData(a.data[:cap(a.data)])
	a.reset()
	a.appendRecord(&sceneFrameBlock{})
	if unsafe.SliceData(a.data[:cap(a.data)]) != backing {
		t.Fatal("the arena reallocated instead of reusing its backing")
	}
}

// The record's offsets are one half of a contract with the WGSL struct, which
// is asserted against the same numbers where the shader is reflected. Nothing
// sits between the two halves: scene packs bytes, the shader reads them, and a
// mismatch renders a plausible wrong picture.
func TestPbrRecordMatchesItsShaderSideOffsets(t *testing.T) {
	if size := unsafe.Sizeof(scenePbrRecord{}); size != 160 {
		t.Fatalf("scenePbrRecord is %d bytes, want 160", size)
	}
	var record scenePbrRecord
	for _, test := range []struct {
		name   string
		offset uintptr
		want   uintptr
	}{
		{name: "baseColorFactor", offset: unsafe.Offsetof(record.BaseColorFactor), want: 0},
		{name: "emissiveFactor", offset: unsafe.Offsetof(record.EmissiveFactor), want: 16},
		{name: "baseColorTransform", offset: unsafe.Offsetof(record.Transforms), want: 32},
		{name: "baseColorRotation", offset: unsafe.Offsetof(record.Rotations), want: 112},
		{name: "metallicFactor", offset: unsafe.Offsetof(record.MetallicFactor), want: 132},
		{name: "roughnessFactor", offset: unsafe.Offsetof(record.RoughnessFactor), want: 136},
		{name: "normalScale", offset: unsafe.Offsetof(record.NormalScale), want: 140},
		{name: "occlusionStrength", offset: unsafe.Offsetof(record.OcclusionStrength), want: 144},
		{name: "alphaCutoff", offset: unsafe.Offsetof(record.AlphaCutoff), want: 148},
		{name: "uvSets", offset: unsafe.Offsetof(record.UVSets), want: 152},
	} {
		if test.offset != test.want {
			t.Errorf("%s is at offset %d, want %d", test.name, test.offset, test.want)
		}
	}
}

// The defaults are glTF's own, so a material that names nothing renders as
// glTF says an empty material renders: white, fully rough, fully metallic, with
// every texture slot multiplying through unchanged.
func TestThePbrRecordDefaultsToGltfsOwnDefaults(t *testing.T) {
	record := defaultPbrRecord()
	if record.BaseColorFactor != (m.Vec4{X: 1, Y: 1, Z: 1, W: 1}) {
		t.Errorf("baseColorFactor is %v, want white", record.BaseColorFactor)
	}
	if record.EmissiveFactor != (m.Vec4{}) {
		t.Errorf("emissiveFactor is %v, want black", record.EmissiveFactor)
	}
	if record.MetallicFactor != 1 || record.RoughnessFactor != 1 {
		t.Errorf("metallic/roughness are %v/%v, want 1/1", record.MetallicFactor, record.RoughnessFactor)
	}
	if record.NormalScale != 1 || record.OcclusionStrength != 1 {
		t.Errorf("normalScale/occlusionStrength are %v/%v, want 1/1", record.NormalScale, record.OcclusionStrength)
	}
	// Zero rather than glTF's 0.5, because the discard is unconditional in the
	// one bundled module: an opaque material must cut nothing away, and alpha
	// is never below zero. A MASK material sets its own cutoff.
	if record.AlphaCutoff != 0 {
		t.Errorf("alphaCutoff is %v, want 0 so the discard is a no-op for an opaque material", record.AlphaCutoff)
	}
	for slot, transform := range record.Transforms {
		if transform != (m.Vec4{Z: 1, W: 1}) {
			t.Errorf("slot %d transform is %v, want zero offset and unit scale", slot, transform)
		}
	}
	if record.UVSets != 0 {
		t.Errorf("uvSets is %#b, want every slot on TEXCOORD_0", record.UVSets)
	}
}

// UV sets are capped at two, and the selector is one bit per slot.
func TestUVSetSelectionIsOneBitPerSlotAndCapsAtTwo(t *testing.T) {
	record := defaultPbrRecord()
	var reported []error
	report := func(err error) { reported = append(reported, err) }

	record.selectUVSet(report, normalSlot, 1)
	if record.UVSets != 1<<normalSlot {
		t.Fatalf("uvSets is %#b, want only the normal slot on TEXCOORD_1", record.UVSets)
	}
	if len(reported) != 0 {
		t.Fatalf("selecting TEXCOORD_1 reported %v", reported)
	}

	record.selectUVSet(report, 0, 2)
	if record.UVSets&1 != 0 {
		t.Fatalf("uvSets is %#b, want the out-of-range slot back on TEXCOORD_0", record.UVSets)
	}
	if len(reported) != 1 {
		t.Fatalf("a texCoord past the cap reported %d errors, want 1: %v", len(reported), reported)
	}
	var unsupported ErrTextureUVSetUnsupported
	if !errors.As(reported[0], &unsupported) || unsupported.TexCoord != 2 {
		t.Fatalf("reported %v, want ErrTextureUVSetUnsupported for TEXCOORD_2", reported[0])
	}
}

// Intensity is premultiplied into every light colour at pack time: it removes a
// per-fragment multiply and costs nothing.
func TestTheFrameBlockPremultipliesSunAndAmbientIntensity(t *testing.T) {
	descr := CameraDescr{
		SunDirection:     m.Vec3{Y: -2},
		SunColor:         m.NewColorLinear(1, 0.5, 0.25, 1),
		SunIntensity:     2,
		AmbientSky:       m.NewColorLinear(0.2, 0.4, 0.6, 1),
		AmbientGround:    m.NewColorLinear(0.1, 0.1, 0.1, 1),
		AmbientIntensity: 0.5,
	}
	block := packFrameLighting(sceneFrameBlock{}, descr)

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
	block := packFrameLighting(sceneFrameBlock{}, CameraDescr{
		SunDirection: m.Vec3{Z: -1},
		SunColor:     m.NewColorLinear(1, 1, 1, 1),
		AmbientSky:   m.NewColorLinear(0.5, 0.5, 0.5, 1),
	})
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
	block := packFrameLighting(sceneFrameBlock{}, CameraDescr{
		SunColor: m.NewColorLinear(1, 1, 1, 1), SunIntensity: 3,
	})
	if block.SunColor != (m.Vec4{}) {
		t.Errorf("sunColor is %v, want black for a camera with no sun direction", block.SunColor)
	}
	if block.SunDirection != (m.Vec4{}) {
		t.Errorf("sunDirection is %v, want zero", block.SunDirection)
	}
}
