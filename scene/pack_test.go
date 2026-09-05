package scene

import (
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
