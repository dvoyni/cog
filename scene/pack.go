package scene

import (
	"unsafe"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/m"
)

// sceneInstance is the per-instance record every scene draw reads through
// sceneInstances, and it is 64 bytes on purpose.
//
// It carries no normal matrix. Under non-uniform scale a normal transformed by
// world is wrong, and scene generates that case itself the moment a debug line
// becomes a stretched box — but a second 3 x vec4 would take the record to 128
// bytes and charge every line for a case it does not have. So the packer sets
// SCENE_NONUNIFORM instead and the shader takes the inverse-transpose for those
// instances alone, branch-uniform across the whole instance.
//
// Field order and size must match SceneInstance in builtin/scene/scene.wgsl.
type sceneInstance struct {
	// World0..World2 are the rows of the 4x3 world matrix: row i of the packed
	// matrix, translation in w. Three rows rather than a mat4x4 because the
	// fourth row of an affine transform is known.
	World0, World1, World2 m.Vec4
	// AnimOffset indexes sceneAnim, or is sceneNoAnim when the draw animates
	// nothing, which is what skips the animation path entirely.
	AnimOffset uint32
	Flags      uint32
	// Spare is the record's eight unspent bytes, kept named so the next thing
	// that needs per-instance data can see what it is spending.
	Spare [2]uint32
}

// The instance flags. Both are decided here rather than when their consumers
// land, because the record's size and the null-skin bind group both follow from
// them.
const (
	// sceneNonUniform marks an instance whose packed matrix does not scale
	// uniformly, and is the shader's signal to take an inverse-transpose for
	// its normals.
	sceneNonUniform uint32 = 1 << 0
	// sceneNoSkin marks a draw with no skin of its own — every buffer-built
	// mesh and every debug shape. Riding the rest-frame path instead would be
	// correct, but it charges a procedural terrain mesh a per-vertex pose fetch
	// for a guaranteed identity.
	//
	// It is set on every draw scene can make today, because every draw is
	// buffer-built. The shared null-skin bind group it exists to select — one
	// identity pose row and one-element inverse-bind, normal-matrix and
	// morph-delta arrays — lands with the group 2 bindings themselves: a bind
	// group can only fill bindings that exist, and no shader declares them yet.
	sceneNoSkin uint32 = 1 << 1
)

// sceneNoAnim is the animOffset of an instance that animates nothing.
const sceneNoAnim uint32 = ^uint32(0)

// nonUniformTolerance is the relative spread between the packed matrix's three
// basis lengths that still counts as uniform. It is relative because the test
// has to hold for a millimetre-scale prop and a kilometre-scale terrain alike.
const nonUniformTolerance = 1e-6

// sceneFrameBlock is one pass's view of the world, bound once per pass through
// sceneFrame. It carries view and projection separately as well as their
// product because a shader that needs view-space depth cannot recover them from
// the product.
//
// Field order and size must match SceneFrame in builtin/scene/scene.wgsl.
type sceneFrameBlock struct {
	View           m.Mat4
	Projection     m.Mat4
	ViewProjection m.Mat4
	CameraPosition m.Vec4
	// SunDirection is the sun's direction of travel, normalised, and zero when
	// the camera declared no sun. SunColor, AmbientSky and AmbientGround are
	// linear radiance with their intensities already premultiplied: it removes
	// a per-fragment multiply and costs nothing. Their w is spare.
	SunDirection  m.Vec4
	SunColor      m.Vec4
	AmbientSky    m.Vec4
	AmbientGround m.Vec4
}

// scenePbrRecord is the bundled PBR's per-batch record, bound as a range of the
// frame's material arena. It pads to a 256 multiple because a storage binding's
// offset must, which is a pad rather than a cap: 160 bytes of content pad to
// 256 instead of being truncated.
//
// Its numbers are glTF's, by verbatim name, because they are user-facing:
// OverrideParams merges by name, the loader maps 1:1 with no translation table
// to drift, and the glTF specification becomes the parameter documentation —
// including the exact semantics of occlusionStrength and normalScale, which are
// easy to get subtly wrong from memory.
//
// Field order and size must match ScenePbrMaterial in
// builtin/scene/scene.wgsl. The shader declares the per-slot metadata as flat
// named members - baseColorTransform, baseColorRotation and their four
// siblings - rather than an array, because array members are not
// name-addressable and animating baseColorTransform per frame is UV scrolling.
// These arrays are the same bytes: the packer indexes by slot, and the name a
// caller overrides through is the shader's.
type scenePbrRecord struct {
	BaseColorFactor m.Vec4
	// EmissiveFactor is linear radiance added after shading. Its w is spare;
	// KHR_materials_emissive_strength folds into the rgb at load.
	EmissiveFactor m.Vec4
	// Transforms is each slot's KHR_texture_transform as offset.xy, scale.xy,
	// and Rotations its rotation in radians. The transform is applied
	// unconditionally, about 30 ALU across five slots.
	Transforms [pbrSlotCount]m.Vec4
	Rotations  [pbrSlotCount]float32

	MetallicFactor    float32
	RoughnessFactor   float32
	NormalScale       float32
	OcclusionStrength float32
	// AlphaCutoff is zero for an OPAQUE material, which makes the shader's
	// unconditional discard a no-op there: alpha is never below zero. A MASK
	// material sets its own, glTF's default being 0.5.
	AlphaCutoff float32
	// UVSets is the packed per-slot UV selector, one bit per slot: 0 is
	// TEXCOORD_0 and 1 is TEXCOORD_1. Two sets is glTF core's minimum and the
	// cap scene keeps.
	UVSets uint32
	// pad takes the record to a multiple of its 16-byte alignment, which WGSL
	// requires of the struct as a whole.
	pad uint32
}

// pbrSlotCount is the number of texture slots the bundled PBR has.
const pbrSlotCount = len(pbrSlots)

// defaultPbrRecord is glTF's own default material: white, fully metallic, fully
// rough, with every texture slot multiplying through unchanged.
func defaultPbrRecord() scenePbrRecord {
	record := scenePbrRecord{
		BaseColorFactor:   m.Vec4{X: 1, Y: 1, Z: 1, W: 1},
		MetallicFactor:    1,
		RoughnessFactor:   1,
		NormalScale:       1,
		OcclusionStrength: 1,
	}
	for slot := range record.Transforms {
		record.Transforms[slot] = m.Vec4{Z: 1, W: 1}
	}
	return record
}

// selectUVSet points one slot at a TEXCOORD set. A slot naming a set past the
// cap falls back to set 0 and is reported: ignoring texCoord: 1 would be a
// silent wrong-output failure on a core glTF feature, so the fallback says so
// out loud.
func (r *scenePbrRecord) selectUVSet(report func(error), slot, texCoord int) {
	if texCoord < 0 || texCoord >= pbrUVSetCount {
		report(ErrTextureUVSetUnsupported{Slot: pbrSlots[slot].texture, TexCoord: texCoord})
		texCoord = 0
	}
	r.UVSets &^= 1 << uint(slot)
	r.UVSets |= uint32(texCoord) << uint(slot)
}

// pbrUVSetCount is the UV-set cap: TEXCOORD_0 and TEXCOORD_1, glTF core's
// minimum. The selector is one bit per slot because of it.
const pbrUVSetCount = 2

// packFrameLighting writes one camera's sun and hemispheric ambient into its
// frame block. Both stay per-camera fields rather than entries in the light
// array: packing the sun as a directional entry would cost an explicit
// discriminator and waste position, range and cone on it, and hemispheric
// ambient is normal-dependent rather than a direction, so it could never join
// the loop anyway.
func packFrameLighting(block sceneFrameBlock, descr CameraDescr) sceneFrameBlock {
	if direction := descr.SunDirection.Normalize(); direction != (m.Vec3{}) {
		block.SunDirection = m.Vec4{X: direction.X, Y: direction.Y, Z: direction.Z}
		block.SunColor = radiance(descr.SunColor, descr.SunIntensity)
	}
	block.AmbientSky = radiance(descr.AmbientSky, descr.AmbientIntensity)
	block.AmbientGround = radiance(descr.AmbientGround, descr.AmbientIntensity)
	return block
}

// radiance premultiplies a linear colour by its intensity, where zero means 1.
// Intensity is unitless - radiance at one world unit - because targets are 8-bit
// sRGB with no tonemapping and no exposure control anywhere, so shading has to
// land in 0..1 directly.
func radiance(color m.Color, intensity float32) m.Vec4 {
	if intensity == 0 {
		intensity = 1
	}
	return m.Vec4{X: color.R * intensity, Y: color.G * intensity, Z: color.B * intensity}
}

// packInstance builds the instance record for one world matrix. Everything the
// record says about animation is the buffer-built answer — no anim, no skin —
// because that is every draw scene can make until models land.
func packInstance(world m.Mat4) sceneInstance {
	instance := sceneInstance{
		World0:     m.Vec4{X: world[0], Y: world[4], Z: world[8], W: world[12]},
		World1:     m.Vec4{X: world[1], Y: world[5], Z: world[9], W: world[13]},
		World2:     m.Vec4{X: world[2], Y: world[6], Z: world[10], W: world[14]},
		AnimOffset: sceneNoAnim,
		Flags:      sceneNoSkin,
	}
	if !uniformScale(world) {
		instance.Flags |= sceneNonUniform
	}
	return instance
}

// uniformScale reports whether a matrix scales its three basis vectors by the
// same factor. It compares squared lengths, so it costs three dot products and
// no square roots, and it is relative so that scale itself does not decide the
// answer.
func uniformScale(matrix m.Mat4) bool {
	lengths := [3]float32{
		matrix[0]*matrix[0] + matrix[1]*matrix[1] + matrix[2]*matrix[2],
		matrix[4]*matrix[4] + matrix[5]*matrix[5] + matrix[6]*matrix[6],
		matrix[8]*matrix[8] + matrix[9]*matrix[9] + matrix[10]*matrix[10],
	}
	low, high := lengths[0], lengths[0]
	for _, length := range lengths[1:] {
		low, high = min(low, length), max(high, length)
	}
	return high-low <= nonUniformTolerance*high
}

// arena is one frame's staging bytes for one storage binding. Records are
// appended into it and bound back out as ranges, which is how a draw addresses
// its own record without an index anyone has to agree on across the update and
// render threads.
//
// It keeps its backing across frames: a reset truncates, so a steady frame
// allocates nothing after the first.
type arena struct {
	data []byte
}

// reset empties the arena for a new frame without giving up its backing.
func (a *arena) reset() { a.data = a.data[:0] }

// bytes returns the arena's contents, valid until the next reset.
func (a *arena) bytes() []byte { return a.data }

// beginRange pads the arena up to a bindable offset and returns it. A storage
// binding's offset must be a multiple of gfx.StorageAlignment, so anything
// bound as its own range starts here.
func (a *arena) beginRange() int {
	if remainder := len(a.data) % gfx.StorageAlignment; remainder != 0 {
		a.data = append(a.data, make([]byte, gfx.StorageAlignment-remainder)...)
	}
	return len(a.data)
}

// appendRecord appends one bindable record and returns its offset.
func (a *arena) appendRecord[T any](record *T) int {
	offset := a.beginRange()
	a.data = append(a.data, recordBytes(record)...)
	return offset
}

// appendElement appends one element of an array binding, packed tight against
// the element before it: an array's elements are addressed by index inside one
// bound range, not bound separately.
func (a *arena) appendElement[T any](element *T) int {
	offset := len(a.data)
	a.data = append(a.data, recordBytes(element)...)
	return offset
}

// recordBytes reinterprets a record as the bytes uploaded for it. Every GPU
// target cog builds for is little-endian, so the in-memory layout is the wire
// layout — the same reinterpretation canvas's sprite instances use.
func recordBytes[T any](record *T) []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(record)), unsafe.Sizeof(*record))
}
