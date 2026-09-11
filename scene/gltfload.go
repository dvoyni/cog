package scene

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/m"
	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/ext/lightspunctual"
	"github.com/qmuntal/gltf/ext/texturetransform"
)

// The extension names scene reads. The list is short on purpose: an extension
// that changes shading needs a shader change, and the bundled shader is one
// module with no variants.
const (
	extEmissiveStrength = "KHR_materials_emissive_strength"
	extMeshQuantization = "KHR_mesh_quantization"
)

// supportedRequired is the set an extensionsRequired entry may name. Anything
// else fails the model wholesale, because a required extension is the file
// saying it cannot be drawn correctly without it - and a wrongly drawn model is
// exactly the failure a report cannot make visible.
//
// The four that matter in practice are Draco, meshopt, basisu and webp: each
// replaces geometry or texture encoding with something scene has no decoder
// for, so there is no geometry to fall back to. Rejecting the whole set rather
// than those four by name is glTF's own rule and costs nothing.
var supportedRequired = map[string]bool{
	texturetransform.ExtensionName: true,
	extEmissiveStrength:            true,
	extMeshQuantization:            true,
	lightspunctual.ExtensionName:   true,
}

// loadedModel is one file converted to scene's own types, CPU-side, with no
// GPU handle anywhere in it. It is what crosses from the parse half of a load
// to the upload half.
type loadedModel struct {
	// geometries are the converted primitives, one per distinct glTF primitive
	// rather than one per node that references it. Two wheel nodes sharing a
	// wheel mesh are one upload and two placements, which is how glTF authors
	// repeated parts and what a per-node conversion would silently double.
	geometries []gltfGeometry
	primitives []loadedPrimitive
	materials  []loadedMaterial
	textures   []loadedTexture
	lights     []ModelLight
	// scenes mirrors the file's scenes array entry for entry, each holding the
	// contiguous range of primitives its walk produced. Every scene is
	// flattened, not only the default one, because path is a model's only cache
	// key: a draw naming a scene has no second load to trigger.
	scenes []loadedScene
	// defaultScene is the entry a draw with no Scene selector renders: the
	// file's declared default, or the first.
	defaultScene int
	// neverCull is set by a primitive whose POSITION accessor declared no
	// min/max. It is per model rather than per primitive because a model with
	// one unbounded primitive has no bound at all - culling the rest of it
	// would leave the unbounded piece drawn alone.
	neverCull bool
	// animation is the model's baked poses, joint records and clip table. A
	// file with no skins and no animated mesh node bakes an empty one, which
	// is what puts every one of its draws on a variant with no group 2 at all.
	animation bakedAnimation
	// morphDeltas is the model's one delta buffer, every morphed primitive's
	// targets concatenated and reached by a base offset. One buffer per model
	// rather than per primitive: a buffer per primitive would mean a bind
	// group per primitive, collapsing group 2's whole reason for existing.
	morphDeltas []m.Vec4
	// reports are the non-fatal failures the load accumulated: a missing
	// texture, an unsupported topology, a UV set past the two scene carries.
	// They fire at install, from the load's own goroutine, which is why an
	// error can outlive the draw call that caused it.
	reports []error
}

// loadedPrimitive is one flattened primitive: geometry in its own local space,
// the matrix that places it under the scene root, and the material variant it
// draws with.
type loadedPrimitive struct {
	// geometry indexes loadedModel.geometries, so a mesh referenced by several
	// nodes is converted, uploaded and given a mesh id exactly once.
	geometry int
	local    m.Mat4
	// rest is where the primitive sits in the model's rest pose: local for
	// everything placed by its own instance record, and the node's authored
	// world transform for everything placed by a row of the pose buffer. The
	// two differ exactly where local is the identity, and only the cold facade
	// reads this one - Bounds and AABB answer about a pose, and the only pose
	// the load has is the rest one.
	rest     m.Mat4
	material int
	// skinned reports whether the primitive's placement lives in the pose
	// buffer rather than in local. A skinned primitive draws through its
	// joints, so local is the identity and its bounding sphere is the bind
	// pose's - which is why it is also never culled.
	//
	// It is the placement's answer and not the geometry's, and it has to be:
	// one converted mesh is plain-bound under one node and static under
	// another, so asking the geometry would cull a shared primitive by the
	// static instance's bounds while its animated one walks out of them.
	skinned bool
	// joint is the model joint a plain-bound placement rides at full weight,
	// and plain says it is one. A plain binding implies skinned; joint 0 is a
	// joint like any other, which is why the bool is not derived from the
	// index.
	joint uint32
	plain bool
	// morph is where the primitive's delta block sits and which of the model's
	// weight slots feed it. The block comes from the geometry, shared by every
	// node referencing that mesh; the slots come from this node.
	morph morphBinding
}

// loadedScene is one entry of the file's scenes array, flattened: the range of
// primitives its walk produced and the nodes within it a Node selector can
// address.
type loadedScene struct {
	name       string
	start, end int
	// nodes is keyed by name, holding the first depth-first match of each. An
	// unnamed node is absent: a selector is a name, so a node without one is
	// not addressable and there is nothing to record.
	nodes map[string]loadedNode
	// order is the same names again, in the order the walk claimed them, which
	// is depth-first. The map answers a selector and this answers Nodes: a map
	// has no order at all, and sorting one alphabetically would report a
	// hierarchy as an alphabet.
	order []string
}

// loadedNode is one addressable node: the contiguous slice of the flattened
// list its subtree occupies, and what re-rooting that slice needs.
type loadedNode struct {
	start, end int
	// first and last bracket the node's own entry and its named descendants'
	// in the scene's order slice, which depth-first claiming makes contiguous
	// the same way it makes the primitive range contiguous. It is a range over
	// names rather than over primitives because a node carrying no geometry has
	// an empty primitive range that a sibling's would be indistinguishable
	// from, and Nodes has to list such a node.
	first, last int
	// reroot is the inverse of the node's authored world transform, which a
	// Node draw applies to discard it. rerootable is false when that transform
	// collapsed an axis and has no inverse - the draw reports and skips rather
	// than drawing through a matrix that is quietly wrong.
	reroot     m.Mat4
	rerootable bool
	// animated is the node's chain of animated ancestors, root-first, and is
	// empty for almost every node in almost every file. A non-empty chain means
	// the node's true world transform is time-varying, so the inverse above is
	// the rest pose's and the real one has to be resolved against the frame's
	// baked pose rows.
	//
	// rerootJoint is the joint carrying the chain's deepest link - its last
	// entry - and rest is that link's own authored world matrix. Everything
	// between that link and this node is rigid, by construction: the link is
	// the deepest ancestor a clip steers. So the frame's true re-root is
	//
	//	reroot * rest * inverse(pose(rerootJoint))
	//
	// which is two products at pack time and collapses to reroot exactly when
	// the pose is the rest pose. Both fields are meaningless when animated is
	// empty, and nothing reads them there.
	animated    []int
	rerootJoint int
	rest        m.Mat4
}

// geometryKey interns one converted primitive. Tangent generation is part of
// the key because it depends on the material a node drew the mesh with: the
// same mesh under a normal-mapped material and a plain one is two conversions,
// which is rare and correct.
type geometryKey struct {
	mesh, primitive int
	tangents        bool
	// skin is part of the key because remapping JOINTS_0 into the model's one
	// numbering rewrites the vertex buffer. The same mesh under two skins is
	// two conversions, which is rare and correct.
	//
	// The node joint is not, and that is the whole of this field's history: it
	// was keyed here because it shared a name with the skin, and it overwrites
	// every vertex with a value the geometry has no opinion about. It rides
	// the instance record instead, so a mesh under nine animated nodes is one
	// conversion and nine placements.
	skin int
}

// loadedMaterial is one glTF material converted to the bundled PBR: the record
// scene binds per batch, the pipeline state it draws under, and the texture and
// sampler each of the five slots binds.
type loadedMaterial struct {
	record scenePbrRecord
	state  gfx.MaterialState
	// slots index loadedModel.textures, or missingTexture for a slot the file
	// left empty or whose image could not be decoded. Both bind the same 1x1
	// default, which is what makes a partial failure a resident model.
	slots    [pbrSlotCount]int
	samplers [pbrSlotCount]gfx.SamplerDesc
}

// materialVariant keys the material table of one load. A flattened matrix with
// a negative determinant reverses winding, and pipeline state is per material
// with no per-draw override, so such a primitive needs its own FrontCW copy of
// the material it shares.
type materialVariant struct {
	material int
	frontCW  bool
}

// defaultMaterial is the material index of a primitive that names none, which
// glTF defines as the fully-metallic white default.
const defaultMaterial = -1

// modelConverter turns one gltf.Document into a loadedModel in a single pass
// and then drops the document. Nothing in gltf's types reaches scene's API: the
// document is about 2x the file in memory, and holding it for the life of a
// resident model would double what a model costs for the ability to re-read
// fields scene already copied.
type modelConverter struct {
	doc      *gltf.Document
	path     string
	textures *textureLoader
	model    loadedModel
	// geometries interns the converted primitives, keyed by where they came
	// from rather than by their bytes: hashing a megabyte of vertices to find a
	// duplicate the file already told us about would be work for nothing.
	geometries map[geometryKey]int
	// variants interns the (material, winding) pairs the flattening asked for,
	// so a model whose every node has a positive determinant - which is most of
	// them - builds exactly one record per glTF material.
	variants map[materialVariant]int
	// visited guards against a cyclic node graph, which is malformed but is a
	// stack overflow rather than a report if nothing checks. It is cleared
	// between scenes rather than kept for the file: a node two scenes both root
	// belongs to both, and a file-wide guard would leave the second empty.
	visited map[int]bool
	// scene is the entry the walk is filling, and points into model.scenes.
	scene *loadedScene
	// animated is the set of nodes some animation steers, and chain the
	// animated ancestors of the node the walk is inside, root-first.
	// chainWorlds holds those ancestors' authored world matrices, so a node
	// claiming its name can record the deepest one without a second walk.
	animated    map[int]bool
	chain       []int
	chainWorlds []m.Mat4
	// sampleRate is Config.PoseSampleRate, the global grid every clip bakes
	// onto. It reaches the parse through the load request rather than through
	// a Lookup, because the parse holds no resource at all.
	sampleRate int
	// joints is the model's one joint numbering, and skinJoints each skin's
	// slots resolved into it, so remapping a primitive's JOINTS_0 is an index
	// rather than a search.
	joints     jointSpace
	skinJoints [][]int
	// curves interns the animation samplers the clips decode, so a clip whose
	// twenty bones share one input accessor reads it once. morphCurves is the
	// same interning for the weights channels, which decode to a different
	// shape: targetCount scalars per keyframe rather than one widened value.
	curves      map[samplerKey]*animCurve
	morphCurves map[samplerKey]*morphCurve
	// morphNodes is each node's run of the model's flattened weight slots,
	// claimed in the flattening walk's depth-first order, and morphDefaults
	// and morphNames the slot-indexed arrays that grow with it. The defaults
	// are node.weights over mesh.weights over zero, resolved once at load.
	morphNodes    map[int]morphSlotRun
	morphDefaults []float32
	morphNames    []string
	// nodeRoots are the nodes nothing parents, and walked, locals and worlds
	// the pose walk's scratch. All four keep their backing across the whole
	// bake: the walk runs once per sampled frame and must allocate nothing.
	nodeRoots []int
	walked    []bool
	locals    []m.Mat4
	worlds    []m.Mat4
	// poseReported keeps the unrepresentable-pose report to one per model
	// however many joints and frames carry shear.
	poseReported bool
	// duplicated keeps a repeated node name to one report however many scenes
	// and however many nodes carry it.
	duplicated map[string]bool
	// boundsReported keeps the missing-bounds report to one per model however
	// many primitives declared no min/max.
	boundsReported bool
}

// convertDocument converts one parsed document. filesystem resolves external
// image URIs and may be nil, in which case a file naming one loses that texture
// to the 1x1 default and says so.
func convertDocument(
	doc *gltf.Document, path string, filesystem fs.FS, sampleRate int,
) (*loadedModel, error) {
	if err := checkRequiredExtensions(doc); err != nil {
		return nil, err
	}
	if len(doc.Scenes) == 0 {
		return nil, errors.New("it has no scenes")
	}
	converter := &modelConverter{
		doc:         doc,
		path:        path,
		textures:    newTextureLoader(doc, filesystem, path),
		geometries:  map[geometryKey]int{},
		variants:    map[materialVariant]int{},
		visited:     map[int]bool{},
		animated:    animatedNodes(doc),
		duplicated:  map[string]bool{},
		sampleRate:  sampleRate,
		curves:      map[samplerKey]*animCurve{},
		morphCurves: map[samplerKey]*morphCurve{},
		morphNodes:  map[int]morphSlotRun{},
	}
	// The joint numbering and the node forest are both settled before the walk
	// starts: a primitive's JOINTS_0 remaps as it is read, and the pose walk
	// needs roots the flattening never computes because a joint may sit
	// outside every scene's node list.
	if err := converter.buildJointSpace(); err != nil {
		return nil, err
	}
	converter.buildNodeForest()
	converter.model.defaultScene = defaultSceneIndex(doc)
	converter.model.scenes = make([]loadedScene, 0, len(doc.Scenes))
	for _, scene := range doc.Scenes {
		converter.flattenScene(scene)
	}
	// Both run last because the walk is what claims a degenerate node's joint
	// and a morphed node's weight slots, so neither numbering is complete
	// until every scene is flattened.
	converter.packMorphDeltas()
	converter.bakeAnimation()
	converter.model.textures = converter.textures.textures
	converter.model.reports = append(converter.model.reports, converter.textures.reports...)
	return &converter.model, nil
}

// checkRequiredExtensions fails a model whose file says it cannot be drawn
// without something scene does not implement.
func checkRequiredExtensions(doc *gltf.Document) error {
	for _, required := range doc.ExtensionsRequired {
		if !supportedRequired[required] {
			return fmt.Errorf("it requires the %s extension, which scene does not implement", required)
		}
	}
	return nil
}

// defaultSceneIndex picks the scene a draw with no Scene selector renders: the
// file's declared default, or the first one.
func defaultSceneIndex(doc *gltf.Document) int {
	if doc.Scene != nil && *doc.Scene >= 0 && *doc.Scene < len(doc.Scenes) {
		return *doc.Scene
	}
	return 0
}

// animatedNodes is the set of nodes some animation steers with a TRS channel.
// Such a node has no fixed authored world transform, and neither has any
// descendant of it - which is the whole reason a re-root inverse cannot always
// be a matrix computed here.
//
// A weights channel is not in it: morph weights reshape a mesh and leave the
// node where it was.
func animatedNodes(doc *gltf.Document) map[int]bool {
	animated := map[int]bool{}
	for _, animation := range doc.Animations {
		if animation == nil {
			continue
		}
		for _, channel := range animation.Channels {
			if channel == nil || channel.Target.Node == nil {
				continue
			}
			switch channel.Target.Path {
			case gltf.TRSTranslation, gltf.TRSRotation, gltf.TRSScale:
				animated[*channel.Target.Node] = true
			}
		}
	}
	return animated
}

// flattenScene walks one scene into its own contiguous range of the flattened
// list. Every scene in the file gets one, because a draw that names a scene
// resolves against the load the path already paid for.
func (c *modelConverter) flattenScene(scene *gltf.Scene) {
	if scene == nil {
		c.model.scenes = append(c.model.scenes, loadedScene{})
		return
	}
	start := len(c.model.primitives)
	c.model.scenes = append(c.model.scenes, loadedScene{
		name: scene.Name, start: start, nodes: map[string]loadedNode{},
	})
	c.scene = &c.model.scenes[len(c.model.scenes)-1]
	clear(c.visited)
	c.chain = c.chain[:0]
	for _, root := range scene.Nodes {
		c.walkNode(root, m.NewMat4())
	}
	c.scene.end = len(c.model.primitives)
	c.scene = nil
}

// walkNode flattens one node and its subtree depth-first, accumulating the
// matrix that places it relative to the scene root.
//
// Depth-first is what makes a subtree a contiguous slice of the result rather
// than a filter over it, which is the whole mechanism the Node selector will
// use: a node's primitives and all its descendants' are emitted before anything
// outside the subtree.
func (c *modelConverter) walkNode(index int, parent m.Mat4) {
	if index < 0 || index >= len(c.doc.Nodes) || c.doc.Nodes[index] == nil || c.visited[index] {
		return
	}
	c.visited[index] = true
	node := c.doc.Nodes[index]
	world := parent.Mul(nodeMatrix(node))
	// A skinned node's own transform is ignored per the glTF specification: its
	// joints resolve against the scene root, so applying the node's matrix as
	// well would apply it twice. Descendants still inherit it, because the
	// hierarchy is a hierarchy whether or not this node is skinned.
	//
	// A node whose world transform is time-varying and that carries a mesh of
	// its own takes the same treatment through a degenerate single-joint skin:
	// its transform lives in the pose buffer instead, so its placement here is
	// the identity too. "Time-varying" is inherited, not local - a static prop
	// bolted to a spinning turret moves with the turret - so the test is the
	// node's own channels or a non-empty ancestor chain.
	//
	// rest is where those primitives sit in the model's rest pose, which is the
	// answer Bounds and AABB owe a caller. For a plain joint that is the node's
	// own world transform, which row 0 of the pose buffer resolves to; for a
	// glTF skin it is the identity, because the skin's joints already resolve
	// against the scene root and its vertices are authored there.
	placement, rest := world, world
	binding := nodeBinding{skin: -1, joint: -1}
	switch {
	case node.Skin != nil:
		placement, rest = m.NewMat4(), m.NewMat4()
		binding.skin = *node.Skin
	case node.Mesh != nil && (c.animated[index] || len(c.chain) > 0):
		placement = m.NewMat4()
		binding.joint = c.joints.claimPlain(index)
	}
	// The name is claimed before the subtree is walked and closed after, so a
	// name a node shares with one of its own descendants resolves to the node -
	// which is what "first depth-first match" says, and what recording the
	// subtree on the way back out would get backwards.
	named := c.claimNode(node.Name, world)
	if node.Mesh != nil {
		c.flattenMesh(*node.Mesh, placement, rest, binding, c.claimMorphSlots(index))
	}
	c.collectLight(node, world)
	animated := c.animated[index]
	if animated {
		c.chain = append(c.chain, index)
		c.chainWorlds = append(c.chainWorlds, world)
	}
	for _, child := range node.Children {
		c.walkNode(child, world)
	}
	if animated {
		c.chain = c.chain[:len(c.chain)-1]
		c.chainWorlds = c.chainWorlds[:len(c.chainWorlds)-1]
	}
	if named {
		c.closeNode(node.Name)
	}
}

// claimNode records one named node's slice, opened at the walk's position, and
// reports whether this node is the one that owns the name. A duplicate keeps
// the first match and says so once.
func (c *modelConverter) claimNode(name string, world m.Mat4) bool {
	if name == "" || c.scene == nil {
		return false
	}
	if _, taken := c.scene.nodes[name]; taken {
		if !c.duplicated[name] {
			c.duplicated[name] = true
			c.model.reports = append(c.model.reports,
				ErrModelNodeDuplicated{Model: c.path, Node: name})
		}
		return false
	}
	reroot, rerootable := world.InverseAffine()
	node := loadedNode{
		start: len(c.model.primitives), reroot: reroot, rerootable: rerootable,
		rerootJoint: -1, first: len(c.scene.order),
	}
	c.scene.order = append(c.scene.order, name)
	if len(c.chain) > 0 {
		node.animated = append([]int(nil), c.chain...)
		node.rest = c.chainWorlds[len(c.chainWorlds)-1]
	}
	c.scene.nodes[name] = node
	return true
}

// closeNode ends a claimed node's slice at the walk's position, which
// depth-first order has just made the end of its whole subtree.
func (c *modelConverter) closeNode(name string) {
	node := c.scene.nodes[name]
	node.end = len(c.model.primitives)
	node.last = len(c.scene.order)
	c.scene.nodes[name] = node
}

// nodeBinding is how one node binds its mesh to the model's joints, and it is
// two answers rather than one because the two live in different places.
//
// They were one type once, named for the skin, and the node joint was keyed
// into the geometry along with it. That is what made the same mesh under nine
// animated nodes nine conversions.
type nodeBinding struct {
	// skin is the glTF skin whose JOINTS_0 the conversion remaps into the
	// model's one numbering, or -1. Remapping rewrites the vertex buffer, so
	// this belongs to the geometry and is part of its key.
	skin int
	// joint is the model joint the node's own transform lives in, or -1. It
	// overwrites nothing: the placement names the bone and every vertex under
	// it rides at full weight, so this belongs to the instance record and is
	// not part of any key.
	joint int
}

// flattenMesh emits one node's primitives at their flattened placement.
func (c *modelConverter) flattenMesh(
	index int, placement, rest m.Mat4, binding nodeBinding, slots morphSlotRun,
) {
	if index < 0 || index >= len(c.doc.Meshes) || c.doc.Meshes[index] == nil {
		return
	}
	// A negative determinant mirrors the geometry, which reverses triangle
	// winding. Pipeline state is per material and the draw gets no say, so the
	// winding has to be answered with a material variant rather than a flag on
	// the instance.
	frontCW := placement.Determinant() < 0
	for at, primitive := range c.doc.Meshes[index].Primitives {
		material := c.material(primitive.Material, frontCW)
		tangents := c.model.materials[material].slots[normalSlot] != missingTexture
		geometry, ok := c.geometry(index, at, primitive, tangents, binding.skin)
		if !ok {
			continue
		}
		converted := &c.model.geometries[geometry]
		morph := converted.morph.binding(len(converted.vertices))
		if morph.morphed() {
			// A primitive carrying more targets than its node claimed slots for
			// is a malformed mesh - glTF requires every primitive of a mesh to
			// declare the same targets - so the extra targets simply have no
			// weight to read.
			morph.slotBase, morph.targets = slots.base, min(morph.targets, slots.count)
		}
		// Skinned is the placement's answer, not the geometry's: the same
		// converted cube is plain-bound under one node and static under
		// another, so the geometry cannot be asked.
		placed := loadedPrimitive{
			geometry: geometry, local: placement, rest: rest, material: material,
			skinned: converted.skinned, morph: morph,
		}
		if binding.joint >= 0 {
			placed.joint, placed.plain, placed.skinned = uint32(binding.joint), true, true
		}
		// The layout is the union of that same answer over every placement, and
		// it is taken here so that the two can never be derived from different
		// facts: a placement that draws under SCENE_SKIN is a placement whose
		// geometry supplies the joints and the weights that variant declares.
		// A plain-bound placement widens a geometry no skin ever touched, which
		// is the tax this union charges and the reason it is a union at all.
		converted.skinnedLayout = converted.skinnedLayout || placed.skinned
		c.model.primitives = append(c.model.primitives, placed)
	}
}

// geometry converts one primitive, or returns the conversion an earlier node
// referencing the same mesh already paid for.
func (c *modelConverter) geometry(
	mesh, at int, primitive *gltf.Primitive, tangents bool, skin int,
) (int, bool) {
	key := geometryKey{mesh: mesh, primitive: at, tangents: tangents, skin: skin}
	if index, ok := c.geometries[key]; ok {
		return index, index >= 0
	}
	geometry, err := convertPrimitive(c.doc, primitive, tangents)
	if err == nil {
		c.bindGeometryJoints(&geometry, skin)
	}
	if err != nil {
		// The failure is interned too, so a mesh referenced by ten nodes
		// reports its one bad primitive once rather than ten times.
		c.geometries[key] = -1
		c.model.reports = append(c.model.reports, ErrModelPrimitiveSkipped{
			Model: c.path, Mesh: c.doc.Meshes[mesh].Name, Err: err,
		})
		return -1, false
	}
	if !geometry.hasBox && !c.boundsReported {
		c.boundsReported, c.model.neverCull = true, true
		c.model.reports = append(c.model.reports, ErrModelBoundsMissing{Model: c.path})
	}
	c.geometries[key] = len(c.model.geometries)
	c.model.geometries = append(c.model.geometries, geometry)
	return len(c.model.geometries) - 1, true
}

// collectLight records a node's KHR_lights_punctual light in the model's own
// space. Nothing converts it: a light is data an app declares, at whatever
// world transform it drew the model at.
func (c *modelConverter) collectLight(node *gltf.Node, world m.Mat4) {
	index, ok := node.Extensions[lightspunctual.ExtensionName].(lightspunctual.LightIndex)
	if !ok {
		return
	}
	lights, ok := c.doc.Extensions[lightspunctual.ExtensionName].(lightspunctual.Lights)
	if !ok || int(index) < 0 || int(index) >= len(lights) || lights[index] == nil {
		return
	}
	light := lights[index]
	colour := light.ColorOrDefault()
	// glTF punctual lights point down their node's local -Z, which is also the
	// convention scene's spot direction takes: the direction light travels.
	direction := world.TransformDirection(m.Vec3{Z: -1}).Normalize()
	descr := LightDescr{
		Position:  world.Translation(),
		Direction: direction,
		Color:     m.NewColorLinear(float32(colour[0]), float32(colour[1]), float32(colour[2]), 1),
		Intensity: float32(light.IntensityOrDefault()),
	}
	if light.Range != nil {
		descr.Range = float32(*light.Range)
	}
	entry := ModelLight{Name: light.Name, Descr: descr}
	switch light.Type {
	case lightspunctual.TypeDirectional:
		entry.Directional = true
	case lightspunctual.TypeSpot:
		descr.Kind = LightSpot
		if light.Spot != nil {
			descr.InnerCone = float32(light.Spot.InnerConeAngle)
			descr.OuterCone = float32(light.Spot.OuterConeAngleOrDefault())
		}
		entry.Descr = descr
	}
	c.model.lights = append(c.model.lights, entry)
}

// material returns the index of the converted material one primitive draws
// with, converting it the first time this load asks for that winding.
func (c *modelConverter) material(index *int, frontCW bool) int {
	source := defaultMaterial
	if index != nil {
		source = *index
	}
	key := materialVariant{material: source, frontCW: frontCW}
	if existing, ok := c.variants[key]; ok {
		return existing
	}
	converted := c.convertMaterial(source, frontCW)
	c.variants[key] = len(c.model.materials)
	c.model.materials = append(c.model.materials, converted)
	return len(c.model.materials) - 1
}

// convertMaterial turns one glTF material into a bundled-PBR record plus its
// texture bindings.
//
// The record's numbers are glTF's by verbatim name, which is what makes the
// glTF specification the parameter documentation and what lets OverrideParams
// merge by name with no translation table to drift out of date.
func (c *modelConverter) convertMaterial(index int, frontCW bool) loadedMaterial {
	converted := loadedMaterial{record: defaultPbrRecord(), state: pbrState(alphaOpaque, false)}
	for slot := range converted.slots {
		converted.slots[slot] = missingTexture
		converted.samplers[slot] = defaultModelSampler
	}
	if frontCW {
		converted.state.FrontFace = gfx.FrontCW
	}
	if index < 0 || index >= len(c.doc.Materials) || c.doc.Materials[index] == nil {
		return converted
	}
	material := c.doc.Materials[index]
	converted.state = pbrState(alphaModeOf(material.AlphaMode), material.DoubleSided)
	if frontCW {
		converted.state.FrontFace = gfx.FrontCW
	}
	if material.AlphaMode == gltf.AlphaMask {
		converted.record.AlphaCutoff = float32(material.AlphaCutoffOrDefault())
	}
	if pbr := material.PBRMetallicRoughness; pbr != nil {
		factor := pbr.BaseColorFactorOrDefault()
		converted.record.BaseColorFactor = m.Vec4{
			X: float32(factor[0]), Y: float32(factor[1]),
			Z: float32(factor[2]), W: float32(factor[3]),
		}
		converted.record.MetallicFactor = float32(pbr.MetallicFactorOrDefault())
		converted.record.RoughnessFactor = float32(pbr.RoughnessFactorOrDefault())
		c.bindSlot(&converted, baseColorSlot, pbr.BaseColorTexture, true)
		c.bindSlot(&converted, metallicRoughnessSlot, pbr.MetallicRoughnessTexture, false)
	}
	if material.NormalTexture != nil && material.NormalTexture.Index != nil {
		converted.record.NormalScale = float32(material.NormalTexture.ScaleOrDefault())
		c.bindSlot(&converted, normalSlot, &gltf.TextureInfo{
			Index:      *material.NormalTexture.Index,
			TexCoord:   material.NormalTexture.TexCoord,
			Extensions: material.NormalTexture.Extensions,
		}, false)
	}
	if material.OcclusionTexture != nil && material.OcclusionTexture.Index != nil {
		converted.record.OcclusionStrength = float32(material.OcclusionTexture.StrengthOrDefault())
		c.bindSlot(&converted, occlusionSlot, &gltf.TextureInfo{
			Index:      *material.OcclusionTexture.Index,
			TexCoord:   material.OcclusionTexture.TexCoord,
			Extensions: material.OcclusionTexture.Extensions,
		}, false)
	}
	c.bindSlot(&converted, emissiveSlot, material.EmissiveTexture, true)
	converted.record.EmissiveFactor = emissiveFactor(material)
	return converted
}

// bindSlot fills one texture slot: the image, its sampler, its UV set and its
// KHR_texture_transform. An absent slot keeps the 1x1 default and the identity
// transform, so the shader's unconditional five samples cost the same either
// way.
func (c *modelConverter) bindSlot(
	converted *loadedMaterial, slot int, info *gltf.TextureInfo, srgb bool,
) {
	if info == nil {
		return
	}
	texture, sampler := c.textures.texture(info.Index, srgb)
	converted.slots[slot], converted.samplers[slot] = texture, sampler
	texCoord := info.TexCoord
	if transform, ok := info.Extensions[texturetransform.ExtensionName].(*texturetransform.TextureTranform); ok {
		scale := transform.ScaleOrDefault()
		converted.record.Transforms[slot] = m.Vec4{
			X: float32(transform.Offset[0]), Y: float32(transform.Offset[1]),
			Z: float32(scale[0]), W: float32(scale[1]),
		}
		converted.record.Rotations[slot] = float32(transform.Rotation)
		if transform.TexCoord != nil {
			texCoord = *transform.TexCoord
		}
	}
	converted.record.selectUVSet(func(err error) {
		c.model.reports = append(c.model.reports, err)
	}, slot, texCoord)
}

// The five slot indices, in the record order pbrSlots fixes. normalSlot is
// already named there, because it is the one slot whose 1x1 default is the flat
// normal rather than the white texel.
const (
	baseColorSlot         = 0
	metallicRoughnessSlot = 1
	occlusionSlot         = 3
	emissiveSlot          = 4
)

// emissiveFactor folds KHR_materials_emissive_strength into emissiveFactor and
// clamps the product at 1.
//
// Folding at load is what keeps the record's numbers glTF's own: a separate
// strength member would be a sixth factor the shader multiplies and
// OverrideParams would have to know about. The clamp is the honest limit of an
// 8-bit sRGB target with no tonemapping and no exposure control - a strength of
// 8 has nowhere to go but white, and clipping it here at least keeps the hue.
func emissiveFactor(material *gltf.Material) m.Vec4 {
	strength := float32(1)
	if raw, ok := material.Extensions[extEmissiveStrength]; ok {
		strength = emissiveStrength(raw, strength)
	}
	factor := m.Vec4{
		X: float32(material.EmissiveFactor[0]) * strength,
		Y: float32(material.EmissiveFactor[1]) * strength,
		Z: float32(material.EmissiveFactor[2]) * strength,
	}
	return m.Vec4{
		X: min(factor.X, 1), Y: min(factor.Y, 1), Z: min(factor.Z, 1),
	}
}

// emissiveStrength reads the extension's one member. There is no ext package
// for it, so the payload arrives as raw JSON and is parsed here; a payload that
// does not parse leaves the strength at 1, which is the extension's own
// default and renders the material as though it were absent.
func emissiveStrength(raw any, fallback float32) float32 {
	var data []byte
	switch payload := raw.(type) {
	case json.RawMessage:
		data = payload
	case []byte:
		data = payload
	default:
		return fallback
	}
	var payload struct {
		EmissiveStrength *float64 `json:"emissiveStrength"`
	}
	if err := json.Unmarshal(data, &payload); err != nil || payload.EmissiveStrength == nil {
		return fallback
	}
	if math.IsNaN(*payload.EmissiveStrength) || *payload.EmissiveStrength < 0 {
		return fallback
	}
	return float32(*payload.EmissiveStrength)
}

// alphaModeOf maps glTF's alphaMode onto scene's.
func alphaModeOf(mode gltf.AlphaMode) alphaMode {
	switch mode {
	case gltf.AlphaMask:
		return alphaMask
	case gltf.AlphaBlend:
		return alphaBlend
	}
	return alphaOpaque
}

// nodeMatrix resolves one node's local transform. glTF stores its matrix
// column-major, which is m.Mat4's own layout, so the matrix case is a widening
// copy and nothing else.
func nodeMatrix(node *gltf.Node) m.Mat4 {
	if node.Matrix != [16]float64{} && node.Matrix != gltf.DefaultMatrix {
		var matrix m.Mat4
		for i, value := range node.Matrix {
			matrix[i] = float32(value)
		}
		return matrix
	}
	translation := node.TranslationOrDefault()
	rotation := node.RotationOrDefault()
	scale := node.ScaleOrDefault()
	return m.TRS4(
		m.Vec3{X: float32(translation[0]), Y: float32(translation[1]), Z: float32(translation[2])},
		m.Quat{
			X: float32(rotation[0]), Y: float32(rotation[1]),
			Z: float32(rotation[2]), W: float32(rotation[3]),
		},
		m.Vec3{X: float32(scale[0]), Y: float32(scale[1]), Z: float32(scale[2])},
	)
}
