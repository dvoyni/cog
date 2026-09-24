// The bundled material's numbers: the members of the material's uniform block,
// between materialprologue.wgsl and materialepilogue.wgsl. gfx packs the block
// per draw from the draw's params by member name, like any shader's uniforms,
// so the renderer that draws it knows none of these names: model hands every
// member over as a named param with the material, and a caller's param of the
// same name overrides it on the draw.
//
// DECLARES: the members baseColorFactor, emissiveFactor, baseColorTransform,
// metallicRoughnessTransform, normalTransform, occlusionTransform,
// emissiveTransform, baseColorRotation, metallicRoughnessRotation,
// normalRotation, occlusionRotation, emissiveRotation, metallicFactor,
// roughnessFactor, normalScale, occlusionStrength, alphaCutoff and uvSets of
// ScenePbrMaterial. Nothing at module scope: this source is only the inside of
// a struct, and is not WGSL on its own.
//
// It is mounted at builtin/model/materialfields.wgsl and published as
// model.MaterialFieldsPath. A shader adding numbers of its own writes a fields
// source that includes this one first and lists its members after it, each
// named with a prefix of its own. A shader extending that one includes that
// source the same way, so extensions stack. The block is capped at 256 bytes
// (gfx.ErrUniformBlockTooLarge) and these take 160 of them; every extension on
// top shares the rest.
//
// Its numbers are glTF's, by verbatim name, because they are user-facing: the
// glTF specification is their documentation. The per-slot metadata is flat
// named members rather than `transforms: array<TexTransform, 5>`, because
// array members are not name-addressable - and animating baseColorTransform per
// frame is UV scrolling, which the array form forecloses permanently.
    baseColorFactor: vec4<f32>,
    emissiveFactor: vec4<f32>,
    // Each transform is offset.xy, scale.xy, with its rotation below:
    // KHR_texture_transform, applied unconditionally.
    baseColorTransform: vec4<f32>,
    metallicRoughnessTransform: vec4<f32>,
    normalTransform: vec4<f32>,
    occlusionTransform: vec4<f32>,
    emissiveTransform: vec4<f32>,
    baseColorRotation: f32,
    metallicRoughnessRotation: f32,
    normalRotation: f32,
    occlusionRotation: f32,
    emissiveRotation: f32,
    metallicFactor: f32,
    roughnessFactor: f32,
    normalScale: f32,
    occlusionStrength: f32,
    // alphaCutoff is zero for an OPAQUE material, which makes the discard in
    // fragmentstage.wgsl a no-op there: alpha is never below zero. MASK is
    // otherwise fixed-function-identical to OPAQUE.
    alphaCutoff: f32,
    // uvSets selects TEXCOORD_0 or TEXCOORD_1 per slot, one bit each. Two sets
    // is glTF core's minimum and the cap scene keeps.
    uvSets: u32,
