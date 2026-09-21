package types

import (
	"math"

	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/libs/m"
)

// bakedAnimation is one model's whole animation, resolved at load and never
// touched again: a pose row per joint per sampled frame, one interleaved
// record per joint, and the clip table a play addresses by name.
//
// Everything here is baked because runtime skinning is entirely vertex-shader
// work. Nothing walks a hierarchy per frame, which is the cost this design
// exists to remove, and the price is storage - a 24-joint three-clip rig is
// about 350 KiB at 60 Hz.
type bakedAnimation struct {
	// jointCount is the width of a pose row. Zero means the model has no
	// skins and no animated mesh node, which leaves poses empty and puts every
	// one of its draws on a variant that declares no pose bindings.
	jointCount int
	// poses is [rest frame][clip 0][clip 1]..., jointCount records to a row,
	// so row = clipBase + frame*jointCount + joint is one MAD in the shader
	// and the play record needs only the two rows.
	poses  []scenePose
	joints []sceneSkinJoint
	clips  []BakedClip
	// jointNames is the name of each joint's node, in joint order, for the
	// lookup facade. An unnamed node contributes an empty string rather than
	// being skipped: the slice is indexed by joint, not searched.
	jointNames []string
	// slotCount is the width of a weight row, and weights the grid itself,
	// laid out exactly as poses are: [rest frame][clip 0][clip 1]..., with
	// slotCount floats to a row.
	//
	// It never reaches the GPU. Morphing is linear in the weights, so the
	// whole blend - the two-frame lerp and the weighted mean across plays -
	// happens on the CPU at pack time and only the resulting sparse list is
	// uploaded, which is why the shader never sees a play on the morph side.
	slotCount int
	weights   []float32
	// targetNames is one entry per slot, in the same flattened depth-first
	// node order MorphWeights is positional over.
	targetNames []string
}

// BakedClip is one clip on the sampled grid.
type BakedClip struct {
	Name string
	// Duration is the clip's own length in seconds, before rounding. It is
	// what Loop takes the modulus of and what ClipInfo reports, because a
	// caller timing a one-shot needs the authored length, not the grid's.
	Duration float32
	// Frames is the sampled frame count, which covers [0, duration] whole:
	// ceil(duration * rate) + 1, and 1 for a single-keyframe or zero-duration
	// clip. base is the pose row its frame 0 sits on.
	Frames int
	Base   int
	// WeightBase is the float offset its frame 0 sits at in the weight grid.
	// It is a second base rather than a scaling of the first because the two
	// grids have different widths: joints per row against slots per row, and
	// a model may have either count at zero.
	WeightBase int
}

// row reports the pose row one of the clip's frames sits on. The frame is
// clamped rather than wrapped: wrapping is the play's business, and it has
// already decided which two frames it wants by the time it asks.
func (c BakedClip) row(frame, jointCount int) int {
	return c.Base + min(max(frame, 0), c.Frames-1)*jointCount
}

// weightRow reports the weight-grid row one of the clip's frames sits on, the
// morph twin of row and clamped for the same reason.
func (c BakedClip) weightRow(frame, slotCount int) int {
	return c.WeightBase + min(max(frame, 0), c.Frames-1)*slotCount
}

// restRow is the pose row of the implicit rest frame, which is row 0 of every
// model.
//
// Without it a draw with no plays has nothing to place its geometry: a skinned
// node's own transform is discarded per the glTF specification and a degenerate
// node's transform lives in the pose buffer, so the model would collapse to the
// origin. One extra frame per model is what makes Preload plus
// draw-with-no-plays legal and defines the zero-total-weight case.
const restRow = 0

// bakeAnimation samples every clip onto the global grid and fills the pose and
// joint buffers, from the joint numbering, node forest and unbaked clips the
// decoder handed over.
func (c *modelConverter) bakeAnimation() {
	decoded := c.decoded
	joints := len(decoded.Joints)
	animation := bakedAnimation{jointCount: joints}
	animation.joints = make([]sceneSkinJoint, joints)
	animation.jointNames = make([]string, joints)
	for joint, entry := range decoded.Joints {
		animation.joints[joint] = skinJointRecord(entry.InverseBind)
		if node := entry.Node; node >= 0 && node < len(decoded.Nodes) && decoded.Nodes[node].Present {
			animation.jointNames[joint] = decoded.Nodes[node].Name
		}
	}
	animation.slotCount = len(decoded.MorphDefaults)
	animation.targetNames = decoded.MorphNames
	tracks := c.clipTracks()
	rows := 1
	for i := range tracks {
		tracks[i].clip.Base = rows * joints
		tracks[i].clip.WeightBase = rows * animation.slotCount
		rows += tracks[i].clip.Frames
		animation.clips = append(animation.clips, tracks[i].clip)
	}
	animation.poses = make([]scenePose, rows*joints)
	if joints > 0 {
		c.walked = make([]bool, len(decoded.Nodes))
		c.bakeRestFrame(&animation)
		for i := range tracks {
			c.bakeClip(&animation, &tracks[i])
		}
	}
	c.bakeMorphWeights(&animation, rows, tracks)
	c.model.animation = animation
}

// clipTrack is one animation with its channels resolved to curves and its
// place on the grid decided.
type clipTrack struct {
	clip BakedClip
	// nodes are the animated nodes this clip steers, each with the base TRS it
	// overrides and the curves that override it. Only nodes the clip actually
	// targets are here: everything else holds still at its authored transform.
	nodes []animatedNode
	// weights are the morph channels this clip steers, each already resolved
	// to the run of model slots it writes. A slot no clip steers holds its
	// authored default, which is the morph half of the same rule the TRS half
	// follows.
	weights []animatedWeights
}

// animatedWeights is one weights channel resolved against the model's flattened
// slot list: the run of slots it writes, and the curve that writes them.
type animatedWeights struct {
	base, count int
	curve       *morphCurve
}

// animatedNode is one node one clip steers.
type animatedNode struct {
	node                            int
	translation, rotation, scale    m.Vec3
	rotationQuat                    m.Quat
	translationCurve, rotationCurve *animCurve
	scaleCurve                      *animCurve
}

// clipTracks places every decoded clip on the grid, in the file's own order.
//
// A weights-only clip is a real clip with no pose in it: it produces no joint,
// so a morph-only model loads with an empty pose buffer and still plays its
// clips by name.
func (c *modelConverter) clipTracks() []clipTrack {
	rate := float32(c.sampleRate)
	tracks := make([]clipTrack, 0, len(c.decoded.Clips))
	for i := range c.decoded.Clips {
		clip := &c.decoded.Clips[i]
		track := clipTrack{clip: BakedClip{Name: clip.Name, Duration: clip.Duration}}
		for _, steered := range clip.Nodes {
			node := c.restingNode(steered.Node)
			node.translationCurve = animCurveOf(steered.Translation)
			node.rotationCurve = animCurveOf(steered.Rotation)
			node.scaleCurve = animCurveOf(steered.Scale)
			track.nodes = append(track.nodes, node)
		}
		for _, steered := range clip.Weights {
			track.weights = append(track.weights, animatedWeights{
				base: steered.SlotBase, count: steered.SlotCount, curve: morphCurveOf(steered.Curve),
			})
		}
		// A single-keyframe clip and a zero-duration clip each bake to one
		// frame, and neither is an error: a pose that never changes is still a
		// pose, and a play on it is a legal way to hold a character still.
		track.clip.Frames = int(math.Ceil(float64(track.clip.Duration*rate))) + 1
		if track.clip.Duration <= 0 {
			track.clip.Frames = 1
		}
		tracks = append(tracks, track)
	}
	return tracks
}

// restingNode reads a node's authored transform as the base a clip's channels
// override, so a clip steering only rotation leaves the node's own translation
// and scale in place.
//
// It decomposes rather than reading the TRS fields, so a node that authored a
// matrix - which glTF forbids for an animated node, and files do anyway - gets
// the transform it asked for instead of an identity.
func (c *modelConverter) restingNode(node int) animatedNode {
	translation, rotation, scale, ok := c.decoded.Nodes[node].Local.Decompose()
	if !ok {
		translation, rotation, scale = m.Vec3{}, m.NewQuat(), m.Vec3{X: 1, Y: 1, Z: 1}
	}
	return animatedNode{
		node: node, translation: translation, rotationQuat: rotation, scale: scale,
	}
}

// local composes one animated node's local matrix at a time, each channel
// overriding its own component of the resting transform.
func (a *animatedNode) local(time float32) m.Mat4 {
	translation, rotation, scale := a.translation, a.rotationQuat, a.scale
	if a.translationCurve != nil {
		value := a.translationCurve.sample(time)
		translation = m.Vec3{X: value.X, Y: value.Y, Z: value.Z}
	}
	if a.rotationCurve != nil {
		value := a.rotationCurve.sample(time)
		rotation = m.Quat{X: value.X, Y: value.Y, Z: value.Z, W: value.W}.Normalize()
	}
	if a.scaleCurve != nil {
		value := a.scaleCurve.sample(time)
		scale = m.Vec3{X: value.X, Y: value.Y, Z: value.Z}
	}
	return m.TRS4(translation, rotation, scale)
}

// bakeRestFrame writes row 0: the authored node hierarchy resolved once, with
// no clip playing.
func (c *modelConverter) bakeRestFrame(animation *bakedAnimation) {
	c.resolveWorlds(nil, 0)
	c.storeRow(animation, restRow)
}

// bakeClip samples one clip onto the grid, frame by frame, fixing quaternion
// hemisphere continuity against the frame before it so that the shader's frame
// lerp needs no runtime sign check.
//
// The fix is within a clip and against the row just written, so it chains: a
// long rotation that crosses the hemisphere twice stays continuous the whole
// way. The rest frame is not part of any clip's chain, because a clip's frame
// 0 is where its own continuity starts.
func (c *modelConverter) bakeClip(animation *bakedAnimation, track *clipTrack) {
	joints := animation.jointCount
	rate := float32(c.sampleRate)
	for frame := range track.clip.Frames {
		c.resolveWorlds(track.nodes, float32(frame)/rate)
		row := track.clip.Base + frame*joints
		c.storeRow(animation, row)
		if frame == 0 {
			continue
		}
		previous := animation.poses[row-joints : row]
		current := animation.poses[row : row+joints]
		for joint := range current {
			if dotQuat(previous[joint].Rotation, current[joint].Rotation) < 0 {
				current[joint].Rotation = negateVec4(current[joint].Rotation)
			}
		}
	}
}

// storeRow decomposes the walk's world matrices into one pose row, reporting
// once if a joint's matrix carries something TRS cannot represent.
func (c *modelConverter) storeRow(animation *bakedAnimation, row int) {
	for joint, entry := range c.decoded.Joints {
		pose, ok := poseFromMatrix(c.worlds[entry.Node])
		if !ok && !c.poseReported {
			c.poseReported = true
			c.model.reports = append(c.model.reports,
				ErrModelPoseApproximated{Model: c.path, Joint: animation.jointNames[joint]})
		}
		animation.poses[row+joint] = pose
	}
}

// resolveWorlds walks the whole node forest once and leaves every node's world
// matrix in c.worlds, with the given clip's animated nodes overridden at time.
//
// One walk per frame over every node, rather than a chain walk per joint, so a
// shared ancestor is composed once however many bones hang off it. The forest
// is the document's, not one scene's: a joint resolves against the scene root
// and a node belongs to at most one parent, so the same walk answers every
// scene at once.
func (c *modelConverter) resolveWorlds(nodes []animatedNode, time float32) {
	forest := c.decoded.Nodes
	c.worlds = grow(c.worlds, len(forest))
	c.locals = grow(c.locals, len(forest))
	for index := range forest {
		c.locals[index] = forest[index].Local
	}
	for i := range nodes {
		c.locals[nodes[i].node] = nodes[i].local(time)
	}
	clear(c.walked)
	for _, root := range c.decoded.Roots {
		c.composeNode(root, m.NewMat4())
	}
}

// composeNode is the forest walk's one step. It guards against a cyclic node
// graph the same way the flattening walk does: malformed input is a report
// elsewhere, but an unguarded recursion here is a stack overflow.
func (c *modelConverter) composeNode(index int, parent m.Mat4) {
	if c.walked[index] {
		return
	}
	c.walked[index] = true
	world := parent.Mul(c.locals[index])
	c.worlds[index] = world
	for _, child := range c.decoded.Nodes[index].Children {
		c.composeNode(child, world)
	}
}

// animCurve is one decoded animation sampler as the bake samples it: its
// keyframe times, its values widened to four floats, and the interpolation
// between them.
type animCurve struct {
	times  []float32
	values [][4]float32
	mode   model.DecodedInterpolation
}

// animCurveOf wraps one decoded curve, or is nil for a component the clip does
// not steer. It copies no keyframe.
func animCurveOf(curve *model.DecodedCurve) *animCurve {
	if curve == nil {
		return nil
	}
	return &animCurve{times: curve.Times, values: curve.Values, mode: curve.Interpolation}
}

// sample evaluates the curve at a time, honouring the sampler's own
// interpolation.
//
// The bake is what STEP and CUBICSPLINE cost: they are evaluated here, at the
// source's own semantics, and the grid stores the result. That is also why the
// grid is 60 Hz rather than the researched 30 - a STEP channel resampled at 30
// visibly misses its edges, and per-vertex cost is unaffected by the rate.
func (c *animCurve) sample(time float32) m.Vec4 {
	last := len(c.times) - 1
	if time <= c.times[0] {
		return c.valueAt(0)
	}
	if time >= c.times[last] {
		return c.valueAt(last)
	}
	low, high := 0, last
	for low+1 < high {
		mid := (low + high) / 2
		if c.times[mid] <= time {
			low = mid
		} else {
			high = mid
		}
	}
	span := c.times[low+1] - c.times[low]
	if span <= 0 {
		return c.valueAt(low)
	}
	amount := (time - c.times[low]) / span
	switch c.mode {
	case model.DecodedInterpolationStep:
		return c.valueAt(low)
	case model.DecodedInterpolationCubicSpline:
		return c.hermite(low, amount, span)
	}
	return lerpVec4(c.valueAt(low), c.valueAt(low+1), amount)
}

// valueAt reads one keyframe's value, taking CUBICSPLINE's three-per-key
// layout into account: the value sits between its in and out tangents.
func (c *animCurve) valueAt(key int) m.Vec4 {
	index := key
	if c.mode == model.DecodedInterpolationCubicSpline {
		index = key*3 + 1
	}
	if index >= len(c.values) {
		index = len(c.values) - 1
	}
	value := c.values[index]
	return m.Vec4{X: value[0], Y: value[1], Z: value[2], W: value[3]}
}

// hermite evaluates one CUBICSPLINE segment. The tangents are stored per unit
// of the segment's own parameter, so both are scaled by the segment's length
// in seconds, which is glTF's own formulation.
func (c *animCurve) hermite(key int, amount, span float32) m.Vec4 {
	start, end := c.valueAt(key), c.valueAt(key+1)
	outgoing := c.tangent(key*3 + 2)
	incoming := c.tangent((key + 1) * 3)
	square := amount * amount
	cube := square * amount
	return addVec4(
		addVec4(
			scaleVec4(start, 2*cube-3*square+1),
			scaleVec4(outgoing, (cube-2*square+amount)*span),
		),
		addVec4(
			scaleVec4(end, -2*cube+3*square),
			scaleVec4(incoming, (cube-square)*span),
		),
	)
}

func (c *animCurve) tangent(index int) m.Vec4 {
	if index < 0 || index >= len(c.values) {
		return m.Vec4{}
	}
	value := c.values[index]
	return m.Vec4{X: value[0], Y: value[1], Z: value[2], W: value[3]}
}

func addVec4(a, b m.Vec4) m.Vec4 {
	return m.Vec4{X: a.X + b.X, Y: a.Y + b.Y, Z: a.Z + b.Z, W: a.W + b.W}
}

func scaleVec4(v m.Vec4, by float32) m.Vec4 {
	return m.Vec4{X: v.X * by, Y: v.Y * by, Z: v.Z * by, W: v.W * by}
}

func negateVec4(v m.Vec4) m.Vec4 { return scaleVec4(v, -1) }

func dotQuat(a, b m.Vec4) float32 { return a.X*b.X + a.Y*b.Y + a.Z*b.Z + a.W*b.W }

// lerpVec4 is the componentwise blend LINEAR uses. Rotations take it too and
// are normalised by the caller: glTF specifies slerp between keyframes, but
// the endpoints are sign-fixed and adjacent frames of a real rig are a few
// degrees apart, where nlerp and slerp differ by less than the grid already
// throws away.
func lerpVec4(a, b m.Vec4, amount float32) m.Vec4 {
	return addVec4(scaleVec4(a, 1-amount), scaleVec4(b, amount))
}
