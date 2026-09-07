package scene

import (
	"fmt"
	"math"

	"github.com/dvoyni/cog/m"
	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
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
	// skins and no animated mesh node, which leaves poses empty and every one
	// of its draws on the null skin.
	jointCount int
	// poses is [rest frame][clip 0][clip 1]..., jointCount records to a row,
	// so row = clipBase + frame*jointCount + joint is one MAD in the shader
	// and the play record needs only the two rows.
	poses  []scenePose
	joints []sceneSkinJoint
	clips  []bakedClip
	// jointNames is the name of each joint's node, in joint order, for the
	// lookup facade. An unnamed node contributes an empty string rather than
	// being skipped: the slice is indexed by joint, not searched.
	jointNames []string
}

// bakedClip is one clip on the sampled grid.
type bakedClip struct {
	name string
	// duration is the clip's own length in seconds, before rounding. It is
	// what Loop takes the modulus of and what ClipInfo reports, because a
	// caller timing a one-shot needs the authored length, not the grid's.
	duration float32
	// frames is the sampled frame count, which covers [0, duration] whole:
	// ceil(duration * rate) + 1, and 1 for a single-keyframe or zero-duration
	// clip. base is the pose row its frame 0 sits on.
	frames int
	base   int
}

// row reports the pose row one of the clip's frames sits on. The frame is
// clamped rather than wrapped: wrapping is the play's business, and it has
// already decided which two frames it wants by the time it asks.
func (c bakedClip) row(frame, jointCount int) int {
	return c.base + min(max(frame, 0), c.frames-1)*jointCount
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

// jointSpace is one model's single joint numbering, shared by every skin and
// every degenerate node joint in the file.
//
// One numbering rather than one per skin is what lets the instance record
// carry a clip base alone: rows lay out per model, so the shader's address
// needs no per-skin offset and a model with three skins is still one pose
// buffer and one bind group.
type jointSpace struct {
	nodes       []int
	inverseBind []m.Mat4
	// skinned maps a node to the joint carrying its skin's inverse bind, and
	// plain to the joint carrying an identity one. They are separate because a
	// node can be both a skin joint and the degenerate joint of its own mesh,
	// and those two bindings need different inverse binds against the same
	// world transform.
	//
	// A node claimed by two skins with different inverse binds keeps the
	// first: glTF permits it, no real asset does it, and the alternative is a
	// joint index space keyed by (node, skin) that duplicates a pose row per
	// frame for every shared bone.
	skinned map[int]int
	plain   map[int]int
}

func newJointSpace() jointSpace {
	return jointSpace{skinned: map[int]int{}, plain: map[int]int{}}
}

func (s *jointSpace) count() int { return len(s.nodes) }

// claimSkinned returns the joint one skin's slot resolves to, allocating it on
// first use.
func (s *jointSpace) claimSkinned(node int, inverseBind m.Mat4) int {
	if joint, ok := s.skinned[node]; ok {
		return joint
	}
	joint := s.append(node, inverseBind)
	s.skinned[node] = joint
	return joint
}

// claimPlain returns the joint a degenerate single-joint binding on a node
// resolves to: the same world transform under an identity inverse bind.
//
// It reuses the node's skin joint when that joint's inverse bind is already
// the identity, which is the common case - a wheel that is a skin joint of
// nothing - and keeps a model from carrying two rows per frame for one bone.
func (s *jointSpace) claimPlain(node int) int {
	if joint, ok := s.plain[node]; ok {
		return joint
	}
	if joint, ok := s.skinned[node]; ok && s.inverseBind[joint] == m.NewMat4() {
		s.plain[node] = joint
		return joint
	}
	joint := s.append(node, m.NewMat4())
	s.plain[node] = joint
	return joint
}

// pose returns the joint that carries a node's world transform under whatever
// inverse bind, and whether the node has one at all. It is the read the
// re-root chain makes: it wants the bone's world matrix and no binding.
func (s *jointSpace) pose(node int) (int, bool) {
	if joint, ok := s.skinned[node]; ok {
		return joint, true
	}
	joint, ok := s.plain[node]
	return joint, ok
}

func (s *jointSpace) append(node int, inverseBind m.Mat4) int {
	s.nodes = append(s.nodes, node)
	s.inverseBind = append(s.inverseBind, inverseBind)
	return len(s.nodes) - 1
}

// buildJointSpace claims a joint for every slot of every skin, before the walk
// starts, so a primitive's JOINTS_0 remaps to the model's numbering the moment
// it is read.
//
// Only skins are claimed here. A degenerate node joint is claimed by the walk
// that finds the mesh needing it, because a node targeted by a clip but
// carrying no mesh has nothing to bind and would cost a row per frame for
// nothing - except where the re-root chain wants its world transform, which
// claims it after the walk.
func (c *modelConverter) buildJointSpace() {
	c.joints = newJointSpace()
	c.skinJoints = make([][]int, len(c.doc.Skins))
	for index, skin := range c.doc.Skins {
		if skin == nil {
			continue
		}
		inverseBind := c.inverseBindMatrices(skin)
		slots := make([]int, len(skin.Joints))
		for slot, node := range skin.Joints {
			bind := m.NewMat4()
			if slot < len(inverseBind) {
				bind = inverseBind[slot]
			}
			slots[slot] = c.joints.claimSkinned(node, bind)
		}
		c.skinJoints[index] = slots
	}
}

// inverseBindMatrices reads one skin's inverse bind accessor. A skin that
// declares none is defined by glTF to mean identity for every joint, which is
// what an empty result yields.
func (c *modelConverter) inverseBindMatrices(skin *gltf.Skin) []m.Mat4 {
	accessor, ok := accessorAt(c.doc, skin.InverseBindMatrices)
	if !ok {
		return nil
	}
	// The accessor is MAT4, which is not a vertex attribute and so has no path
	// through readAttribute's widening. glTF requires float components here -
	// the normalised integer matrix forms it permits elsewhere are excluded
	// for inverse binds - so one type assertion is the whole decode.
	data, err := modeler.ReadAccessor(c.doc, accessor, nil)
	if err == nil {
		if _, ok := data.([][4][4]float32); !ok {
			err = fmt.Errorf("inverse bind matrices are %v of %v, which glTF does not permit",
				accessor.Type, accessor.ComponentType)
		}
	}
	if err != nil {
		c.model.reports = append(c.model.reports,
			ErrModelSkinUnbound{Model: c.path, Skin: skin.Name, Err: err})
		return nil
	}
	// glTF stores a matrix column-major in the buffer, but the decoder groups
	// those floats into a [4][4]float32 indexed [row][column] - so value[0] is
	// the matrix's first row, not its first column. m.Mat4 is column-major, so
	// the copy transposes.
	//
	// This is worth spelling out because getting it wrong is invisible to a
	// test that writes its fixtures through the same package: the transpose
	// cancels, the round trip agrees with itself, and only a real file - whose
	// inverse bind is a rotation, whose transpose is its inverse - shows the
	// skeleton inside out.
	values := data.([][4][4]float32)
	matrices := make([]m.Mat4, len(values))
	for i, value := range values {
		for row := range value {
			for column := range value[row] {
				matrices[i][column*4+row] = value[row][column]
			}
		}
	}
	return matrices
}

// bindGeometryJoints rewrites one converted primitive's joint indices into the
// model's single numbering, and decides whether it is skinned at all.
//
// A skin's JOINTS_0 indexes that skin's own joints array, which is local to
// the skin; the model's numbering is what makes rows addressable by
// clipBase + frame*jointCount + joint with no per-skin offset anywhere. A
// degenerate binding has no JOINTS_0 to remap and gets one written outright.
//
// Weights are normalised here rather than in the shader. glTF requires them to
// sum to one and files drift; normalising per vertex at load costs one pass
// over data already in cache and removes a divide from the per-vertex path.
func (c *modelConverter) bindGeometryJoints(geometry *gltfGeometry, binding skinBinding) {
	switch {
	case binding.joint >= 0:
		joint := uint16(binding.joint)
		for i := range geometry.vertices {
			geometry.vertices[i].Joints = [4]uint16{joint}
			geometry.vertices[i].Weights = m.Vec4{X: 1}
		}
		geometry.skinned = true
	case binding.skin >= 0 && binding.skin < len(c.skinJoints):
		slots := c.skinJoints[binding.skin]
		bound := false
		for i := range geometry.vertices {
			vertex := &geometry.vertices[i]
			total := vertex.Weights.X + vertex.Weights.Y + vertex.Weights.Z + vertex.Weights.W
			if total <= 0 {
				vertex.Joints, vertex.Weights = [4]uint16{}, m.Vec4{}
				continue
			}
			bound = true
			vertex.Weights = m.Vec4{
				X: vertex.Weights.X / total, Y: vertex.Weights.Y / total,
				Z: vertex.Weights.Z / total, W: vertex.Weights.W / total,
			}
			for influence, slot := range vertex.Joints {
				// A slot past the skin's joints array is a malformed file. It
				// resolves to joint 0, whose weight the file has already
				// decided; the alternative is dropping a whole primitive over
				// one bad index.
				if int(slot) < len(slots) {
					vertex.Joints[influence] = uint16(slots[slot])
					continue
				}
				vertex.Joints[influence] = 0
			}
		}
		geometry.skinned = bound
	}
}

// bakeAnimation samples every clip onto the global grid and fills the pose and
// joint buffers. It runs after every scene is flattened, because the walk is
// what claims the degenerate joints.
func (c *modelConverter) bakeAnimation() {
	c.claimRerootJoints()
	joints := c.joints.count()
	animation := bakedAnimation{jointCount: joints}
	animation.joints = make([]sceneSkinJoint, joints)
	animation.jointNames = make([]string, joints)
	for joint, node := range c.joints.nodes {
		animation.joints[joint] = skinJointRecord(c.joints.inverseBind[joint])
		if node >= 0 && node < len(c.doc.Nodes) && c.doc.Nodes[node] != nil {
			animation.jointNames[joint] = c.doc.Nodes[node].Name
		}
	}
	tracks := c.clipTracks()
	rows := 1
	for i := range tracks {
		tracks[i].clip.base = rows * joints
		rows += tracks[i].clip.frames
		animation.clips = append(animation.clips, tracks[i].clip)
	}
	animation.poses = make([]scenePose, rows*joints)
	if joints > 0 {
		c.bakeRestFrame(&animation)
		for i := range tracks {
			c.bakeClip(&animation, &tracks[i])
		}
	}
	c.model.animation = animation
}

// claimRerootJoints gives a joint to the deepest animated ancestor of every
// named node that has one, so a Node draw of a subtree hanging under a moving
// bone can resolve its re-root inverse against the frame's poses rather than
// against the rest pose.
//
// Almost every node in almost every file has an empty chain, and this loop
// claims nothing for those - which is the whole of "the empty case stays free".
func (c *modelConverter) claimRerootJoints() {
	for i := range c.model.scenes {
		for name, node := range c.model.scenes[i].nodes {
			if len(node.animated) == 0 {
				continue
			}
			ancestor := node.animated[len(node.animated)-1]
			// Any joint on that node will do. Pose records hold globalJoint
			// alone, unpremultiplied, so every joint following one node holds
			// the same world transform whatever inverse bind it carries - and
			// claiming a second one would duplicate a pose row per frame for
			// every named bone of a rig, which on a real fox is most of them.
			joint, ok := c.joints.pose(ancestor)
			if !ok {
				joint = c.joints.claimPlain(ancestor)
			}
			node.rerootJoint = joint
			c.model.scenes[i].nodes[name] = node
		}
	}
}

// clipTrack is one animation with its channels resolved to curves and its
// place on the grid decided.
type clipTrack struct {
	clip bakedClip
	// nodes are the animated nodes this clip steers, each with the base TRS it
	// overrides and the curves that override it. Only nodes the clip actually
	// targets are here: everything else holds still at its authored transform.
	nodes []animatedNode
}

// animatedNode is one node one clip steers.
type animatedNode struct {
	node                            int
	translation, rotation, scale    m.Vec3
	rotationQuat                    m.Quat
	translationCurve, rotationCurve *animCurve
	scaleCurve                      *animCurve
}

// clipTracks resolves every animation in the document into a track, in the
// document's own order, skipping any that steers no node's TRS.
//
// A weights-only animation is skipped here rather than dropped: it carries no
// pose and produces no joint, which is why a morph-only model loads with an
// empty pose buffer. It still becomes a clip once morph targets land.
func (c *modelConverter) clipTracks() []clipTrack {
	rate := float32(c.sampleRate)
	tracks := make([]clipTrack, 0, len(c.doc.Animations))
	for _, animation := range c.doc.Animations {
		if animation == nil {
			continue
		}
		track := clipTrack{clip: bakedClip{name: animation.Name}}
		byNode := map[int]int{}
		for _, channel := range animation.Channels {
			if channel == nil || channel.Target.Node == nil {
				continue
			}
			node := *channel.Target.Node
			if node < 0 || node >= len(c.doc.Nodes) || c.doc.Nodes[node] == nil {
				continue
			}
			curve := c.animCurve(animation, channel.Sampler)
			if curve == nil {
				continue
			}
			slot, ok := byNode[node]
			if !ok {
				slot = len(track.nodes)
				byNode[node] = slot
				track.nodes = append(track.nodes, c.restingNode(node))
			}
			switch channel.Target.Path {
			case gltf.TRSTranslation:
				track.nodes[slot].translationCurve = curve
			case gltf.TRSRotation:
				track.nodes[slot].rotationCurve = curve
			case gltf.TRSScale:
				track.nodes[slot].scaleCurve = curve
			default:
				continue
			}
			track.clip.duration = max(track.clip.duration, curve.end())
		}
		if len(track.nodes) == 0 {
			continue
		}
		// A single-keyframe clip and a zero-duration clip each bake to one
		// frame, and neither is an error: a pose that never changes is still a
		// pose, and a play on it is a legal way to hold a character still.
		track.clip.frames = int(math.Ceil(float64(track.clip.duration*rate))) + 1
		if track.clip.duration <= 0 {
			track.clip.frames = 1
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
	translation, rotation, scale, ok := nodeMatrix(c.doc.Nodes[node]).Decompose()
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
	for frame := range track.clip.frames {
		c.resolveWorlds(track.nodes, float32(frame)/rate)
		row := track.clip.base + frame*joints
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
	for joint, node := range c.joints.nodes {
		pose, ok := poseFromMatrix(c.worlds[node])
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
	c.worlds = grow(c.worlds, len(c.doc.Nodes))
	c.locals = grow(c.locals, len(c.doc.Nodes))
	for index, node := range c.doc.Nodes {
		if node == nil {
			c.locals[index] = m.NewMat4()
			continue
		}
		c.locals[index] = nodeMatrix(node)
	}
	for i := range nodes {
		c.locals[nodes[i].node] = nodes[i].local(time)
	}
	clear(c.walked)
	for _, root := range c.nodeRoots {
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
	for _, child := range c.doc.Nodes[index].Children {
		if child >= 0 && child < len(c.doc.Nodes) && c.doc.Nodes[child] != nil {
			c.composeNode(child, world)
		}
	}
}

// buildNodeForest finds the nodes nothing parents, which are the roots the
// pose walk starts from. It is the document's forest rather than a scene's
// root list because a joint may sit outside every scene's node list and still
// be referenced by a skin, and its world transform is defined regardless.
func (c *modelConverter) buildNodeForest() {
	parented := make([]bool, len(c.doc.Nodes))
	for _, node := range c.doc.Nodes {
		if node == nil {
			continue
		}
		for _, child := range node.Children {
			if child >= 0 && child < len(parented) {
				parented[child] = true
			}
		}
	}
	for index, node := range c.doc.Nodes {
		if node != nil && !parented[index] {
			c.nodeRoots = append(c.nodeRoots, index)
		}
	}
	c.walked = make([]bool, len(c.doc.Nodes))
}

// animCurve is one animation sampler decoded once: its keyframe times, its
// values widened to four floats, and the interpolation between them.
type animCurve struct {
	times  []float32
	values []attrValue
	mode   gltf.Interpolation
}

// end reports the curve's last keyframe time, which is what a clip's duration
// is the maximum of.
func (c *animCurve) end() float32 {
	if len(c.times) == 0 {
		return 0
	}
	return c.times[len(c.times)-1]
}

// animCurve decodes one sampler, or returns nil for one that cannot be read.
// Samplers are interned per document, because a clip that steers twenty bones
// with one shared input accessor should decode it once.
func (c *modelConverter) animCurve(animation *gltf.Animation, index int) *animCurve {
	if index < 0 || index >= len(animation.Samplers) || animation.Samplers[index] == nil {
		return nil
	}
	key := samplerKey{animation: animation, sampler: index}
	if curve, ok := c.curves[key]; ok {
		return curve
	}
	c.curves[key] = nil
	sampler := animation.Samplers[index]
	input, ok := accessorAt(c.doc, &sampler.Input)
	if !ok {
		return nil
	}
	output, ok := accessorAt(c.doc, &sampler.Output)
	if !ok {
		return nil
	}
	curve := &animCurve{mode: sampler.Interpolation}
	if err := readAttribute(c.doc, input, func(_ int, value attrValue) {
		curve.times = append(curve.times, value[0])
	}); err != nil {
		return nil
	}
	if err := readAttribute(c.doc, output, func(_ int, value attrValue) {
		curve.values = append(curve.values, value)
	}); err != nil {
		return nil
	}
	if len(curve.times) == 0 || len(curve.values) == 0 {
		return nil
	}
	c.curves[key] = curve
	return curve
}

// samplerKey interns one animation's samplers. The animation is part of the
// key because sampler indices are local to it.
type samplerKey struct {
	animation *gltf.Animation
	sampler   int
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
	case gltf.InterpolationStep:
		return c.valueAt(low)
	case gltf.InterpolationCubicSpline:
		return c.hermite(low, amount, span)
	}
	return lerpVec4(c.valueAt(low), c.valueAt(low+1), amount)
}

// valueAt reads one keyframe's value, taking CUBICSPLINE's three-per-key
// layout into account: the value sits between its in and out tangents.
func (c *animCurve) valueAt(key int) m.Vec4 {
	index := key
	if c.mode == gltf.InterpolationCubicSpline {
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
