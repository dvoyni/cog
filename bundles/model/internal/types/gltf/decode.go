package gltf

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"path"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/ext/lightspunctual"
	"github.com/qmuntal/gltf/ext/texturetransform"
)

// The extension names the decoder reads. The list is short on purpose: an
// extension that changes shading needs a shader change, and the bundled shader
// is one module with no variants.
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
// replaces geometry or texture encoding with something there is no decoder
// for, so there is no geometry to fall back to. Rejecting the whole set rather
// than those four by name is glTF's own rule and costs nothing.
var supportedRequired = map[string]bool{
	texturetransform.ExtensionName: true,
	extEmissiveStrength:            true,
	extMeshQuantization:            true,
	lightspunctual.ExtensionName:   true,
}

// Model is one glTF file decoded into plain data: vertex attribute arrays and
// index lists, unbaked curves and skins, morph target floats, material
// parameters as plain values, image references, lights, and the flattened scene
// walk. It holds no GPU layout and no handle of any kind, and nothing of the
// glTF library's document survives into it: the document is about twice the
// file in memory, and holding it for the life of a resident model would double
// what a model costs.
//
// It is what crosses from the decoder to model, which packs vertices, bakes
// clips onto its pose grid, packs morph blocks and fills material records from
// it.
type Model struct {
	// Geometries are the decoded primitives, one per distinct glTF primitive
	// rather than one per node that references it. Two wheel nodes sharing a
	// wheel mesh are one upload and two placements, which is how glTF authors
	// repeated parts and what a per-node conversion would silently double.
	Geometries []Geometry
	Primitives []Primitive
	Materials  []Material
	Images     []Image
	Lights     []Light
	// Scenes mirrors the file's scenes array entry for entry, each holding the
	// contiguous range of primitives its walk produced. Every scene is
	// flattened, not only the default one, because path is a model's only cache
	// key: a draw naming a scene has no second load to trigger.
	Scenes []Scene
	// DefaultScene is the entry a draw with no Scene selector renders: the
	// file's declared default, or the first.
	DefaultScene int
	// NeverCull is set by a primitive whose POSITION accessor declared no
	// min/max. It is per model rather than per primitive because a model with
	// one unbounded primitive has no bound at all - culling the rest of it
	// would leave the unbounded piece drawn alone.
	NeverCull bool

	// Joints is the model's one joint numbering, Skins each skin's slots
	// resolved into it, Nodes the file's node forest and Roots the nodes
	// nothing parents. They are what a pose bake walks.
	Joints []Joint
	Skins  []Skin
	Nodes  []Node
	Roots  []int
	// Clips are the file's animations, unbaked.
	Clips []Clip
	// MorphDefaults is the model's flattened weight slots, one per target of
	// every morphed node in depth-first node order, each holding node.weights
	// over mesh.weights over zero; MorphNames is each slot's target name.
	MorphDefaults []float32
	MorphNames    []string

	// Reports are the non-fatal failures the decode accumulated: a missing
	// texture, an unsupported topology, a duplicated node name. They are
	// fired at install, which is why an error can outlive the draw call that
	// caused it.
	Reports []error
}

// Primitive is one flattened primitive: the geometry it draws, the matrix that
// places it under the scene root, and the material it draws with.
type Primitive struct {
	// Geometry indexes Model.Geometries, so a mesh referenced by several
	// nodes is decoded once.
	Geometry int
	Local    m.Mat4
	// Rest is where the primitive sits in the model's rest pose: Local for
	// everything placed by its own instance, and the node's authored world
	// transform for everything placed through the pose buffer. The two differ
	// exactly where Local is the identity.
	Rest     m.Mat4
	Material int
	// Skinned reports whether the primitive's placement lives in the pose
	// buffer rather than in Local. It is the placement's answer and not the
	// geometry's: one decoded mesh is plain-bound under one node and static
	// under another.
	Skinned bool
	// Joint is the model joint a plain-bound placement rides at full weight,
	// and Plain says it is one. A plain binding implies Skinned; joint 0 is a
	// joint like any other, which is why the bool is not derived from the
	// index.
	Joint uint32
	Plain bool
	// MorphSlotBase and MorphTargets are the run of the model's weight slots
	// that feed this primitive's targets, both zero for a primitive with none.
	// The targets come from the geometry, shared by every node referencing
	// that mesh; the slots come from this node.
	MorphSlotBase, MorphTargets int
}

// Scene is one entry of the file's scenes array, flattened: the range of
// primitives its walk produced and the nodes within it a Node selector can
// address.
type Scene struct {
	Name       string
	Start, End int
	// Nodes is keyed by name, holding the first depth-first match of each. An
	// unnamed node is absent: a selector is a name, so a node without one is
	// not addressable and there is nothing to record.
	Nodes map[string]SceneNode
	// Order is the same names again, in the order the walk claimed them, which
	// is depth-first. The map answers a selector and this answers a listing: a
	// map has no order at all, and sorting one alphabetically would report a
	// hierarchy as an alphabet.
	Order []string
}

// SceneNode is one addressable node: the contiguous slice of the flattened
// list its subtree occupies, and what re-rooting that slice needs.
type SceneNode struct {
	Start, End int
	// First and Last bracket the node's own entry and its named descendants'
	// in the scene's Order, which depth-first claiming makes contiguous the
	// same way it makes the primitive range contiguous. It is a range over
	// names rather than over primitives because a node carrying no geometry has
	// an empty primitive range that a sibling's would be indistinguishable
	// from, and a listing has to include such a node.
	First, Last int
	// Reroot is the inverse of the node's authored world transform, which a
	// Node draw applies to discard it. Rerootable is false when that transform
	// collapsed an axis and has no inverse - the draw reports and skips rather
	// than drawing through a matrix that is quietly wrong.
	Reroot     m.Mat4
	Rerootable bool
	// Animated is the node's chain of animated ancestors, root-first, and is
	// empty for almost every node in almost every file. A non-empty chain means
	// the node's true world transform is time-varying, so Reroot is the rest
	// pose's and the real one has to be resolved against the frame's pose.
	//
	// RerootJoint is the joint carrying the chain's deepest link - its last
	// entry - and Rest is that link's own authored world matrix. Everything
	// between that link and this node is rigid, by construction: the link is
	// the deepest ancestor a clip steers. So the frame's true re-root is
	//
	//	Reroot * Rest * inverse(pose(RerootJoint))
	//
	// which collapses to Reroot exactly when the pose is the rest pose. Both
	// fields are meaningless when Animated is empty, and nothing reads them
	// there.
	Animated    []int
	RerootJoint int
	Rest        m.Mat4
}

// AlphaMode is a material's glTF alphaMode.
type AlphaMode uint8

const (
	AlphaOpaque AlphaMode = iota
	AlphaMask
	AlphaBlend
)

// The five texture slots of a glTF metallic-roughness material, in the order
// Material.Slots holds them.
const (
	SlotBaseColor = iota
	SlotMetallicRoughness
	SlotNormal
	SlotOcclusion
	SlotEmissive
	SlotCount
)

// Material is one glTF material as plain values, once for each winding a
// placement drew it with.
//
// Every number is glTF's own, under its own name, with glTF's default where the
// file said nothing, so the default material - a primitive naming none - is
// white, fully metallic and fully rough.
type Material struct {
	AlphaMode   AlphaMode
	DoubleSided bool
	// FrontCW is set for the copy a mirrored placement draws with. A flattened
	// matrix with a negative determinant reverses winding, and winding is the
	// material's pipeline state with no per-draw override, so such a primitive
	// needs its own copy of the material it shares.
	FrontCW bool
	// AlphaCutoff is the file's alphaCutoff for a MASK material and zero for
	// any other.
	AlphaCutoff       float32
	BaseColorFactor   m.Vec4
	MetallicFactor    float32
	RoughnessFactor   float32
	NormalScale       float32
	OcclusionStrength float32
	// EmissiveFactor has KHR_materials_emissive_strength folded into its rgb
	// and is clamped at 1. Its w is zero.
	EmissiveFactor m.Vec4
	Slots          [SlotCount]Slot
}

// Slot is one texture slot of a material.
type Slot struct {
	// Bound says the material names a texture here at all. A bound slot's
	// TexCoord was asked for, even when its image could not be named.
	Bound bool
	// Image indexes Model.Images, or is NoImage.
	Image   int
	Sampler gfx.SamplerDesc
	// TexCoord is the TEXCOORD set the slot samples, KHR_texture_transform's
	// override applied.
	TexCoord int
	// Transform is the slot's KHR_texture_transform as offset.xy, scale.xy,
	// and Rotation its rotation in radians: the identity when the file
	// declares none.
	Transform m.Vec4
	Rotation  float32
}

// Light is one KHR_lights_punctual light, placed in the model's own space by
// the node that carries it.
type Light struct {
	Name string
	// Directional and Spot say which of the three kinds it is; neither is a
	// point light.
	Directional, Spot bool
	Position          m.Vec3
	// Direction is where light travels: the carrying node's local -Z.
	Direction            m.Vec3
	Color                m.Color
	Intensity            float32
	Range                float32
	InnerCone, OuterCone float32
}

// defaultMaterial is the material index of a primitive that names none, which
// glTF defines as the fully-metallic white default.
const defaultMaterial = -1

// geometryKey interns one decoded primitive. Tangent generation is part of the
// key because it depends on the material a node drew the mesh with: the same
// mesh under a normal-mapped material and a plain one is two geometries, which
// is rare and correct.
type geometryKey struct {
	mesh, primitive int
	tangents        bool
	// skin is part of the key because remapping JOINTS_0 into the model's one
	// numbering rewrites the vertices. The same mesh under two skins is two
	// geometries, which is rare and correct.
	//
	// The node joint is not: it rides the placement, so a mesh under nine
	// animated nodes is one geometry and nine placements.
	skin int
}

// materialVariant keys the material table of one decode.
type materialVariant struct {
	material int
	frontCW  bool
}

// decoder turns one gltf.Document into a Model in a single pass and then drops
// the document.
type decoder struct {
	doc    *gltf.Document
	path   string
	images *imageRequests
	model  Model
	// geometries interns the decoded primitives, keyed by where they came
	// from rather than by their bytes: hashing a megabyte of vertices to find a
	// duplicate the file already told us about would be work for nothing.
	geometries map[geometryKey]int
	// variants interns the (material, winding) pairs the flattening asked for,
	// so a model whose every node has a positive determinant - which is most of
	// them - carries exactly one material per glTF material.
	variants map[materialVariant]int
	// visited guards against a cyclic node graph, which is malformed but is a
	// stack overflow rather than a report if nothing checks. It is cleared
	// between scenes rather than kept for the file: a node two scenes both root
	// belongs to both, and a file-wide guard would leave the second empty.
	visited map[int]bool
	// scene is the entry the walk is filling, and points into model.Scenes.
	scene *Scene
	// animated is the set of nodes some animation steers, and chain the
	// animated ancestors of the node the walk is inside, root-first.
	// chainWorlds holds those ancestors' authored world matrices, so a node
	// claiming its name can record the deepest one without a second walk.
	animated    map[int]bool
	chain       []int
	chainWorlds []m.Mat4
	joints      jointSpace
	// curves interns the animation samplers the clips decode, so a clip whose
	// twenty bones share one input accessor reads it once. weightCurves is the
	// same interning for the weights channels.
	curves       map[samplerKey]*Curve
	weightCurves map[samplerKey]*WeightCurve
	// morphNodes is each node's run of the model's flattened weight slots,
	// claimed in the flattening walk's depth-first order.
	morphNodes map[int]morphSlotRun
	// duplicated keeps a repeated node name to one report however many scenes
	// and however many nodes carry it.
	duplicated map[string]bool
	// boundsReported keeps the missing-bounds report to one per model however
	// many primitives declared no min/max.
	boundsReported bool
}

// Decode parses one glTF or GLB file's bytes and decodes it. modelPath is the
// file's storage path: a .gltf file's relative buffer URIs resolve against its
// directory in fsys, external images are named against it, and every report
// names the model by it. Images are named and never opened, so fsys is read
// only for buffers.
func Decode(data []byte, modelPath string, fsys fs.FS) (*Model, error) {
	parser := gltf.NewDecoderFS(bytes.NewReader(data), directoryFS(fsys, path.Dir(modelPath)))
	document := new(gltf.Document)
	if err := parser.Decode(document); err != nil {
		return nil, err
	}
	return DecodeDocument(document, modelPath)
}

// directoryFS presents one directory of the filesystem as its own root, which
// is the shape the glTF parser wants for relative URIs. A directory that
// cannot be subsetted - "." at the root - is the filesystem itself.
func directoryFS(fsys fs.FS, dir string) fs.FS {
	if dir == "" || dir == "." {
		return fsys
	}
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		return fsys
	}
	return sub
}

// DecodeDocument decodes one parsed document. It opens nothing: an external
// image is named here and read by whoever loads it, which is why this half
// needs no filesystem at all.
func DecodeDocument(doc *gltf.Document, modelPath string) (*Model, error) {
	if err := checkRequiredExtensions(doc); err != nil {
		return nil, err
	}
	if len(doc.Scenes) == 0 {
		return nil, errors.New("it has no scenes")
	}
	d := &decoder{
		doc:          doc,
		path:         modelPath,
		images:       newImageRequests(doc, modelPath),
		geometries:   map[geometryKey]int{},
		variants:     map[materialVariant]int{},
		visited:      map[int]bool{},
		animated:     animatedNodes(doc),
		duplicated:   map[string]bool{},
		curves:       map[samplerKey]*Curve{},
		weightCurves: map[samplerKey]*WeightCurve{},
		morphNodes:   map[int]morphSlotRun{},
	}
	// The skins' joints are claimed before the walk starts, so the joints a
	// vertex can name are the lowest of the model's numbering.
	d.buildJointSpace()
	d.decodeNodes()
	d.model.DefaultScene = defaultSceneIndex(doc)
	d.model.Scenes = make([]Scene, 0, len(doc.Scenes))
	for _, scene := range doc.Scenes {
		d.flattenScene(scene)
	}
	// Both run last because the walk is what claims a degenerate node's joint
	// and a morphed node's weight slots, so neither numbering is complete
	// until every scene is flattened.
	d.claimRerootJoints()
	d.decodeClips()
	d.model.Joints = d.joints.joints
	d.model.Images = d.images.images
	d.model.Reports = append(d.model.Reports, d.images.reports...)
	return &d.model, nil
}

// checkRequiredExtensions fails a model whose file says it cannot be drawn
// without something the decoder does not implement.
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
func (d *decoder) flattenScene(scene *gltf.Scene) {
	if scene == nil {
		d.model.Scenes = append(d.model.Scenes, Scene{})
		return
	}
	start := len(d.model.Primitives)
	d.model.Scenes = append(d.model.Scenes, Scene{
		Name: scene.Name, Start: start, Nodes: map[string]SceneNode{},
	})
	d.scene = &d.model.Scenes[len(d.model.Scenes)-1]
	clear(d.visited)
	d.chain = d.chain[:0]
	for _, root := range scene.Nodes {
		d.walkNode(root, m.NewMat4())
	}
	d.scene.End = len(d.model.Primitives)
	d.scene = nil
}

// walkNode flattens one node and its subtree depth-first, accumulating the
// matrix that places it relative to the scene root.
//
// Depth-first is what makes a subtree a contiguous slice of the result rather
// than a filter over it, which is the whole mechanism the Node selector uses:
// a node's primitives and all its descendants' are emitted before anything
// outside the subtree.
func (d *decoder) walkNode(index int, parent m.Mat4) {
	if index < 0 || index >= len(d.doc.Nodes) || d.doc.Nodes[index] == nil || d.visited[index] {
		return
	}
	d.visited[index] = true
	node := d.doc.Nodes[index]
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
	// rest is where those primitives sit in the model's rest pose. For a plain
	// joint that is the node's own world transform, which the rest row of the
	// pose buffer resolves to; for a glTF skin it is the identity, because the
	// skin's joints already resolve against the scene root and its vertices are
	// authored there.
	placement, rest := world, world
	binding := nodeBinding{skin: -1, joint: -1}
	switch {
	case node.Skin != nil:
		placement, rest = m.NewMat4(), m.NewMat4()
		binding.skin = *node.Skin
	case node.Mesh != nil && (d.animated[index] || len(d.chain) > 0):
		placement = m.NewMat4()
		binding.joint = d.joints.claimPlain(index)
	}
	// The name is claimed before the subtree is walked and closed after, so a
	// name a node shares with one of its own descendants resolves to the node -
	// which is what "first depth-first match" says, and what recording the
	// subtree on the way back out would get backwards.
	named := d.claimNode(node.Name, world)
	if node.Mesh != nil {
		d.flattenMesh(*node.Mesh, placement, rest, binding, d.claimMorphSlots(index))
	}
	d.collectLight(node, world)
	animated := d.animated[index]
	if animated {
		d.chain = append(d.chain, index)
		d.chainWorlds = append(d.chainWorlds, world)
	}
	for _, child := range node.Children {
		d.walkNode(child, world)
	}
	if animated {
		d.chain = d.chain[:len(d.chain)-1]
		d.chainWorlds = d.chainWorlds[:len(d.chainWorlds)-1]
	}
	if named {
		d.closeNode(node.Name)
	}
}

// claimNode records one named node's slice, opened at the walk's position, and
// reports whether this node is the one that owns the name. A duplicate keeps
// the first match and says so once.
func (d *decoder) claimNode(name string, world m.Mat4) bool {
	if name == "" || d.scene == nil {
		return false
	}
	if _, taken := d.scene.Nodes[name]; taken {
		if !d.duplicated[name] {
			d.duplicated[name] = true
			d.model.Reports = append(d.model.Reports,
				ErrModelNodeDuplicated{Model: d.path, Node: name})
		}
		return false
	}
	reroot, rerootable := world.InverseAffine()
	node := SceneNode{
		Start: len(d.model.Primitives), Reroot: reroot, Rerootable: rerootable,
		RerootJoint: -1, First: len(d.scene.Order),
	}
	d.scene.Order = append(d.scene.Order, name)
	if len(d.chain) > 0 {
		node.Animated = append([]int(nil), d.chain...)
		node.Rest = d.chainWorlds[len(d.chainWorlds)-1]
	}
	d.scene.Nodes[name] = node
	return true
}

// closeNode ends a claimed node's slice at the walk's position, which
// depth-first order has just made the end of its whole subtree.
func (d *decoder) closeNode(name string) {
	node := d.scene.Nodes[name]
	node.End = len(d.model.Primitives)
	node.Last = len(d.scene.Order)
	d.scene.Nodes[name] = node
}

// nodeBinding is how one node binds its mesh to the model's joints, and it is
// two answers rather than one because the two live in different places.
type nodeBinding struct {
	// skin is the glTF skin whose JOINTS_0 is remapped into the model's one
	// numbering, or -1. Remapping rewrites the vertices, so this belongs to the
	// geometry and is part of its key.
	skin int
	// joint is the model joint the node's own transform lives in, or -1. It
	// overwrites nothing: the placement names the bone and every vertex under
	// it rides at full weight, so this belongs to the placement and is not
	// part of any key.
	joint int
}

// flattenMesh emits one node's primitives at their flattened placement.
func (d *decoder) flattenMesh(
	index int, placement, rest m.Mat4, binding nodeBinding, slots morphSlotRun,
) {
	if index < 0 || index >= len(d.doc.Meshes) || d.doc.Meshes[index] == nil {
		return
	}
	// A negative determinant mirrors the geometry, which reverses triangle
	// winding. Pipeline state is per material and the draw gets no say, so the
	// winding has to be answered with a material variant rather than a flag on
	// the instance.
	frontCW := placement.Determinant() < 0
	for at, primitive := range d.doc.Meshes[index].Primitives {
		material := d.material(primitive.Material, frontCW)
		tangents := d.model.Materials[material].Slots[SlotNormal].Image != NoImage
		geometry, ok := d.geometry(index, at, primitive, tangents, binding.skin)
		if !ok {
			continue
		}
		decoded := &d.model.Geometries[geometry]
		placed := Primitive{
			Geometry: geometry, Local: placement, Rest: rest, Material: material,
			Skinned: decoded.Skinned,
		}
		if decoded.Morphed() {
			// A primitive carrying more targets than its node claimed slots for
			// is a malformed mesh - glTF requires every primitive of a mesh to
			// declare the same targets - so the extra targets simply have no
			// weight to read.
			placed.MorphSlotBase, placed.MorphTargets = slots.base, min(len(decoded.Targets), slots.count)
		}
		if binding.joint >= 0 {
			placed.Joint, placed.Plain, placed.Skinned = uint32(binding.joint), true, true
		}
		// The layout is the union of that same answer over every placement, and
		// it is taken here so that the two can never be derived from different
		// facts: a placement that draws through the pose buffer is a placement
		// whose geometry supplies the joints and the weights that needs. A
		// plain-bound placement widens a geometry no skin ever touched, which is
		// the tax this union charges and the reason it is a union at all.
		decoded.SkinnedLayout = decoded.SkinnedLayout || placed.Skinned
		d.model.Primitives = append(d.model.Primitives, placed)
	}
}

// geometry decodes one primitive, or returns the decode an earlier node
// referencing the same mesh already paid for.
func (d *decoder) geometry(
	mesh, at int, primitive *gltf.Primitive, tangents bool, skin int,
) (int, bool) {
	key := geometryKey{mesh: mesh, primitive: at, tangents: tangents, skin: skin}
	if index, ok := d.geometries[key]; ok {
		return index, index >= 0
	}
	geometry, err := readGeometry(d.doc, primitive, tangents)
	if err != nil {
		// The failure is interned too, so a mesh referenced by ten nodes
		// reports its one bad primitive once rather than ten times.
		d.geometries[key] = -1
		d.model.Reports = append(d.model.Reports, ErrModelPrimitiveSkipped{
			Model: d.path, Mesh: d.doc.Meshes[mesh].Name, Err: err,
		})
		return -1, false
	}
	// A skin's JOINTS_0 indexes that skin's own joints array. glTF requires a
	// skinned node's mesh to carry JOINTS_0 and WEIGHTS_0, and one whose every
	// vertex carries no weight draws unskinned at the skin's root rather than
	// being lost.
	if skin >= 0 && skin < len(d.doc.Skins) {
		geometry.Skin = skin
		geometry.Skinned = weighted(geometry.Weights, len(geometry.Positions))
	}
	if !geometry.HasBox && !d.boundsReported {
		d.boundsReported, d.model.NeverCull = true, true
		d.model.Reports = append(d.model.Reports, ErrModelBoundsMissing{Model: d.path})
	}
	d.geometries[key] = len(d.model.Geometries)
	d.model.Geometries = append(d.model.Geometries, geometry)
	return len(d.model.Geometries) - 1, true
}

// collectLight records a node's KHR_lights_punctual light in the model's own
// space. Nothing converts it: a light is data an app declares, at whatever
// world transform it drew the model at.
func (d *decoder) collectLight(node *gltf.Node, world m.Mat4) {
	index, ok := node.Extensions[lightspunctual.ExtensionName].(lightspunctual.LightIndex)
	if !ok {
		return
	}
	lights, ok := d.doc.Extensions[lightspunctual.ExtensionName].(lightspunctual.Lights)
	if !ok || int(index) < 0 || int(index) >= len(lights) || lights[index] == nil {
		return
	}
	light := lights[index]
	colour := light.ColorOrDefault()
	decoded := Light{
		Name:     light.Name,
		Position: world.Translation(),
		// glTF punctual lights point down their node's local -Z: the
		// direction light travels.
		Direction: world.TransformDirection(m.Vec3{Z: -1}).Normalize(),
		Color:     m.NewColorLinear(float32(colour[0]), float32(colour[1]), float32(colour[2]), 1),
		Intensity: float32(light.IntensityOrDefault()),
	}
	if light.Range != nil {
		decoded.Range = float32(*light.Range)
	}
	switch light.Type {
	case lightspunctual.TypeDirectional:
		decoded.Directional = true
	case lightspunctual.TypeSpot:
		decoded.Spot = true
		if light.Spot != nil {
			decoded.InnerCone = float32(light.Spot.InnerConeAngle)
			decoded.OuterCone = float32(light.Spot.OuterConeAngleOrDefault())
		}
	}
	d.model.Lights = append(d.model.Lights, decoded)
}

// material returns the index of the decoded material one primitive draws with,
// decoding it the first time this decode asks for that winding.
func (d *decoder) material(index *int, frontCW bool) int {
	source := defaultMaterial
	if index != nil {
		source = *index
	}
	key := materialVariant{material: source, frontCW: frontCW}
	if existing, ok := d.variants[key]; ok {
		return existing
	}
	decoded := d.decodeMaterial(source, frontCW)
	d.variants[key] = len(d.model.Materials)
	d.model.Materials = append(d.model.Materials, decoded)
	return len(d.model.Materials) - 1
}

// defaultMaterialValues is glTF's own default material: white, fully metallic,
// fully rough, with every texture slot empty and untransformed.
func defaultMaterialValues() Material {
	material := Material{
		BaseColorFactor:   m.Vec4{X: 1, Y: 1, Z: 1, W: 1},
		MetallicFactor:    1,
		RoughnessFactor:   1,
		NormalScale:       1,
		OcclusionStrength: 1,
	}
	for slot := range material.Slots {
		material.Slots[slot] = Slot{Image: NoImage, Sampler: DefaultSampler, Transform: m.Vec4{Z: 1, W: 1}}
	}
	return material
}

// decodeMaterial reads one glTF material's values and texture bindings.
//
// The numbers are glTF's by verbatim name, which is what makes the glTF
// specification the parameter documentation.
func (d *decoder) decodeMaterial(index int, frontCW bool) Material {
	decoded := defaultMaterialValues()
	decoded.FrontCW = frontCW
	if index < 0 || index >= len(d.doc.Materials) || d.doc.Materials[index] == nil {
		return decoded
	}
	material := d.doc.Materials[index]
	decoded.AlphaMode, decoded.DoubleSided = alphaModeOf(material.AlphaMode), material.DoubleSided
	if material.AlphaMode == gltf.AlphaMask {
		decoded.AlphaCutoff = float32(material.AlphaCutoffOrDefault())
	}
	if pbr := material.PBRMetallicRoughness; pbr != nil {
		factor := pbr.BaseColorFactorOrDefault()
		decoded.BaseColorFactor = m.Vec4{
			X: float32(factor[0]), Y: float32(factor[1]),
			Z: float32(factor[2]), W: float32(factor[3]),
		}
		decoded.MetallicFactor = float32(pbr.MetallicFactorOrDefault())
		decoded.RoughnessFactor = float32(pbr.RoughnessFactorOrDefault())
		d.bindSlot(&decoded, SlotBaseColor, pbr.BaseColorTexture, true)
		d.bindSlot(&decoded, SlotMetallicRoughness, pbr.MetallicRoughnessTexture, false)
	}
	if material.NormalTexture != nil && material.NormalTexture.Index != nil {
		decoded.NormalScale = float32(material.NormalTexture.ScaleOrDefault())
		d.bindSlot(&decoded, SlotNormal, &gltf.TextureInfo{
			Index:      *material.NormalTexture.Index,
			TexCoord:   material.NormalTexture.TexCoord,
			Extensions: material.NormalTexture.Extensions,
		}, false)
	}
	if material.OcclusionTexture != nil && material.OcclusionTexture.Index != nil {
		decoded.OcclusionStrength = float32(material.OcclusionTexture.StrengthOrDefault())
		d.bindSlot(&decoded, SlotOcclusion, &gltf.TextureInfo{
			Index:      *material.OcclusionTexture.Index,
			TexCoord:   material.OcclusionTexture.TexCoord,
			Extensions: material.OcclusionTexture.Extensions,
		}, false)
	}
	d.bindSlot(&decoded, SlotEmissive, material.EmissiveTexture, true)
	decoded.EmissiveFactor = emissiveFactor(material)
	return decoded
}

// bindSlot fills one texture slot: the image, its sampler, its UV set and its
// KHR_texture_transform. An absent slot keeps no image and the identity
// transform.
func (d *decoder) bindSlot(decoded *Material, slot int, info *gltf.TextureInfo, srgb bool) {
	if info == nil {
		return
	}
	bound := &decoded.Slots[slot]
	bound.Bound = true
	bound.Image, bound.Sampler = d.images.texture(info.Index, srgb)
	bound.TexCoord = info.TexCoord
	if transform, ok := info.Extensions[texturetransform.ExtensionName].(*texturetransform.TextureTranform); ok {
		scale := transform.ScaleOrDefault()
		bound.Transform = m.Vec4{
			X: float32(transform.Offset[0]), Y: float32(transform.Offset[1]),
			Z: float32(scale[0]), W: float32(scale[1]),
		}
		bound.Rotation = float32(transform.Rotation)
		if transform.TexCoord != nil {
			bound.TexCoord = *transform.TexCoord
		}
	}
}

// emissiveFactor folds KHR_materials_emissive_strength into emissiveFactor and
// clamps the product at 1.
//
// Folding at load is what keeps the numbers glTF's own: a separate strength
// member would be a sixth factor the shader multiplies and an override would
// have to know about. The clamp is the honest limit of an 8-bit sRGB target
// with no tonemapping and no exposure control - a strength of 8 has nowhere to
// go but white, and clipping it here at least keeps the hue.
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

// alphaModeOf maps glTF's alphaMode onto the decoder's.
func alphaModeOf(mode gltf.AlphaMode) AlphaMode {
	switch mode {
	case gltf.AlphaMask:
		return AlphaMask
	case gltf.AlphaBlend:
		return AlphaBlend
	}
	return AlphaOpaque
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
