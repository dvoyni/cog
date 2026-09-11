package scene

import (
	"math"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/m"
)

// residentAnimation is a model's baked animation once it is resident: the clip
// table and joint count the packer reads every frame, and the two GPU buffers
// group 2 binds.
//
// The pose and joint records themselves are gone from CPU memory by then -
// bakeBuffer takes the bytes rather than copying them - with one exception,
// poseRows, kept only where a re-root needs to follow a moving bone.
type residentAnimation struct {
	jointCount int
	sampleRate int
	clips      []bakedClip
	jointNames []string
	// poses and skinJoints are the durable buffers, and the two byte counts
	// are what PoseBytes and TotalPoseBytes report. A model with no joints
	// bakes neither, and its draws take the variant that declares no pose
	// bindings at all.
	poses          gfx.BufferDescr
	skinJoints     gfx.BufferDescr
	poseBytes      int
	skinJointBytes int
	// poseRows is the CPU copy of the pose records, kept only when some named
	// node in the file has an animated ancestor - which is almost never. It is
	// what lets a Node draw of a subtree hanging off a moving bone resolve its
	// re-root against the frame rather than against the rest pose, and keeping
	// it conditionally is what makes the empty case free.
	poseRows []scenePose
	// morphDeltas is the model's one delta buffer and morphBytes what
	// MorphBytes reports. slotCount is the width of a weight row, weights the
	// baked grid the pack-time blend samples, and targetNames the flattened
	// list MorphTargets reports - all three CPU-side, because the morph blend
	// is CPU-side and only its sparse result is uploaded.
	morphDeltas gfx.BufferDescr
	morphBytes  int
	slotCount   int
	weights     []float32
	targetNames []string
}

// skin reports the group 2 buffers a draw of this model binds.
//
// The two halves are answered independently: a rigged prop has poses and no
// shapes, and a morph-only face the other way round. What is answered here is
// also what picks the draw's shader variant, so a half the model does not have
// is a half the module does not declare and nothing is left unbound.
func (a *residentAnimation) skin() skinBuffers {
	var skin skinBuffers
	if a.jointCount > 0 {
		skin.poses, skin.joints, skin.bound = a.poses, a.skinJoints, true
	}
	if a.morphBytes > 0 {
		skin.morphs, skin.morphed = a.morphDeltas, true
	}
	return skin
}

// clip finds a clip by name, first match. Names are what a caller has - the
// glTF file's own strings - and an index would be a number that silently means
// something else the moment the artist reorders the animations.
func (a *residentAnimation) clip(name string) (*bakedClip, bool) {
	for i := range a.clips {
		if a.clips[i].name == name {
			return &a.clips[i], true
		}
	}
	return nil, false
}

// resolvePlays folds one draw's clip plays into the records the shader reads,
// appending them to dst.
//
// Everything the shader would otherwise have to work out happens here: the
// name lookup, the cap, the wrap or clamp, the frame pair, the normalisation
// and the two folded weights. What reaches the GPU is a pair of rows and a
// pair of scalars per play, and a vertex multiplies and adds.
//
// An empty result is the rest frame, and it is the answer to three different
// questions - no plays, every play naming a clip that does not exist, and
// weights summing to about zero - which is exactly why row 0 exists.
//
// The weight rows come back beside the records, parallel to them, because the
// morph blend needs the same frame pair the pose blend resolved and the play
// record has no room to carry it: that record is a GPU struct whose layout the
// shader reads.
func resolvePlays(
	anim *residentAnimation, path string, plays []ClipPlay,
	dst []scenePlayRecord, weights []weightFrames, report reportOnce,
) ([]scenePlayRecord, []weightFrames) {
	if len(anim.clips) == 0 || len(plays) == 0 {
		return dst, weights
	}
	var kept [maxClipPlays]resolvedPlay
	count := 0
	for _, play := range plays {
		clip, ok := anim.clip(play.Clip)
		if !ok {
			report(clipReportKey(path, play.Clip), ErrModelClipMissing{Model: path, Clip: play.Clip})
			continue
		}
		resolved := resolvedPlay{clip: clip, time: play.Time, loop: play.Loop, weight: play.Weight}
		if count < maxClipPlays {
			kept[count] = resolved
			count++
			continue
		}
		// The fifth play displaces the lightest of the four, so which four
		// survive does not depend on the order the caller listed them.
		lightest := 0
		for i := 1; i < count; i++ {
			if abs(kept[i].weight) < abs(kept[lightest].weight) {
				lightest = i
			}
		}
		if abs(resolved.weight) > abs(kept[lightest].weight) {
			kept[lightest] = resolved
		}
		report(playsReportKey(path), ErrModelPlaysOverLimit{
			Model: path, Plays: len(plays), Limit: maxClipPlays,
		})
	}
	var total float32
	for i := range count {
		total += kept[i].weight
	}
	// A total of about zero falls back to the rest frame rather than dividing
	// by it. There is no legitimate non-unit sum - the blend is a weighted mean
	// of TRS, so a half-weighted animation would shrink every bone toward the
	// origin rather than half-apply - so normalising is the only reading, and
	// zero is the one case it cannot express.
	if abs(total) < zeroWeightTolerance {
		return dst, weights
	}
	for i := range count {
		record, frames := kept[i].resolve(anim, kept[i].weight/total)
		dst = append(dst, record)
		weights = append(weights, frames)
	}
	return dst, weights
}

// weightFrames is the frame pair one play landed on, expressed as rows of the
// model's morph-weight grid. It is never uploaded: the two folded weights it
// pairs with live in the play record, and the blend they drive happens here.
//
// Both rows are -1 for a model with no morph slots at all, which is what makes
// the morph accumulation skip a rig with no shapes without a second test.
type weightFrames struct{ row0, row1 int }

// resolvedPlay is one play that named a real clip, before normalisation.
type resolvedPlay struct {
	clip   *bakedClip
	time   float32
	loop   bool
	weight float32
}

// resolve turns one resolved play into the two rows and two folded weights the
// shader reads, plus the same frame pair in the model's weight grid.
//
// There is no seam case. The grid covers [0, duration] whole - its last frame
// sits at or just past the clip's end - so a wrapped time never asks for a
// pair that straddles the clip's own boundary, and the pair the modulus lands
// on is always inside the clip's rows.
func (p resolvedPlay) resolve(
	anim *residentAnimation, weight float32,
) (scenePlayRecord, weightFrames) {
	jointCount, sampleRate := anim.jointCount, anim.sampleRate
	time := p.time
	switch {
	case p.clip.duration <= 0:
		time = 0
	case p.loop:
		// Modulo, not clamp, which is what makes negative time legal and a
		// reversed animation free: Go's remainder keeps the dividend's sign,
		// so a negative one is folded back up.
		time = float32(math.Mod(float64(time), float64(p.clip.duration)))
		if time < 0 {
			time += p.clip.duration
		}
	default:
		time = min(max(time, 0), p.clip.duration)
	}
	position := time * float32(sampleRate)
	frame := int(position)
	fraction := position - float32(frame)
	record := scenePlayRecord{
		BaseRow0: uint32(p.clip.row(frame, jointCount)),
		BaseRow1: uint32(p.clip.row(frame+1, jointCount)),
		W0:       weight * (1 - fraction),
		W1:       weight * fraction,
	}
	frames := weightFrames{row0: -1, row1: -1}
	if anim.slotCount > 0 {
		frames.row0 = p.clip.weightRow(frame, anim.slotCount)
		frames.row1 = p.clip.weightRow(frame+1, anim.slotCount)
	}
	return record, frames
}

// zeroWeightTolerance is the total play weight below which a draw falls back to
// the rest frame instead of normalising. It is absolute rather than relative
// because it is testing against zero, which has no scale.
const zeroWeightTolerance = 1e-6

func abs(value float32) float32 {
	if value < 0 {
		return -value
	}
	return value
}

// reportOnce is the report-once callback the pack path takes: a key and the
// error to fire under it. It is a function rather than the Lookup itself so
// that the resolution is testable without a resident model behind it.
type reportOnce func(key string, err error)

// The report-once keys the animation path fires under. They sit beside the
// load's "model:"+path key rather than inside it, the way the selectors' do: a
// file whose clips are fine and whose caller's clip name is not should still
// report a later texture failure.
func clipReportKey(path, clip string) string { return "model:" + path + "#clip:" + clip }
func playsReportKey(path string) string      { return "model:" + path + "#plays" }

// packAnim writes one draw's sceneAnim block and returns the animOffset an
// instance carries, or sceneNoAnim when the draw animates nothing.
//
// The block is indirect because sceneInstances is bound once per pass and
// shared by every draw in it: a fixed per-instance record would have to be
// sized for the worst case and put a ~320 byte tax on every debug line against
// about 48 bytes of content.
//
// A skinned draw packs one block per call and a morphed one packs a block per
// primitive, because the three morph words are per-primitive constants. Putting
// them in the per-batch material record would remove that duplication exactly,
// and was rejected: it would put scene geometry constants into a record gfx
// packs on the render thread while scene records on the update thread.
func (b *frameBuild) packAnim(plays []scenePlayRecord, morph morphBlock) uint32 {
	if len(plays) == 0 && len(morph.targets) == 0 {
		return sceneNoAnim
	}
	offset := len(b.anims.bytes()) / 16
	header := sceneAnimHeader{
		PlayCount:   uint32(len(plays)),
		TargetCount: uint32(len(morph.targets)),
		MorphBase:   morph.binding.base,
		MorphStride: morph.binding.stride,
	}
	b.anims.appendElement(&header)
	for i := range plays {
		b.anims.appendElement(&plays[i])
	}
	for i := range morph.targets {
		b.anims.appendElement(&morph.targets[i])
	}
	// Two morph entries share one vec4, so an odd count leaves the arena half
	// a vec4 short. animOffset counts vec4s, so the next block would then start
	// at an offset no instance can name.
	b.anims.padToVec4()
	return uint32(offset)
}

// morphBlock is the morph half of one draw's sceneAnim block: the primitive's
// addressing constants and the sparse list of targets that survived the cull.
type morphBlock struct {
	binding morphBinding
	targets []sceneMorphWeight
}

// blendJoint recomposes one joint's blended world matrix on the CPU, which is
// what the shader does per influence and what a re-rooted draw needs once.
//
// It exists for exactly one case: a Node draw whose subtree hangs off a bone
// some clip steers. The node's authored world transform is no longer where the
// node is, so the load-time re-root inverse is the rest pose's rather than the
// frame's. Every other draw takes the load's matrix untouched.
func blendJoint(anim *residentAnimation, plays []scenePlayRecord, joint int) (m.Mat4, bool) {
	if joint < 0 || joint >= anim.jointCount || len(anim.poseRows) == 0 {
		return m.NewMat4(), false
	}
	if len(plays) == 0 {
		return matrixFromPose(anim.poseRows[restRow+joint]), true
	}
	var rotation, translation, scale m.Vec4
	for _, play := range plays {
		for _, side := range [2]struct {
			row    uint32
			weight float32
		}{{play.BaseRow0, play.W0}, {play.BaseRow1, play.W1}} {
			row := int(side.row) + joint
			if row < 0 || row >= len(anim.poseRows) {
				continue
			}
			pose := anim.poseRows[row]
			// The cross-play accumulation still sign-fixes against the running
			// accumulator: the bake fixes continuity within a clip, but two
			// clips are two independent chains and may disagree.
			if dotQuat(rotation, pose.Rotation) < 0 {
				pose.Rotation = negateVec4(pose.Rotation)
			}
			rotation = addVec4(rotation, scaleVec4(pose.Rotation, side.weight))
			translation = addVec4(translation, scaleVec4(pose.Translation, side.weight))
			scale = addVec4(scale, scaleVec4(pose.Scale, side.weight))
		}
	}
	quaternion := m.Quat{X: rotation.X, Y: rotation.Y, Z: rotation.Z, W: rotation.W}
	if quaternion.LengthSquared() == 0 {
		return m.NewMat4(), false
	}
	return m.TRS4(
		m.Vec3{X: translation.X, Y: translation.Y, Z: translation.Z},
		quaternion.Normalize(),
		m.Vec3{X: scale.X, Y: scale.Y, Z: scale.Z},
	), true
}
