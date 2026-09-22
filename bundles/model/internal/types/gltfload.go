package types

import (
	"fmt"

	"github.com/dvoyni/cog/libs/assets"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
	"github.com/qmuntal/gltf"
)

// LoadedModel is one file converted to scene's own types, CPU-side, with no
// GPU handle anywhere in it. It is what crosses from the parse half of a load
// to the upload half.
//
// model's decoder produces the plain data and this conversion lays it out for
// the GPU: it fills the conversion vertices the pack reads, generates flat
// normals and tangents, remaps joints, packs the morph blocks, bakes the clips
// onto the pose grid and fills the PBR records. None of that is the decoder's.
type LoadedModel struct {
	// geometries are the converted primitives, one per distinct glTF primitive
	// rather than one per node that references it. Two wheel nodes sharing a
	// wheel mesh are one upload and two placements, which is how glTF authors
	// repeated parts and what a per-node conversion would silently double.
	geometries []gltfGeometry
	primitives []loadedPrimitive
	materials  []loadedMaterial
	textures   []textureDescr
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
	// block concatenated and reached by a base offset. One buffer per model
	// rather than per primitive: a buffer per primitive would mean a bind
	// group per primitive, collapsing group 2's whole reason for existing.
	//
	// It is a raw word array rather than a slice of records: a block holds its
	// own ranges and per-target headers ahead of its records, and a record is
	// no longer a whole number of vec4s.
	morphDeltas []uint32
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
	// geometry indexes LoadedModel.geometries, so a mesh referenced by several
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
	morph MorphBinding
}

// loadedScene is one entry of the file's scenes array, flattened by the
// decoder: the range of primitives its walk produced and the nodes within it a
// Node selector can address. It holds no GPU handle, so it is taken whole.
type loadedScene = DecodedScene

// loadedMaterial is one glTF material converted to the bundled PBR: the record
// scene binds per batch, the pipeline state it draws under, and the texture and
// sampler each of the five slots binds.
type loadedMaterial struct {
	record ScenePbrRecord
	state  gfx.MaterialState
	// slots index LoadedModel.textures, or missingTexture for a slot the file
	// left empty or named no readable image for. Both fall back to the slot's
	// own 1x1 default, which is what makes a partial failure a resident model.
	slots    [pbrSlotCount]int
	samplers [pbrSlotCount]gfx.SamplerDesc
}

// missingTexture is the slot value for a texture the file itself could not
// name - no texture at that index, no image source, an image the container
// holds no bytes for. The model still becomes resident and the slot binds its
// own per-slot default, because a model missing one texture is a model you can
// see and fix; a model that failed wholesale over a missing picture is a level
// with a hole in it.
//
// A picture that was named and did not arrive is a different thing and does not
// come through here: it has a cache entry, and what it binds is that entry's
// placeholder.
const missingTexture = -1

// The five slot indices, in the record order PbrSlots fixes. NormalSlot is
// already named there, because it is the one slot whose 1x1 default is the flat
// normal rather than the white texel.
const (
	baseColorSlot         = 0
	metallicRoughnessSlot = 1
	occlusionSlot         = 3
	emissiveSlot          = 4
)

// pbrSlotOf maps each of the decoder's material slots onto the record's.
var pbrSlotOf = [DecodedSlotCount]int{
	DecodedSlotBaseColor:         baseColorSlot,
	DecodedSlotMetallicRoughness: metallicRoughnessSlot,
	DecodedSlotNormal:            NormalSlot,
	DecodedSlotOcclusion:         occlusionSlot,
	DecodedSlotEmissive:          emissiveSlot,
}

// modelConverter lays one decoded model out for the GPU. The decoded model is
// dropped with it: nothing of it but the scene walk is held past the load.
type modelConverter struct {
	decoded *DecodedModel
	path    string
	model   LoadedModel
	// sampleRate is Config.PoseSampleRate, the global grid every clip bakes
	// onto. It reaches the conversion through the load request rather than
	// through a Lookup, because the conversion holds no resource at all.
	sampleRate int
	// walked, locals and worlds are the pose walk's scratch. All three keep
	// their backing across the whole bake: the walk runs once per sampled
	// frame and must allocate nothing.
	walked []bool
	locals []m.Mat4
	worlds []m.Mat4
	// poseReported keeps the unrepresentable-pose report to one per model
	// however many joints and frames carry shear.
	poseReported bool
}

// convertDocument decodes and converts one parsed document. It opens nothing:
// an external image is named here and read by the Library when the texture
// cache misses it, which is why this half of the load needs no filesystem at
// all.
func convertDocument(doc *gltf.Document, path string, sampleRate int) (*LoadedModel, error) {
	decoded, err := DecodeDocument(doc, path)
	if err != nil {
		return nil, err
	}
	return convertDecoded(decoded, path, sampleRate)
}

// convertDecoded converts one decoded model.
func convertDecoded(decoded *DecodedModel, path string, sampleRate int) (*LoadedModel, error) {
	if err := checkJointCap(decoded.Skins); err != nil {
		return nil, err
	}
	c := &modelConverter{decoded: decoded, path: path, sampleRate: sampleRate}
	c.model.reports = decoded.Reports
	c.model.geometries = make([]gltfGeometry, len(decoded.Geometries))
	for i := range decoded.Geometries {
		c.model.geometries[i] = convertGeometry(&decoded.Geometries[i], decoded.Skins)
		// The decoded arrays are dropped as each geometry is converted, so a
		// large model holds both forms of one primitive at a time rather than
		// of all of them.
		decoded.Geometries[i] = DecodedGeometry{}
	}
	c.model.primitives = make([]loadedPrimitive, len(decoded.Primitives))
	for i := range decoded.Primitives {
		c.model.primitives[i] = c.convertPrimitive(&decoded.Primitives[i])
	}
	c.model.materials = make([]loadedMaterial, len(decoded.Materials))
	for i := range decoded.Materials {
		c.model.materials[i] = c.convertMaterial(&decoded.Materials[i])
	}
	c.model.textures = make([]textureDescr, len(decoded.Images))
	for i := range decoded.Images {
		c.model.textures[i] = textureDescrOf(&decoded.Images[i])
	}
	for i := range decoded.Lights {
		c.model.lights = append(c.model.lights, modelLightOf(&decoded.Lights[i]))
	}
	c.model.scenes = decoded.Scenes
	c.model.defaultScene = decoded.DefaultScene
	c.model.neverCull = decoded.NeverCull
	c.packMorphDeltas()
	c.bakeAnimation()
	return &c.model, nil
}

// checkJointCap fails a model whose skins could make a vertex name a joint past
// the byte the storage vertex holds it in.
//
// The decoder claims every skin's joints first, in one contiguous block at the
// bottom of the model's numbering, so the joints a vertex can name are exactly
// the ones this counts. A plain joint rides the instance record in a full u32
// and is bounded by node count rather than by anything, so however many a file
// has, none of them can push a vertex's index over.
//
// Failing the whole model is the point. A joint index that did not fit would
// truncate to a different bone, and a prop welded to the wrong limb with
// nothing reported is exactly the failure a cap exists to prevent - so the
// report is the loudest one a load has, which fails the model and names it.
func checkJointCap(skins []DecodedSkin) error {
	claimed := 0
	for _, skin := range skins {
		if len(skin.Joints) > sceneMaxSkinJoints {
			return fmt.Errorf(
				"its skin %q has %d joints, and a vertex names one in a byte, so %d is the most a skin may have",
				skin.Name, len(skin.Joints), sceneMaxSkinJoints)
		}
		for _, joint := range skin.Joints {
			claimed = max(claimed, joint+1)
		}
		// Skins share a numbering, because one numbering per model is what
		// lets a pose row be addressed with no per-skin offset. So two skins
		// inside the cap can still put a remapped index past it between them,
		// and the check that actually guards the byte is this one - the
		// per-skin test above is what names the skin when one skin alone is
		// the reason.
		if claimed > sceneMaxSkinJoints {
			return fmt.Errorf(
				"its skins claim %d joints between them, and a vertex names one in a byte, so %d is the most a model may bind",
				claimed, sceneMaxSkinJoints)
		}
	}
	return nil
}

// convertPrimitive places one decoded primitive, binding its morph targets to
// the record stride its geometry packs at.
func (c *modelConverter) convertPrimitive(decoded *DecodedPrimitive) loadedPrimitive {
	placed := loadedPrimitive{
		geometry: decoded.Geometry, local: decoded.Local, rest: decoded.Rest,
		material: decoded.Material, skinned: decoded.Skinned,
		joint: decoded.Joint, plain: decoded.Plain,
	}
	if morph := c.model.geometries[decoded.Geometry].morph.binding(); morph.Morphed() {
		morph.SlotBase, morph.Targets = decoded.MorphSlotBase, decoded.MorphTargets
		placed.morph = morph
	}
	return placed
}

// convertMaterial fills one decoded material's bundled-PBR record and pipeline
// state.
//
// The record's numbers are glTF's by verbatim name, which is what makes the
// glTF specification the parameter documentation and what lets OverrideParams
// merge by name with no translation table to drift out of date.
func (c *modelConverter) convertMaterial(decoded *DecodedMaterial) loadedMaterial {
	converted := loadedMaterial{
		record: DefaultPbrRecord(),
		state:  PbrState(alphaModeOf(decoded.AlphaMode), decoded.DoubleSided),
	}
	if decoded.FrontCW {
		converted.state.FrontFace = gfx.FrontCW
	}
	record := &converted.record
	record.AlphaCutoff = decoded.AlphaCutoff
	record.BaseColorFactor = decoded.BaseColorFactor
	record.MetallicFactor = decoded.MetallicFactor
	record.RoughnessFactor = decoded.RoughnessFactor
	record.NormalScale = decoded.NormalScale
	record.OcclusionStrength = decoded.OcclusionStrength
	record.EmissiveFactor = decoded.EmissiveFactor
	for from, slot := range pbrSlotOf {
		bound := &decoded.Slots[from]
		converted.slots[slot], converted.samplers[slot] = missingTexture, bound.Sampler
		if bound.Image != DecodedNoImage {
			converted.slots[slot] = bound.Image
		}
		record.Transforms[slot], record.Rotations[slot] = bound.Transform, bound.Rotation
		// An absent slot keeps the 1x1 default and the identity transform, so
		// the shader's unconditional five samples cost the same either way. A
		// bound one selects its UV set, which reports a set past the two scene
		// carries.
		if bound.Bound {
			record.selectUVSet(func(err error) {
				c.model.reports = append(c.model.reports, err)
			}, slot, bound.TexCoord)
		}
	}
	return converted
}

// alphaModeOf maps the decoder's alphaMode onto scene's.
func alphaModeOf(mode DecodedAlphaMode) AlphaMode {
	switch mode {
	case DecodedAlphaMask:
		return AlphaMask
	case DecodedAlphaBlend:
		return AlphaBlend
	}
	return AlphaOpaque
}

// textureDescrOf keys one decoded image in the texture cache: an external
// image by its storage path alone, an embedded one by the model's path and its
// index, with its bytes supplied beside the name so the Library never opens the
// container to look for them.
func textureDescrOf(image *DecodedImage) textureDescr {
	if !image.Embedded {
		return textureDescr{
			Name: image.Path, Params: textureDescrParams{image: externalImage, srgb: image.SRGB},
		}
	}
	return textureDescr{
		Name:   image.Path,
		Params: textureDescrParams{image: image.Index, srgb: image.SRGB},
		Blob:   assets.NewBlob(image.Bytes),
	}
}

// modelLightOf turns one decoded punctual light into the descriptor scene's
// own recording calls take.
func modelLightOf(light *DecodedLight) ModelLight {
	descr := LightDescr{
		Position:  light.Position,
		Direction: light.Direction,
		Color:     light.Color,
		Intensity: light.Intensity,
		Range:     light.Range,
	}
	if light.Spot {
		descr.Kind = LightSpot
		descr.InnerCone, descr.OuterCone = light.InnerCone, light.OuterCone
	}
	return ModelLight{Name: light.Name, Directional: light.Directional, Descr: descr}
}
