package internal

import (
	"unsafe"

	"github.com/dvoyni/cog/extensions/gfx"
	"github.com/dvoyni/cog/libs/m"
)

// SceneNoAnim is the animOffset of an instance that animates nothing.
const SceneNoAnim uint32 = ^uint32(0)

// ScenePbrRecord is the bundled PBR's per-batch record, bound as a range of the
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
type ScenePbrRecord struct {
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
const pbrSlotCount = len(PbrSlots)

// defaultPbrRecord is glTF's own default material: white, fully metallic, fully
// rough, with every texture slot multiplying through unchanged.
func defaultPbrRecord() ScenePbrRecord {
	record := ScenePbrRecord{
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
func (r *ScenePbrRecord) selectUVSet(report func(error), slot, texCoord int) {
	if texCoord < 0 || texCoord >= pbrUVSetCount {
		report(ErrTextureUVSetUnsupported{Slot: PbrSlots[slot].Texture, TexCoord: texCoord})
		texCoord = 0
	}
	r.UVSets &^= 1 << uint(slot)
	r.UVSets |= uint32(texCoord) << uint(slot)
}

// pbrUVSetCount is the UV-set cap: TEXCOORD_0 and TEXCOORD_1, glTF core's
// minimum. The selector is one bit per slot because of it.
const pbrUVSetCount = 2

// AnimBinding is everything one batch's instances say about animation: the
// group 2 buffers they read, the offset of their sceneAnim block, whether the
// placement is skinned at all, and the one joint it rides when it is
// plain-bound.
//
// It is per batch rather than per instance because a batch is one primitive of
// one recorded call, and the instances of one call share the draw's animation
// — a hundred crates is one call, and a hundred independently-animated
// characters is a hundred calls.
type AnimBinding struct {
	Skin   SkinBuffers
	Offset uint32
	// MorphAt indexes the frame's per-primitive morph offsets, or is -1 when
	// the model has no shapes and the draw's one block serves every primitive.
	// It is an index rather than a slice because the arena it points into is
	// still being appended to while these are written.
	MorphAt int
	// Skinned is false for every buffer-built mesh and every debug shape, and
	// for a model primitive no clip can move. Riding the free rest-frame path
	// instead would be correct, but it charges a procedural terrain mesh — the
	// highest-vertex-count thing scene can be handed — a per-vertex pose fetch
	// and TRS blend for a guaranteed identity.
	Skinned bool
	// Joint is the model joint a plain-bound placement rides, and plain says
	// it is one: joint 0 is a joint like any other, and every buffer-built
	// draw's binding is the zero value. A plain binding implies skinned.
	Joint uint32
	Plain bool
}

// SkinBuffers is the group 2 bindings a draw reads, in either half
// independently. The two flags are also what picks the draw's shader variant, so
// a half left empty is a half the module does not declare.
type SkinBuffers struct {
	Poses  gfx.BufferDescr
	Joints gfx.BufferDescr
	// Morphs is the model's one delta buffer. It is a binding of its own rather
	// than a range of the pose buffer
	// because the two halves are answered separately: a rigged prop has poses
	// and no shapes, and a face has shapes and no poses.
	Morphs gfx.BufferDescr
	// Bound says the pose pair is real and morphed says the delta buffer is. A
	// gfx.BufferDescr holds a byte slice and so is not comparable, and there is
	// no reserved zero descriptor, so "did anyone fill this in" needs a field
	// of its own - two of them, because the halves are answered separately.
	Bound   bool
	Morphed bool
}

// recordSliceBytes reinterprets a slice of records as the bytes uploaded for
// it, the array twin of recordBytes. Every GPU target cog builds for is
// little-endian, so the in-memory layout is the wire layout.
func recordSliceBytes[T any](records []T) []byte {
	if len(records) == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&records[0])), len(records)*int(unsafe.Sizeof(records[0])))
}
