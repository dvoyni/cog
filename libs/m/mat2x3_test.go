package m

import (
	"encoding/binary"
	"math"
	"testing"
	"unsafe"
)

func TestMat2x3MulAppliesItsArgumentFirst(t *testing.T) {
	translation := Translation2x3(10, 0)
	rotation := Rotation2x3(math.Pi / 2)
	point := Vec2{X: 1}

	if got, want := translation.Mul(rotation).TransformPoint(point), (Vec2{X: 10, Y: 1}); !vec2Near(got, want) {
		t.Fatalf("translation·rotation moved %v to %v, want %v: turned, then moved", point, got, want)
	}
	if got, want := rotation.Mul(translation).TransformPoint(point), (Vec2{Y: 11}); !vec2Near(got, want) {
		t.Fatalf("rotation·translation moved %v to %v, want %v: moved, then turned", point, got, want)
	}
}

func TestTRS2x3ComposesTranslationRotationScale(t *testing.T) {
	translation := Vec2{X: 3, Y: -2}
	var rotation float32 = 0.8
	scale := Vec2{X: 2, Y: -0.5}

	want := Translation2x3(translation.X, translation.Y).Mul(Rotation2x3(rotation)).Mul(Scaling2x3(scale.X, scale.Y))
	assertMat2x3Near(t, TRS2x3(translation, rotation, scale), want)
}

func TestMat2x3InverseUndoesARotatedScaledTransform(t *testing.T) {
	matrix := TRS2x3(Vec2{X: 7, Y: -4}, 1.3, Vec2{X: 3, Y: 0.25})
	inverse, ok := matrix.Inverse()
	if !ok {
		t.Fatal("a rotated, scaled transform should invert")
	}

	identity := Translation2x3(0, 0)
	assertMat2x3Near(t, matrix.Mul(inverse), identity)
	assertMat2x3Near(t, inverse.Mul(matrix), identity)
}

func TestMat2x3InverseRefusesACollapsedAxis(t *testing.T) {
	if _, ok := Scaling2x3(2, 0).Inverse(); ok {
		t.Fatal("a transform with a zero scale should not invert")
	}
}

func TestMat2x3NegativeScaleMirrorsAboutTheTranslation(t *testing.T) {
	matrix := TRS2x3(Vec2{X: 100, Y: 50}, 0, Vec2{X: -1, Y: 1})

	if got, want := matrix.TransformPoint(Vec2{X: 4, Y: 3}), (Vec2{X: 96, Y: 53}); !vec2Near(got, want) {
		t.Fatalf("mirrored point = %v, want %v", got, want)
	}
	inverse, ok := matrix.Inverse()
	if !ok {
		t.Fatal("a mirroring transform should invert")
	}
	assertMat2x3Near(t, inverse, TRS2x3(Vec2{X: 100, Y: -50}, 0, Vec2{X: -1, Y: 1}))
}

func TestMat2x3Mat3IsTheHomogeneousForm(t *testing.T) {
	matrix := TRS2x3(Vec2{X: -3, Y: 8}, 2.1, Vec2{X: 1.5, Y: 4})
	point := Vec2{X: 6, Y: -2}

	homogeneous := matrix.Mat3().MulVec3(Vec3{X: point.X, Y: point.Y, Z: 1})
	if want := matrix.TransformPoint(point); !vec3Near(homogeneous, Vec3{X: want.X, Y: want.Y, Z: 1}) {
		t.Fatalf("Mat3 moved %v to %v, want %v with a w of 1", point, homogeneous, want)
	}
	product := matrix.Mul(matrix).Mat3()
	productOfMat3s := matrix.Mat3().Mul(matrix.Mat3())
	assertElementsNear(t, product[:], productOfMat3s[:])
}

// WGSL's mat3x2<f32> is three columns of vec2<f32>, each 8 bytes at an
// alignment of 8, so column c's row r is at byte 8c+4r of its 24.
func TestMat2x3BytesAreWGSLMat3x2(t *testing.T) {
	matrix := Translation2x3(5, 7).Mul(Scaling2x3(2, 3))
	if size := unsafe.Sizeof(matrix); size != 24 {
		t.Fatalf("size = %d bytes, want mat3x2<f32>'s 24", size)
	}

	bytes := unsafe.Slice((*byte)(unsafe.Pointer(&matrix)), unsafe.Sizeof(matrix))
	element := func(column, row int) float32 {
		return math.Float32frombits(binary.LittleEndian.Uint32(bytes[8*column+4*row:]))
	}
	want := [3][2]float32{{2, 0}, {0, 3}, {5, 7}}
	for column, rows := range want {
		for row, value := range rows {
			if got := element(column, row); got != value {
				t.Fatalf("column %d row %d = %v, want %v", column, row, got, value)
			}
		}
	}
}
