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
	// neverCull is set by a primitive whose POSITION accessor declared no
	// min/max. It is per model rather than per primitive because a model with
	// one unbounded primitive has no bound at all - culling the rest of it
	// would leave the unbounded piece drawn alone.
	neverCull bool
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
	material int
}

// geometryKey interns one converted primitive. Tangent generation is part of
// the key because it depends on the material a node drew the mesh with: the
// same mesh under a normal-mapped material and a plain one is two conversions,
// which is rare and correct.
type geometryKey struct {
	mesh, primitive int
	tangents        bool
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
	// stack overflow rather than a report if nothing checks.
	visited map[int]bool
	// boundsReported keeps the missing-bounds report to one per model however
	// many primitives declared no min/max.
	boundsReported bool
}

// convertDocument converts one parsed document. filesystem resolves external
// image URIs and may be nil, in which case a file naming one loses that texture
// to the 1x1 default and says so.
func convertDocument(doc *gltf.Document, path string, filesystem fs.FS) (*loadedModel, error) {
	if err := checkRequiredExtensions(doc); err != nil {
		return nil, err
	}
	converter := &modelConverter{
		doc:        doc,
		path:       path,
		textures:   newTextureLoader(doc, filesystem, path),
		geometries: map[geometryKey]int{},
		variants:   map[materialVariant]int{},
		visited:    map[int]bool{},
	}
	roots, err := defaultSceneRoots(doc)
	if err != nil {
		return nil, err
	}
	for _, root := range roots {
		converter.walkNode(root, m.NewMat4())
	}
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

// defaultSceneRoots picks the scene a draw with no selector renders: the file's
// declared default, or the first one. The Scene selector itself lands with the
// selectors ticket; the default is what every draw resolves to without one.
func defaultSceneRoots(doc *gltf.Document) ([]int, error) {
	if len(doc.Scenes) == 0 {
		return nil, errors.New("it has no scenes")
	}
	index := 0
	if doc.Scene != nil && *doc.Scene >= 0 && *doc.Scene < len(doc.Scenes) {
		index = *doc.Scene
	}
	return doc.Scenes[index].Nodes, nil
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
	placement := world
	if node.Skin != nil {
		placement = m.NewMat4()
	}
	if node.Mesh != nil {
		c.flattenMesh(*node.Mesh, placement)
	}
	c.collectLight(node, world)
	for _, child := range node.Children {
		c.walkNode(child, world)
	}
}

// flattenMesh emits one node's primitives at their flattened placement.
func (c *modelConverter) flattenMesh(index int, placement m.Mat4) {
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
		geometry, ok := c.geometry(index, at, primitive, tangents)
		if !ok {
			continue
		}
		c.model.primitives = append(c.model.primitives, loadedPrimitive{
			geometry: geometry, local: placement, material: material,
		})
	}
}

// geometry converts one primitive, or returns the conversion an earlier node
// referencing the same mesh already paid for.
func (c *modelConverter) geometry(
	mesh, at int, primitive *gltf.Primitive, tangents bool,
) (int, bool) {
	key := geometryKey{mesh: mesh, primitive: at, tangents: tangents}
	if index, ok := c.geometries[key]; ok {
		return index, index >= 0
	}
	geometry, err := convertPrimitive(c.doc, primitive, tangents)
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
