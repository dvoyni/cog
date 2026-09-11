package scene

import (
	"github.com/dvoyni/cog/m"
)

// The per-mesh UV range, which is how both texture coordinate sets fit in four
// bytes each.
//
// A half float would have saved the same four bytes with no metadata at all, so
// the range buys accuracy rather than size - and the gap is not marginal. A UV
// island reaching the atlas edge touches 1.0 exactly, crosses into the [1, 2)
// binade and doubles its half-float step for the whole primitive; eight
// primitives of the vendored corpus do that and it is ordinary authoring. At a
// 1024-texel texture the half float costs half a texel and is invisible; at
// 4096 it is 2 to 4 texels on ordinary content and 64 on the worst tiled
// outlier, and the engine has no say over what resolution an app ships.
//
// The range is derived and never surfaced. A mesh's UV precision depends on the
// spread of the UVs in that same bake, which is a property of the mesh rather
// than of the API: letting an author supply one would add a parameter to
// BakeMesh, UpdateMesh and TemporaryMesh alike, and every path here re-derives
// it whenever the vertices are replaced.
//
// The decode is the GPU's, published as sceneDecodeUV in
// builtin/scene/vertexdecode.wgsl; what is here is the half that runs at bake.

// uvCodeMax is the largest code of one UV component's 16-bit unorm, and the
// divisor the fetch unit has already applied by the time the decode sees it. The
// encode scales by exactly it and not by a power of two beside it.
const uvCodeMax = 0xFFFF

// sceneMesh is the 32-byte per-mesh record: the scale and the bias that take
// each UV set's two unorm codes back to the coordinates that were authored,
// stored in the buffer bound at @group(0) @binding(3) and indexed by the
// instance's mesh word.
//
// Field order and size must match SceneMesh in builtin/scene/instance.wgsl.
//
// Its zero value is the record of a mesh that has no range of its own - a
// custom layout, which scene never packed and cannot find a UV inside, or a
// standard mesh whose every UV is zero - and such a mesh names slot 0, the
// reserved identity record, rather than carrying a record of its own. The two
// agree: a mesh with no spread stores code 0 in every component, and slot 0
// decodes that to 0.
type sceneMesh struct {
	UV0Scale m.Vec2
	UV0Bias  m.Vec2
	UV1Scale m.Vec2
	UV1Bias  m.Vec2
}

// identityMesh is slot 0 of the per-mesh buffer: scale 1, bias 0, so the
// dequantisation of a mesh that names it is a branchless no-op.
//
// It was chosen over a validity flag in the instance's flags word. A
// draw-uniform branch is cheap, but an identity record is free - it needs no
// flag bit, no branch and no second path to test - and one slot of 32 bytes per
// frame is the whole of its cost.
var identityMesh = sceneMesh{
	UV0Scale: m.Vec2{X: 1, Y: 1},
	UV1Scale: m.Vec2{X: 1, Y: 1},
}

// packRecord is the record a pack quantises against: the derived one, or the
// identity where the derived one is empty. An empty record is exactly what
// names slot 0 at draw time, so this is the encode reading the same numbers the
// decode will, rather than two sets of numbers that happen to agree.
func (r sceneMesh) packRecord() sceneMesh {
	if r == (sceneMesh{}) {
		return identityMesh
	}
	return r
}

// uvRange accumulates one UV set's axis-aligned range, per component, as the
// coordinates go past.
//
// seen is what separates "every UV is the origin" from "no UV was offered at
// all" while they are being collected. Both end at the same record - a
// zero-width range at the origin - but a set that starts at the Go zero value
// would swallow the first real coordinate into a range reaching back to 0,
// which for a UV atlas sitting in [0.4, 0.6] would cost three bits of the four
// bytes this whole mechanism is for.
type uvRange struct {
	min, max m.Vec2
	seen     bool
}

// add widens the range to hold one coordinate.
func (r *uvRange) add(uv m.Vec2) {
	if !r.seen {
		r.min, r.max, r.seen = uv, uv, true
		return
	}
	r.min = m.Vec2{X: min(r.min.X, uv.X), Y: min(r.min.Y, uv.Y)}
	r.max = m.Vec2{X: max(r.max.X, uv.X), Y: max(r.max.Y, uv.Y)}
}

// scaleBias reports the two vectors the decode multiplies and adds.
//
// A zero-width range reports scale 0 and bias equal to the constant, which
// decodes exactly right with the same arithmetic and needs no special case at
// draw time. It is not hypothetical: two CesiumMilkTruck primitives have their
// first UV set collapsed to a single point, and scene's own unit meshes leave
// the second set unwritten.
func (r uvRange) scaleBias() (m.Vec2, m.Vec2) {
	return m.Vec2{X: r.max.X - r.min.X, Y: r.max.Y - r.min.Y}, r.min
}

// meshRecordFor builds one mesh's record from the two ranges its bake
// accumulated.
func meshRecordFor(uv0, uv1 uvRange) sceneMesh {
	var record sceneMesh
	record.UV0Scale, record.UV0Bias = uv0.scaleBias()
	record.UV1Scale, record.UV1Bias = uv1.scaleBias()
	return record
}

// quantizeUV maps one UV component onto its 16-bit unorm code against the
// mesh's range. The clamps are for the ends only: a coordinate inside the range
// it was derived from is inside [0, 1] by construction, and float arithmetic is
// what can put it a hair outside.
//
// A zero-width range codes 0 and lets the bias carry the constant. The guard is
// written as !(scale > 0) so that a NaN scale - which nothing can produce from
// finite UVs, but which a NaN coordinate would - lands on 0 rather than on a
// division.
//
// The subtraction and the divide are float64 because the range's own width is
// what they are relative to: a tiled set reaching 18.52 has a bias of the same
// magnitude, and doing (value - bias) in float32 there would throw away the low
// bits of exactly the difference this is trying to resolve.
func quantizeUV(value, scale, bias float32) uint16 {
	if !(scale > 0) {
		return 0
	}
	scaled := (float64(value) - float64(bias)) / float64(scale) * uvCodeMax
	if !(scaled > 0) {
		return 0
	}
	if scaled >= uvCodeMax {
		return uvCodeMax
	}
	return uint16(scaled + 0.5)
}
