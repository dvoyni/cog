package m

import (
	"math"
	"testing"
)

func TestPlaneNormalizeAndSignedDistance(t *testing.T) {
	plane := Plane{Normal: Vec3{Y: 3}, Distance: -6}.Normalize()
	if !near(plane.Normal.Length(), 1) {
		t.Fatalf("normalized normal = %v, want unit length", plane.Normal)
	}
	if got := plane.SignedDistance(Vec3{Y: 5}); !near(got, 3) {
		t.Fatalf("SignedDistance in front = %v, want 3", got)
	}
	if got := plane.SignedDistance(Vec3{Y: 2}); !near(got, 0) {
		t.Fatalf("SignedDistance on the plane = %v, want 0", got)
	}
	if got := plane.SignedDistance(Vec3{}); !near(got, -2) {
		t.Fatalf("SignedDistance behind = %v, want -2", got)
	}
	if got := (Plane{}).Normalize(); got != (Plane{}) {
		t.Fatalf("degenerate Normalize = %v, want the zero plane unchanged", got)
	}
}

// testFrustum looks down -Z from (0, 0, 5) with a 90 degree vertical field of
// view, so the frustum spans world z in [4, -5] and half an extent equal to the
// view depth at every slice.
func testFrustum() Frustum {
	return FrustumFromMat4(Perspective4(math.Pi/2, 1, 1, 10).Mul(LookAt4(Vec3{Z: 5}, Vec3{}, Vec3{Y: 1})))
}

func TestFrustumFromMat4YieldsSixNormalizedPlanes(t *testing.T) {
	frustum := testFrustum()
	if len(frustum) != 6 {
		t.Fatalf("frustum has %d planes, want 6", len(frustum))
	}
	for index, plane := range frustum {
		if !near(plane.Normal.Length(), 1) {
			t.Fatalf("plane %d normal = %v, want unit length", index, plane.Normal)
		}
	}
}

func TestFrustumContainsSphereTestsAllSixPlanes(t *testing.T) {
	frustum := testFrustum()
	cases := []struct {
		name   string
		center Vec3
		radius float32
		want   bool
	}{
		{"at the focus", Vec3{}, 0.5, true},
		{"beyond far", Vec3{Z: -6}, 0.5, false},
		{"straddling far", Vec3{Z: -6}, 2, true},
		{"behind near", Vec3{Z: 5.5}, 0.25, false},
		{"beyond right", Vec3{X: 20}, 1, false},
		{"beyond left", Vec3{X: -20}, 1, false},
		{"beyond top", Vec3{Y: 20}, 1, false},
		{"beyond bottom", Vec3{Y: -20}, 1, false},
	}
	for _, testCase := range cases {
		if got := frustum.ContainsSphere(testCase.center, testCase.radius); got != testCase.want {
			t.Fatalf("ContainsSphere(%s) = %v, want %v", testCase.name, got, testCase.want)
		}
	}
}

func TestSphereTransformScalesByLargestAxis(t *testing.T) {
	sphere := Sphere{Center: Vec3{X: 1}, Radius: 1}
	got := sphere.Transform(Translation4(1, 2, 3).Mul(Scaling4(2, 3, 4)))
	if !vec3Near(got.Center, Vec3{3, 2, 3}) {
		t.Fatalf("center = %v, want (3, 2, 3)", got.Center)
	}
	if !near(got.Radius, 4) {
		t.Fatalf("radius = %v, want 4, the largest axis scale", got.Radius)
	}
}

func TestBox3SphereUnionAndTransform(t *testing.T) {
	box := Box3{Min: Vec3{-1, -2, -3}, Max: Vec3{1, 2, 3}}

	sphere := box.Sphere()
	if !vec3Near(sphere.Center, Vec3{}) || !near(sphere.Radius, sqrt(14)) {
		t.Fatalf("Sphere = %v, want center (0, 0, 0) radius %v", sphere, sqrt(14))
	}

	union := box.Union(Box3{Min: Vec3{0, 0, 0}, Max: Vec3{5, 5, 5}})
	if union != (Box3{Min: Vec3{-1, -2, -3}, Max: Vec3{5, 5, 5}}) {
		t.Fatalf("Union = %v", union)
	}

	rotated := box.Transform(RotationZ4(math.Pi / 2))
	if !vec3Near(rotated.Min, Vec3{-2, -1, -3}) || !vec3Near(rotated.Max, Vec3{2, 1, 3}) {
		t.Fatalf("Transform = %v, want the refitted quarter-turn box", rotated)
	}

	moved := box.Transform(Translation4(10, 0, 0))
	if !vec3Near(moved.Min, Vec3{9, -2, -3}) || !vec3Near(moved.Max, Vec3{11, 2, 3}) {
		t.Fatalf("translated Transform = %v", moved)
	}
}

func TestNewRayNormalizesDirection(t *testing.T) {
	ray := NewRay(Vec3{1, 2, 3}, Vec3{0, 0, -4})
	if !vec3Near(ray.Dir, Vec3{Z: -1}) {
		t.Fatalf("Dir = %v, want unit length", ray.Dir)
	}
	if got := ray.At(2); !vec3Near(got, Vec3{1, 2, 1}) {
		t.Fatalf("At(2) = %v, want (1, 2, 1)", got)
	}
}

func TestRayTransformKeepsTUnderRigidTransformOnly(t *testing.T) {
	local := NewRay(Vec3{}, Vec3{Z: -1})
	sphere := Sphere{Center: Vec3{Z: -5}, Radius: 1}
	wantT, ok := local.IntersectSphere(sphere)
	if !ok || !near(wantT, 4) {
		t.Fatalf("local t = %v, %v, want 4", wantT, ok)
	}

	rigid := Translation4(3, -2, 7).Mul(RotationY4(0.6))
	rigidT, ok := local.Transform(rigid).IntersectSphere(sphere.Transform(rigid))
	if !ok || !near(rigidT, wantT) {
		t.Fatalf("t under a rigid transform = %v, %v, want %v", rigidT, ok, wantT)
	}

	// t does not survive a scaling transform, because Transform renormalizes Dir.
	scaling := Scaling4(2, 2, 2)
	scaledT, ok := local.Transform(scaling).IntersectSphere(sphere.Transform(scaling))
	if !ok || !near(scaledT, 2*wantT) {
		t.Fatalf("t under a scaling transform = %v, %v, want %v", scaledT, ok, 2*wantT)
	}

	if got := rigid.TransformRay(local); got != local.Transform(rigid) {
		t.Fatalf("Mat4.TransformRay = %v, want the same as Ray.Transform", got)
	}
}

func TestIntersectSphere(t *testing.T) {
	sphere := Sphere{Center: Vec3{Z: -5}, Radius: 1}

	if got, ok := NewRay(Vec3{}, Vec3{Z: -1}).IntersectSphere(sphere); !ok || !near(got, 4) {
		t.Fatalf("front hit = %v, %v, want 4", got, ok)
	}
	if got, ok := NewRay(Vec3{}, Vec3{Z: 1}).IntersectSphere(sphere); ok {
		t.Fatalf("a hit behind the origin should be rejected, got %v", got)
	}
	if got, ok := NewRay(Vec3{Z: -5}, Vec3{X: 1}).IntersectSphere(sphere); !ok || got != 0 {
		t.Fatalf("from inside = %v, %v, want 0, true", got, ok)
	}
	if got, ok := NewRay(Vec3{X: 10}, Vec3{Z: -1}).IntersectSphere(sphere); ok {
		t.Fatalf("a miss should be rejected, got %v", got)
	}
}

func TestIntersectPlane(t *testing.T) {
	ground := Plane{Normal: Vec3{Y: 1}}

	if got, ok := NewRay(Vec3{Y: 5}, Vec3{Y: -1}).IntersectPlane(ground); !ok || !near(got, 5) {
		t.Fatalf("downward hit = %v, %v, want 5", got, ok)
	}
	if got, ok := NewRay(Vec3{Y: 5}, Vec3{Y: 1}).IntersectPlane(ground); ok {
		t.Fatalf("a hit behind the origin should be rejected, got %v", got)
	}
	if got, ok := NewRay(Vec3{Y: 5}, Vec3{X: 1}).IntersectPlane(ground); ok {
		t.Fatalf("a parallel ray should miss, got %v", got)
	}
	if got, ok := NewRay(Vec3{}, Vec3{X: 1}).IntersectPlane(ground); ok {
		t.Fatalf("a ray lying in the plane should miss, got %v", got)
	}
	if got, ok := NewRay(Vec3{}, Vec3{Y: -1}).IntersectPlane(ground); !ok || got != 0 {
		t.Fatalf("a ray starting on the plane = %v, %v, want 0, true", got, ok)
	}
}

func TestIntersectBox3(t *testing.T) {
	box := Box3{Min: Vec3{-1, -1, -1}, Max: Vec3{1, 1, 1}}

	if got, ok := NewRay(Vec3{X: 5}, Vec3{X: -1}).IntersectBox3(box); !ok || !near(got, 4) {
		t.Fatalf("axis-aligned hit = %v, %v, want 4", got, ok)
	}
	if got, ok := NewRay(Vec3{}, Vec3{X: 1}).IntersectBox3(box); !ok || got != 0 {
		t.Fatalf("from inside = %v, %v, want 0, true", got, ok)
	}
	if got, ok := NewRay(Vec3{X: -5}, Vec3{X: -1}).IntersectBox3(box); ok {
		t.Fatalf("a hit behind the origin should be rejected, got %v", got)
	}
	if got, ok := NewRay(Vec3{X: 5, Y: 5}, Vec3{X: -1}).IntersectBox3(box); ok {
		t.Fatalf("a ray parallel to a slab and outside it should miss, got %v", got)
	}
	if got, ok := NewRay(Vec3{X: 3, Y: 3, Z: 3}, Vec3{-1, -1, -1}).IntersectBox3(box); !ok || !near(got, 2*sqrt(3)) {
		t.Fatalf("diagonal hit = %v, %v, want %v", got, ok, 2*sqrt(3))
	}
}

func TestVec3MinMaxAbsAndVec4Conversions(t *testing.T) {
	first, second := Vec3{1, -5, 3}, Vec3{-2, 4, 3}
	if got, want := first.Min(second), (Vec3{-2, -5, 3}); got != want {
		t.Fatalf("Min = %v, want %v", got, want)
	}
	if got, want := first.Max(second), (Vec3{1, 4, 3}); got != want {
		t.Fatalf("Max = %v, want %v", got, want)
	}
	if got, want := first.Abs(), (Vec3{1, 5, 3}); got != want {
		t.Fatalf("Abs = %v, want %v", got, want)
	}
	if got, want := first.Vec4(1), (Vec4{1, -5, 3, 1}); got != want {
		t.Fatalf("Vec4 = %v, want %v", got, want)
	}
	if got, want := (Vec4{1, 2, 3, 4}).Vec3(), (Vec3{1, 2, 3}); got != want {
		t.Fatalf("Vec3 = %v, want %v", got, want)
	}
}
