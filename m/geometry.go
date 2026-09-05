package m

import "math"

// Plane is the set of points satisfying Normal.Dot(point) + Distance == 0.
// A positive signed distance is in front of the plane, on the side Normal
// points to.
type Plane struct {
	Normal   Vec3
	Distance float32
}

// Normalize scales the plane so Normal has unit length and SignedDistance
// measures world distance. A plane with a zero normal is returned unchanged.
func (plane Plane) Normalize() Plane {
	length := plane.Normal.Length()
	if length == 0 {
		return plane
	}
	inverse := 1 / length
	return Plane{Normal: plane.Normal.MulS(inverse), Distance: plane.Distance * inverse}
}

func (plane Plane) SignedDistance(point Vec3) float32 {
	return plane.Normal.Dot(point) + plane.Distance
}

// Frustum holds the six planes of a view volume, all pointing inwards, in the
// order left, right, bottom, top, near, far.
type Frustum [6]Plane

// FrustumFromMat4 extracts the six planes of a view-projection by row
// combination. The near plane comes from row 2 alone rather than row 3 plus row
// 2, because Perspective4 and Orthographic4 write clip depth in 0..1 rather than
// -1..1.
func FrustumFromMat4(viewProjection Mat4) Frustum {
	row := func(index int) Vec4 {
		return Vec4{viewProjection[index], viewProjection[index+4], viewProjection[index+8], viewProjection[index+12]}
	}
	horizontal, vertical, depth, homogeneous := row(0), row(1), row(2), row(3)
	planes := [6]Vec4{
		homogeneous.Add(horizontal),
		homogeneous.Sub(horizontal),
		homogeneous.Add(vertical),
		homogeneous.Sub(vertical),
		depth,
		homogeneous.Sub(depth),
	}
	var frustum Frustum
	for index, plane := range planes {
		frustum[index] = Plane{Normal: plane.Vec3(), Distance: plane.W}.Normalize()
	}
	return frustum
}

// ContainsSphere reports whether any part of the sphere is inside the frustum.
// It tests all six planes, far included, so a camera without a finite Far has
// no volume to cull against.
func (frustum Frustum) ContainsSphere(center Vec3, radius float32) bool {
	for _, plane := range frustum {
		if plane.SignedDistance(center) < -radius {
			return false
		}
	}
	return true
}

// Sphere is the bounding volume scene culls and picks with.
type Sphere struct {
	Center Vec3
	Radius float32
}

// Transform moves the sphere by an affine matrix, scaling the radius by the
// largest axis scale. That is exact under uniform scale and conservative
// otherwise, since a sphere under non-uniform scale is not a sphere.
func (sphere Sphere) Transform(matrix Mat4) Sphere {
	scale := max(
		Vec3{matrix[0], matrix[1], matrix[2]}.Length(),
		Vec3{matrix[4], matrix[5], matrix[6]}.Length(),
		Vec3{matrix[8], matrix[9], matrix[10]}.Length(),
	)
	return Sphere{Center: matrix.TransformPoint(sphere.Center), Radius: sphere.Radius * scale}
}

// Box3 is an axis-aligned bounding box, the shape a glTF POSITION accessor
// reports as min and max. The zero Box3 is a point at the origin, not an empty
// box, so Union over a set starts from the set's first member.
type Box3 struct {
	Min, Max Vec3
}

// Sphere returns the box's bounding sphere, for the per-primitive spheres scene
// culls with.
func (box Box3) Sphere() Sphere {
	return Sphere{
		Center: box.Min.Add(box.Max).MulS(0.5),
		Radius: box.Max.Sub(box.Min).MulS(0.5).Length(),
	}
}

func (box Box3) Union(other Box3) Box3 {
	return Box3{Min: box.Min.Min(other.Min), Max: box.Max.Max(other.Max)}
}

// Transform refits the box around the transformed corners. The result is
// looser than the original under rotation, so it is for growing bounds rather
// than for testing them: intersect a ray against the local box instead, by
// transforming the ray with InverseAffine.
func (box Box3) Transform(matrix Mat4) Box3 {
	center := box.Min.Add(box.Max).MulS(0.5)
	extent := box.Max.Sub(box.Min).MulS(0.5)
	transformed := Vec3{
		abs32(matrix[0])*extent.X + abs32(matrix[4])*extent.Y + abs32(matrix[8])*extent.Z,
		abs32(matrix[1])*extent.X + abs32(matrix[5])*extent.Y + abs32(matrix[9])*extent.Z,
		abs32(matrix[2])*extent.X + abs32(matrix[6])*extent.Y + abs32(matrix[10])*extent.Z,
	}
	center = matrix.TransformPoint(center)
	return Box3{Min: center.Sub(transformed), Max: center.Add(transformed)}
}

// Ray is a half-line whose Dir is unit length, which is what makes every
// intersect's t a world distance. Build one with NewRay rather than by literal.
type Ray struct {
	Origin, Dir Vec3
}

func NewRay(origin, dir Vec3) Ray { return Ray{Origin: origin, Dir: dir.Normalize()} }

func (ray Ray) At(t float32) Vec3 { return ray.Origin.Add(ray.Dir.MulS(t)) }

// Transform moves the ray by a matrix, carrying Origin as a point and Dir as a
// direction, and renormalizes Dir. A t therefore survives a rigid transform
// unchanged and does not survive a scaling one.
func (ray Ray) Transform(matrix Mat4) Ray {
	return NewRay(matrix.TransformPoint(ray.Origin), matrix.TransformDirection(ray.Dir))
}

func (matrix Mat4) TransformRay(ray Ray) Ray { return ray.Transform(matrix) }

// IntersectSphere returns the distance to the nearest hit in front of the
// origin. A ray starting inside the sphere returns zero.
func (ray Ray) IntersectSphere(sphere Sphere) (t float32, ok bool) {
	toCenter := ray.Origin.Sub(sphere.Center)
	outside := toCenter.LengthSquared() - sphere.Radius*sphere.Radius
	if outside <= 0 {
		return 0, true
	}
	along := toCenter.Dot(ray.Dir)
	discriminant := along*along - outside
	if discriminant < 0 {
		return 0, false
	}
	if t = -along - sqrt(discriminant); t < 0 {
		return 0, false
	}
	return t, true
}

// IntersectPlane returns the distance to the plane in front of the origin. A
// ray parallel to the plane misses, including one lying in it, since a hit
// everywhere is no more useful than a hit nowhere.
func (ray Ray) IntersectPlane(plane Plane) (t float32, ok bool) {
	const parallelEpsilon = 1e-6
	slope := plane.Normal.Dot(ray.Dir)
	if abs32(slope) <= parallelEpsilon {
		return 0, false
	}
	if t = -plane.SignedDistance(ray.Origin) / slope; t < 0 {
		return 0, false
	}
	return t, true
}

// IntersectBox3 returns the distance to the nearest hit in front of the origin,
// by the slab method. A ray starting inside the box returns zero. The box is
// axis-aligned in whatever space the ray is in, so a rotated model wants the
// ray transformed into local space, not the box transformed into world space.
func (ray Ray) IntersectBox3(box Box3) (t float32, ok bool) {
	const parallelEpsilon = 1e-6
	origin := [3]float32{ray.Origin.X, ray.Origin.Y, ray.Origin.Z}
	direction := [3]float32{ray.Dir.X, ray.Dir.Y, ray.Dir.Z}
	minimum := [3]float32{box.Min.X, box.Min.Y, box.Min.Z}
	maximum := [3]float32{box.Max.X, box.Max.Y, box.Max.Z}

	entry, exit := float32(0), float32(math.MaxFloat32)
	for axis := range origin {
		if abs32(direction[axis]) <= parallelEpsilon {
			if origin[axis] < minimum[axis] || origin[axis] > maximum[axis] {
				return 0, false
			}
			continue
		}
		inverse := 1 / direction[axis]
		enter, leave := (minimum[axis]-origin[axis])*inverse, (maximum[axis]-origin[axis])*inverse
		if enter > leave {
			enter, leave = leave, enter
		}
		entry, exit = max(entry, enter), min(exit, leave)
		if entry > exit {
			return 0, false
		}
	}
	return entry, true
}
