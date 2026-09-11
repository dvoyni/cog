package scene

import (
	"unsafe"

	"github.com/dvoyni/cog/m"
)

// ClipPlay is one animation clip playing on one model draw.
//
// Animation is stateless: nothing in scene advances Time, and no play survives
// the frame that recorded it. Gameplay - or the anim plugin - owns the clock
// and hands the result to the draw, which is what makes scrubbing, reversing
// and pausing the caller's business rather than an API scene has to grow.
//
// Clips are addressed by name, first match. An unknown name is reported once
// per model and the play dropped, so a typo costs the one play rather than the
// whole character.
type ClipPlay struct {
	Clip string
	// Time is the play head in seconds, already advanced by the caller. Loop
	// decides what happens outside [0, duration]: false clamps, true takes the
	// modulus, which makes negative time legal and a reversed animation free.
	//
	// Loop is on the play rather than on the caller's time because the frame
	// pair at the seam - the clip's last frame blended back into its first -
	// can only be built by whoever knows the clip wraps, and that is not
	// derivable from a raw time value.
	Time float32
	Loop bool
	// Weight is this play's share of the blend. Weights are normalised across
	// a draw's plays before anything is packed: the blend is a weighted mean
	// of TRS, not an additive layer, so weights summing to 0.5 would not
	// half-apply the animation - they would shrink every bone's translation
	// toward the origin and mangle the character. A total of about zero falls
	// back to the rest frame.
	Weight float32
}

// ClipInfo is one clip a model file declares, as Clips reports it.
//
// Duration is here because a caller needs it to know when a one-shot play has
// ended, which is a question only the clip's own length answers and the one
// piece of clip state gameplay cannot compute for itself.
type ClipInfo struct {
	Name     string
	Duration float32
}

// maxClipPlays is how many clips one draw may blend. A fifth play is dropped
// by lowest weight and reported once per model rather than failing the draw:
// the four heaviest are what the character mostly looks like anyway.
const maxClipPlays = 4

// scenePose is the 48-byte baked pose record - three aligned vec4 loads -
// holding globalJoint alone, unpremultiplied by the inverse bind.
//
// Premultiplying was the original recommendation and is overturned. Inverse
// bind matrices routinely carry the bind pose's non-uniform scale, and
// premultiplying injects it into a record that is then decomposed to TRS,
// which cannot represent shear at all; degenerate single-joint skins have no
// inverse bind to premultiply; and unpremultiplied, the buffer literally holds
// bone world transforms, which is what bone sockets need.
//
// Scale is a vec3 rather than a scalar packed into Translation.W. That would
// cut pose memory and per-vertex loads by a third - the single largest cost in
// this design - but squash-and-stretch is animated non-uniform scale, a
// mainstream idiom, and unlike Transform there is no Matrix escape hatch to
// correct it at.
//
// Field order and size must match ScenePose in builtin/scene/scene.wgsl.
type scenePose struct {
	// Rotation is a unit quaternion as xyzw. Translation and Scale use xyz and
	// leave w spare, which is the price of the three-aligned-loads layout.
	Rotation    m.Vec4
	Translation m.Vec4
	Scale       m.Vec4
}

// sceneSkinJoint is the 112-byte per-skin, per-joint record: the inverse bind
// and its normal matrix interleaved.
//
// They are both per skin, both indexed by the same joint index, and both
// fetched on every influence, so one buffer means one address computation
// instead of two and lands both halves adjacent for all four influences. It
// also recovers a storage-buffer slot, which is what takes group 2 to three
// bindings rather than four.
//
// The columns are explicit vec4s rather than mat4x3 and mat3x3 because WGSL
// pads every matrix column to 16 bytes anyway: the record already carries 28
// bytes matrix syntax cannot address, and Normal0.W spends four of them on the
// precomputed tangent handedness.
//
// Field order and size must match SceneSkinJoint in builtin/scene/scene.wgsl.
type sceneSkinJoint struct {
	// InverseBind0..3 are the columns of the 4x4 inverse bind, in the same
	// column-major order m.Mat4 stores.
	InverseBind0, InverseBind1, InverseBind2, InverseBind3 m.Vec4
	// Normal0..2 are the columns of transpose(inverse(inverseBind)) as a 3x3,
	// each padded to a vec4. The shader takes the normal through this matrix
	// rather than inverting anything, because an inverse bind can be
	// non-orthonormal and so can the composed skinning matrix.
	//
	// Normal0.W is the tangent handedness sign: the sign of the inverse bind's
	// determinant, which a mirrored bind pose flips. Normal1.W and Normal2.W
	// are the record's remaining spare words.
	Normal0, Normal1, Normal2 m.Vec4
}

// scenePlayRecord is one play as the shader reads it: the two pose rows the
// frame pair sits on, with the weights already folded.
//
// The CPU folds weight * (1 - frac) and weight * frac into W0 and W1, so the
// shader does no clip-length, wrap or normalisation arithmetic - it multiplies
// and adds. BaseRow0 and BaseRow1 are rows, not frames: the shader adds the
// joint index and is done, which is what makes the address one MAD.
//
// Field order and size must match ScenePlay in builtin/scene/scene.wgsl.
type scenePlayRecord struct {
	BaseRow0, BaseRow1 uint32
	W0, W1             float32
}

// sceneAnimHeader is the sceneAnim block's two-vec4 header. Every field is a
// count or an offset the shader reads before it loops, and the morph words
// are the primitive constants its delta address is built from.
//
// A draw is skinned only, morphed only, both, or neither, and PlayCount and
// TargetCount are each independently zero-checkable - which is why there is no
// flags bitfield here: two counts the shader reads anyway already carry it.
//
// Field order and size must match the header SceneAnim reads in
// builtin/scene/scene.wgsl, where the block is a raw vec4 arena.
type sceneAnimHeader struct {
	PlayCount   uint32
	TargetCount uint32
	// MorphBase is the word index of the primitive's block in the model's
	// delta buffer and MorphStride the words one record spends there, which is
	// also what says which slots that record holds.
	MorphBase   uint32
	MorphStride uint32
	// The second vec4 is wholly reserved. It carried MorphTargetStride -
	// vertexCount * MorphStride, the folded constant the old dense address
	// formula multiplied by - and a target now stores records only for the span
	// it moves, so each one carries its own base in the block header and no
	// per-primitive stride exists to fold. The header stays a whole number of
	// vec4s because the block is an array<vec4<u32>> and animOffset counts
	// vec4s.
	reserved [4]uint32
}

// The sizes of the animation records: bytes for the two durable buffers, and
// the vec4 units animOffset counts for the two the per-frame arena holds.
var (
	poseSize        = int(unsafe.Sizeof(scenePose{}))
	skinJointSize   = int(unsafe.Sizeof(sceneSkinJoint{}))
	animHeaderVec4s = int(unsafe.Sizeof(sceneAnimHeader{})) / 16
	playRecordVec4s = int(unsafe.Sizeof(scenePlayRecord{})) / 16
)

// skinJointRecord builds one joint's interleaved record from its inverse bind,
// precomputing the normal matrix and the handedness sign the shader would
// otherwise have to derive per vertex per influence.
//
// A bind matrix too flat to invert keeps the inverse bind it was given and
// takes the identity as its normal matrix. That is the wrong matrix, but it is
// a finite one: the cofactors over a zero determinant would hand every
// downstream normalize a NaN, and a collapsed bind pose is a broken asset
// rather than a case to be correct about.
func skinJointRecord(inverseBind m.Mat4) sceneSkinJoint {
	normal := m.NewMat3()
	if inverse, ok := inverseBind.Mat3().Inverse(); ok {
		normal = inverse.Transpose()
	}
	sign := float32(1)
	if inverseBind.Mat3().Determinant() < 0 {
		sign = -1
	}
	return sceneSkinJoint{
		InverseBind0: m.Vec4{X: inverseBind[0], Y: inverseBind[1], Z: inverseBind[2], W: inverseBind[3]},
		InverseBind1: m.Vec4{X: inverseBind[4], Y: inverseBind[5], Z: inverseBind[6], W: inverseBind[7]},
		InverseBind2: m.Vec4{X: inverseBind[8], Y: inverseBind[9], Z: inverseBind[10], W: inverseBind[11]},
		InverseBind3: m.Vec4{X: inverseBind[12], Y: inverseBind[13], Z: inverseBind[14], W: inverseBind[15]},
		Normal0:      m.Vec4{X: normal[0], Y: normal[1], Z: normal[2], W: sign},
		Normal1:      m.Vec4{X: normal[3], Y: normal[4], Z: normal[5]},
		Normal2:      m.Vec4{X: normal[6], Y: normal[7], Z: normal[8]},
	}
}

// poseFromMatrix decomposes one joint's world matrix into its pose record, and
// reports whether the decomposition is faithful.
//
// Unrepresentable data is best-effort plus one report: a matrix carrying shear
// recomposes to something slightly different, and a slightly wrong elbow beats
// a missing character. Faithfulness is tested by recomposing rather than by
// asking what kind of matrix this is, so the answer covers whatever the file
// actually did.
func poseFromMatrix(matrix m.Mat4) (scenePose, bool) {
	translation, rotation, scale, ok := matrix.Decompose()
	if !ok {
		translation, rotation, scale = decomposeCollapsed(matrix)
	}
	pose := scenePose{
		Rotation:    m.Vec4{X: rotation.X, Y: rotation.Y, Z: rotation.Z, W: rotation.W},
		Translation: m.Vec4{X: translation.X, Y: translation.Y, Z: translation.Z},
		Scale:       m.Vec4{X: scale.X, Y: scale.Y, Z: scale.Z},
	}
	return pose, faithful(matrix, m.TRS4(translation, rotation, scale))
}

// decomposeCollapsed decomposes a matrix with at least one zero-length axis,
// which Mat4.Decompose declines because a collapsed basis carries no rotation
// to recover.
//
// A TRS record holds such a matrix exactly - the scale is simply zero on that
// axis - so declining here would turn an object animated down to nothing into
// an object that pops back to full size. Scale keyframes reaching zero are
// ordinary: the Khronos InterpolationTest does it on three of its nine cubes.
//
// The rotation the collapsed axes lost is rebuilt from the ones that survived,
// which is arbitrary and unobservable: a zero-scaled axis has no direction to
// get wrong.
func decomposeCollapsed(matrix m.Mat4) (m.Vec3, m.Quat, m.Vec3) {
	axes := [3]m.Vec3{
		{X: matrix[0], Y: matrix[1], Z: matrix[2]},
		{X: matrix[4], Y: matrix[5], Z: matrix[6]},
		{X: matrix[8], Y: matrix[9], Z: matrix[10]},
	}
	var scale m.Vec3
	var unit [3]m.Vec3
	live := 0
	for i, axis := range axes {
		length := axis.Length()
		switch i {
		case 0:
			scale.X = length
		case 1:
			scale.Y = length
		case 2:
			scale.Z = length
		}
		if length > 0 {
			unit[i] = axis.DivS(length)
			live++
		}
	}
	switch live {
	case 0:
		unit = [3]m.Vec3{{X: 1}, {Y: 1}, {Z: 1}}
	case 1:
		// One direction survives. Complete it with any perpendicular and the
		// cross product of the two, which is a right-handed basis containing
		// the axis the file actually meant.
		for i, axis := range unit {
			if axis != (m.Vec3{}) {
				unit[(i+1)%3] = orthogonal(axis)
				unit[(i+2)%3] = axis.Cross(unit[(i+1)%3])
				break
			}
		}
	case 2:
		for i, axis := range unit {
			if axis == (m.Vec3{}) {
				unit[i] = unit[(i+1)%3].Cross(unit[(i+2)%3])
				break
			}
		}
	}
	basis := m.Mat3{
		unit[0].X, unit[0].Y, unit[0].Z,
		unit[1].X, unit[1].Y, unit[1].Z,
		unit[2].X, unit[2].Y, unit[2].Z,
	}
	return matrix.Translation(), m.QuatFromMat3(basis), scale
}

// faithful reports whether a recomposition landed back on the matrix it came
// from, relative to that matrix's own magnitude so the test holds for a
// millimetre-scale prop and a kilometre-scale rig alike.
func faithful(original, recomposed m.Mat4) bool {
	var residual, magnitude float32
	for i := range original {
		difference := original[i] - recomposed[i]
		residual += difference * difference
		magnitude += original[i] * original[i]
	}
	return residual <= poseResidualTolerance*poseResidualTolerance*max(magnitude, 1)
}

// poseResidualTolerance is the relative error a decomposition may carry before
// the load says so. It is loose enough that float32 rounding through a
// quaternion never trips it and tight enough that real shear does.
const poseResidualTolerance = 1e-3

// matrixFromPose recomposes one pose record. It is the CPU's side of what the
// shader does per influence, and exists for the one thing the shader cannot
// do: resolving a re-rooted node's animated ancestor at pack time.
func matrixFromPose(pose scenePose) m.Mat4 {
	return m.TRS4(
		m.Vec3{X: pose.Translation.X, Y: pose.Translation.Y, Z: pose.Translation.Z},
		m.Quat{X: pose.Rotation.X, Y: pose.Rotation.Y, Z: pose.Rotation.Z, W: pose.Rotation.W},
		m.Vec3{X: pose.Scale.X, Y: pose.Scale.Y, Z: pose.Scale.Z},
	)
}
