// The material's numbers: its uniform block, the struct ScenePbrMaterial, and
// its binding. model sets the block whole on every set it builds - a draw
// params binding is one value, never its members - from PbrValues, the Go
// mirror of this struct, so the two change together or not at all.
//
// DECLARES: the struct ScenePbrMaterial and the binding scenePbrMaterial, the
// uniform block at @group(1) @binding(0).
//
// It is mounted at builtin/model/materialblock.wgsl. material.wgsl includes it,
// and so does scene's debug shader, which binds the block and none of the
// material's textures. A shader with numbers of its own declares them as a
// binding of its own rather than as members here: the block is model's, set
// whole, and a member added to it is one nothing would set.
//
// Its numbers are glTF's, by verbatim name, because they are user-facing: the
// glTF specification is their documentation. The per-slot metadata is flat
// named members rather than `transforms: array<TexTransform, 5>`, because
// array members are not name-addressable - and animating baseColorTransform per
// frame is UV scrolling, which the array form forecloses permanently. They
// take 160 bytes.
struct ScenePbrMaterial {
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
};

@group(1) @binding(0) var<uniform> scenePbrMaterial: ScenePbrMaterial;
