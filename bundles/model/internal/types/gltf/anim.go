package gltf

import (
	"fmt"

	"github.com/dvoyni/cog/libs/m"
	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
)

// Joint is one joint of the model's single joint numbering: the node whose
// world transform it follows, and the inverse bind that transform is taken
// against.
//
// One numbering per model rather than one per skin is what lets a pose row be
// addressed with no per-skin offset, so every skin's joints and every
// degenerate node joint share it. The skins' joints are claimed first, in one
// contiguous block at the bottom of the numbering, so the joints a vertex can
// name are always the lowest indices.
type Joint struct {
	Node        int
	InverseBind m.Mat4
}

// Skin is one glTF skin: its name and each of its joints array's slots
// resolved into the model's numbering. A slot of JOINTS_0 is an index into
// Joints. A skin the file left null has no joints.
type Skin struct {
	Name   string
	Joints []int
}

// Node is one entry of the file's node array: its name, its authored local
// transform and its children. A null node is absent, with an identity local
// and no children; Children holds only indices that name a node.
type Node struct {
	Name     string
	Local    m.Mat4
	Children []int
	Present  bool
}

// Interpolation is how a sampler interpolates between its keyframes.
type Interpolation uint8

const (
	InterpolationLinear Interpolation = iota
	InterpolationStep
	InterpolationCubicSpline
)

// interpolationOf maps glTF's interpolation onto the decoder's.
func interpolationOf(mode gltf.Interpolation) Interpolation {
	switch mode {
	case gltf.InterpolationStep:
		return InterpolationStep
	case gltf.InterpolationCubicSpline:
		return InterpolationCubicSpline
	}
	return InterpolationLinear
}

// Curve is one TRS sampler decoded once: its keyframe times, its values widened
// to four floats, and the interpolation between them. A CUBICSPLINE curve
// stores three values a keyframe, in-tangent, value and out-tangent.
type Curve struct {
	Times         []float32
	Values        [][4]float32
	Interpolation Interpolation
}

// End reports the curve's last keyframe time, which is what a clip's duration
// is the maximum of.
func (c *Curve) End() float32 {
	if len(c.Times) == 0 {
		return 0
	}
	return c.Times[len(c.Times)-1]
}

// Clip is one animation with its channels resolved, unbaked. Only nodes and
// weight runs the clip actually steers are here: everything else holds still
// at its authored transform and its default weights.
type Clip struct {
	Name string
	// Duration is the clip's own length in seconds: the latest keyframe of any
	// curve it steers.
	Duration float32
	Nodes    []ClipNode
	Weights  []ClipWeights
}

// ClipNode is one node one clip steers, and the curve that overrides each of
// its three components, nil for a component the clip leaves at the node's
// authored value.
type ClipNode struct {
	Node                         int
	Translation, Rotation, Scale *Curve
}

// ClipWeights is one weights channel resolved against the model's flattened
// weight slots: the run of slots it writes and the curve that writes them.
type ClipWeights struct {
	SlotBase, SlotCount int
	Curve               *WeightCurve
}

// jointSpace is the model's one joint numbering as the walk builds it.
type jointSpace struct {
	joints []Joint
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
	if joint, ok := s.skinned[node]; ok && s.joints[joint].InverseBind == m.NewMat4() {
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
	s.joints = append(s.joints, Joint{Node: node, InverseBind: inverseBind})
	return len(s.joints) - 1
}

// buildJointSpace claims a joint for every slot of every skin, before the walk
// starts, so the joints a vertex can name are claimed in one contiguous block
// at the bottom of the numbering, before a plain joint or a re-root joint can
// take an index.
//
// Only skins are claimed here. A degenerate node joint is claimed by the walk
// that finds the mesh needing it, because a node targeted by a clip but
// carrying no mesh has nothing to bind and would cost a row per frame for
// nothing - except where the re-root chain wants its world transform, which
// claims it after the walk.
func (c *decoder) buildJointSpace() {
	c.joints = newJointSpace()
	c.model.Skins = make([]Skin, len(c.doc.Skins))
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
		c.model.Skins[index] = Skin{Name: skin.Name, Joints: slots}
	}
}

// inverseBindMatrices reads one skin's inverse bind accessor. A skin that
// declares none is defined by glTF to mean identity for every joint, which is
// what an empty result yields.
func (c *decoder) inverseBindMatrices(skin *gltf.Skin) []m.Mat4 {
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
		c.model.Reports = append(c.model.Reports,
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

// claimRerootJoints gives a joint to the deepest animated ancestor of every
// named node that has one, so a Node draw of a subtree hanging under a moving
// bone can resolve its re-root inverse against the frame's poses rather than
// against the rest pose.
//
// Almost every node in almost every file has an empty chain, and this loop
// claims nothing for those - which is the whole of "the empty case stays free".
func (c *decoder) claimRerootJoints() {
	for i := range c.model.Scenes {
		for name, node := range c.model.Scenes[i].Nodes {
			if len(node.Animated) == 0 {
				continue
			}
			ancestor := node.Animated[len(node.Animated)-1]
			// Any joint on that node will do. Pose records hold the joint's world
			// transform alone, unpremultiplied, so every joint following one node
			// holds the same world transform whatever inverse bind it carries -
			// and claiming a second one would duplicate a pose row per frame for
			// every named bone of a rig, which on a real fox is most of them.
			joint, ok := c.joints.pose(ancestor)
			if !ok {
				joint = c.joints.claimPlain(ancestor)
			}
			node.RerootJoint = joint
			c.model.Scenes[i].Nodes[name] = node
		}
	}
}

// decodeNodes copies the node array: each node's name, its authored local
// matrix and its children, and finds the nodes nothing parents, which are the
// roots a pose walk starts from. It is the document's forest rather than a
// scene's root list because a joint may sit outside every scene's node list
// and still be referenced by a skin, and its world transform is defined
// regardless.
func (c *decoder) decodeNodes() {
	nodes := make([]Node, len(c.doc.Nodes))
	parented := make([]bool, len(c.doc.Nodes))
	for index, node := range c.doc.Nodes {
		if node == nil {
			nodes[index] = Node{Local: m.NewMat4()}
			continue
		}
		entry := Node{Name: node.Name, Local: nodeMatrix(node), Present: true}
		for _, child := range node.Children {
			if child >= 0 && child < len(c.doc.Nodes) {
				parented[child] = true
				if c.doc.Nodes[child] != nil {
					entry.Children = append(entry.Children, child)
				}
			}
		}
		nodes[index] = entry
	}
	c.model.Nodes = nodes
	for index, node := range c.doc.Nodes {
		if node != nil && !parented[index] {
			c.model.Roots = append(c.model.Roots, index)
		}
	}
}

// decodeClips resolves every animation in the document into a clip, in the
// document's own order, skipping any that steers nothing.
//
// A weights-only animation is a real clip with no pose in it: it produces no
// joint, so a morph-only model loads with an empty pose buffer and still plays
// its clips by name.
func (c *decoder) decodeClips() {
	for _, animation := range c.doc.Animations {
		if animation == nil {
			continue
		}
		clip := Clip{Name: animation.Name}
		byNode := map[int]int{}
		for _, channel := range animation.Channels {
			if channel == nil || channel.Target.Node == nil {
				continue
			}
			node := *channel.Target.Node
			if node < 0 || node >= len(c.doc.Nodes) || c.doc.Nodes[node] == nil {
				continue
			}
			if channel.Target.Path == gltf.TRSWeights {
				if weights, ok := c.weightsChannel(animation, channel, node); ok {
					clip.Weights = append(clip.Weights, weights)
					clip.Duration = max(clip.Duration, weights.Curve.End())
				}
				continue
			}
			curve := c.curve(animation, channel.Sampler)
			if curve == nil {
				continue
			}
			slot, ok := byNode[node]
			if !ok {
				slot = len(clip.Nodes)
				byNode[node] = slot
				clip.Nodes = append(clip.Nodes, ClipNode{Node: node})
			}
			switch channel.Target.Path {
			case gltf.TRSTranslation:
				clip.Nodes[slot].Translation = curve
			case gltf.TRSRotation:
				clip.Nodes[slot].Rotation = curve
			case gltf.TRSScale:
				clip.Nodes[slot].Scale = curve
			default:
				continue
			}
			clip.Duration = max(clip.Duration, curve.End())
		}
		if len(clip.Nodes) == 0 && len(clip.Weights) == 0 {
			continue
		}
		c.model.Clips = append(c.model.Clips, clip)
	}
}

// curve decodes one sampler, or returns nil for one that cannot be read.
// Samplers are interned per document, because a clip that steers twenty bones
// with one shared input accessor should decode it once.
func (c *decoder) curve(animation *gltf.Animation, index int) *Curve {
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
	curve := &Curve{Interpolation: interpolationOf(sampler.Interpolation)}
	if err := readAttribute(c.doc, input, func(_ int, value attrValue) {
		curve.Times = append(curve.Times, value[0])
	}); err != nil {
		return nil
	}
	if err := readAttribute(c.doc, output, func(_ int, value attrValue) {
		curve.Values = append(curve.Values, value)
	}); err != nil {
		return nil
	}
	if len(curve.Times) == 0 || len(curve.Values) == 0 {
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
