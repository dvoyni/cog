// The bundled PBR material: its per-batch record, its five texture slots, and
// the fetch that turns them into shading inputs.
//#include ./pbr.wgsl

// ScenePbrMaterial is the bundled material's per-batch record, bound as a range
// of the frame's material arena. The binding is the addressing: no index has to
// agree across the update/render thread boundary.
//
// Its numbers are glTF's, by verbatim name, because they are user-facing:
// OverrideParams merges by name and the glTF specification is their
// documentation. The per-slot metadata is flat named members rather than
// `transforms: array<TexTransform, 5>`, because array members are not
// name-addressable - and animating baseColorTransform per frame is UV
// scrolling, which the array form forecloses permanently.
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
    // alphaCutoff is zero for an OPAQUE material, which makes the discard below
    // a no-op there: alpha is never below zero. MASK is otherwise
    // fixed-function-identical to OPAQUE.
    alphaCutoff: f32,
    // uvSets selects TEXCOORD_0 or TEXCOORD_1 per slot, one bit each. Two sets
    // is glTF core's minimum and the cap scene keeps.
    uvSets: u32,
    pad: u32,
};

@group(1) @binding(0) var<storage, read> scenePbrMaterial: ScenePbrMaterial;
// Five textures and five samplers, one pair per slot. glTF references a sampler
// per texture and two slots of one material can legitimately differ - a tiling
// ground beside a clamped decal - so a single shared sampler would silently
// mis-sample a legal file.
@group(1) @binding(1) var baseColorTexture: texture_2d<f32>;
@group(1) @binding(2) var baseColorSampler: sampler;
@group(1) @binding(3) var metallicRoughnessTexture: texture_2d<f32>;
@group(1) @binding(4) var metallicRoughnessSampler: sampler;
@group(1) @binding(5) var normalTexture: texture_2d<f32>;
@group(1) @binding(6) var normalSampler: sampler;
@group(1) @binding(7) var occlusionTexture: texture_2d<f32>;
@group(1) @binding(8) var occlusionSampler: sampler;
@group(1) @binding(9) var emissiveTexture: texture_2d<f32>;
@group(1) @binding(10) var emissiveSampler: sampler;

// sceneSlotUV picks a slot's UV set and applies its KHR_texture_transform. The
// transform is applied unconditionally - about 30 ALU across five slots, less
// than one iteration of the light loop - because a branch per slot costs more
// than it saves.
fn sceneSlotUV(uv0: vec2<f32>, uv1: vec2<f32>, slot: u32, transform: vec4<f32>, rotation: f32) -> vec2<f32> {
    let uv = select(uv0, uv1, ((scenePbrMaterial.uvSets >> slot) & 1u) != 0u);
    let scaled = uv * transform.zw;
    let cosine = cos(rotation);
    let sine = sin(rotation);
    let rotated = vec2<f32>(
        cosine * scaled.x + sine * scaled.y,
        -sine * scaled.x + cosine * scaled.y,
    );
    return transform.xy + rotated;
}

// sceneShadingNormal builds the shading normal: the interpolated normal, flipped
// on a back face, with the tangent-space normal map applied over it.
//
// The double-sided flip is unconditional. glTF requires the shading normal be
// flipped on back faces of a double-sided material, and making it conditional
// would cost a record flag and a branch to save nothing: for a single-sided
// material the select is a proven no-op, because a back face is not rasterised.
fn sceneShadingNormal(
    normal: vec3<f32>, tangent: vec4<f32>, sampled: vec3<f32>, scale: f32, frontFacing: bool,
) -> vec3<f32> {
    var n = normalize(normal);
    n = select(-n, n, frontFacing);
    // glTF's normalScale scales the tangent-space x and y only.
    let mapped = normalize(vec3<f32>((sampled.xy * 2.0 - 1.0) * scale, sampled.z * 2.0 - 1.0));
    let projected = tangent.xyz - n * dot(n, tangent.xyz);
    if dot(projected, projected) < 1e-12 {
        return n;
    }
    let t = normalize(projected);
    let b = cross(n, t) * select(-1.0, 1.0, tangent.w >= 0.0);
    return normalize(mat3x3<f32>(t, b, n) * mapped);
}

// scenePbrSurface fetches every slot once and returns the shading inputs they
// produce.
fn scenePbrSurface(
    uv0: vec2<f32>, uv1: vec2<f32>, normal: vec3<f32>, tangent: vec4<f32>,
    color: vec4<f32>, worldPos: vec3<f32>, frontFacing: bool,
) -> ScenePbrSurface {
    let baseUV = sceneSlotUV(uv0, uv1, 0u,
        scenePbrMaterial.baseColorTransform, scenePbrMaterial.baseColorRotation);
    let metallicRoughnessUV = sceneSlotUV(uv0, uv1, 1u,
        scenePbrMaterial.metallicRoughnessTransform, scenePbrMaterial.metallicRoughnessRotation);
    let normalUV = sceneSlotUV(uv0, uv1, 2u,
        scenePbrMaterial.normalTransform, scenePbrMaterial.normalRotation);
    let occlusionUV = sceneSlotUV(uv0, uv1, 3u,
        scenePbrMaterial.occlusionTransform, scenePbrMaterial.occlusionRotation);
    let emissiveUV = sceneSlotUV(uv0, uv1, 4u,
        scenePbrMaterial.emissiveTransform, scenePbrMaterial.emissiveRotation);

    let base = textureSample(baseColorTexture, baseColorSampler, baseUV) *
        scenePbrMaterial.baseColorFactor * color;
    // glTF packs occlusion in R, roughness in G and metallic in B.
    let metallicRoughness = textureSample(
        metallicRoughnessTexture, metallicRoughnessSampler, metallicRoughnessUV);
    let occluded = textureSample(occlusionTexture, occlusionSampler, occlusionUV).r;
    let sampledNormal = textureSample(normalTexture, normalSampler, normalUV).xyz;
    let emissive = textureSample(emissiveTexture, emissiveSampler, emissiveUV).rgb *
        scenePbrMaterial.emissiveFactor.rgb;

    var out: ScenePbrSurface;
    out.surface.position = worldPos;
    out.surface.normal = sceneShadingNormal(
        normal, tangent, sampledNormal, scenePbrMaterial.normalScale, frontFacing);
    out.surface.baseColor = base.rgb;
    out.surface.metallic = metallicRoughness.b * scenePbrMaterial.metallicFactor;
    out.surface.roughness = metallicRoughness.g * scenePbrMaterial.roughnessFactor;
    // occlusionStrength interpolates from no occlusion at 0 to the sampled
    // value at 1, which is glTF's own wording.
    out.surface.occlusion = 1.0 + scenePbrMaterial.occlusionStrength * (occluded - 1.0);
    out.emissive = emissive;
    out.alpha = base.a;
    return out;
}
