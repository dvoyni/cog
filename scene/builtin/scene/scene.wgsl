// The bundled scene shader.
//
// Group numbering is scene's frequency convention, expressed here because gfx
// binds whatever reflection reports and never renumbers: group 0 is per pass,
// group 1 per material, group 2 per model, group 3 reserved. Ascending
// frequency, lowest group changing least.
//
// Every numeric input is a storage buffer. gfx's uniform path gives every draw
// its own pooled 256-byte buffer and its own upload, so a thousand draws would
// be a thousand buffers against a 256-byte cap; scene abandons it entirely and
// binds ranges of one per-frame arena instead.
//
// A declared binding must be bound at draw time or CreateBindGroup fails, its
// error is swallowed, and the whole frame's command buffer vanishes with no
// error anywhere. So this module declares exactly what it reads, and scene
// binds all five texture slots on every draw - a white texel and a flat normal
// where a material names none.
//
// There is one module, not a variant per feature set: gfx.ShaderDescr is
// source-or-path and the backend hardcodes vs_main/fs_main, so a variant would
// be a whole second module carrying its own copy of the BRDF below - the
// failure where a shading fix lands in some copies and not others.

// SceneFrame is one pass's view of the world. View and projection ride along
// beside their product because a shader wanting view-space depth cannot
// recover them from viewProjection.
//
// The sun and hemispheric ambient are per-camera fields rather than entries in
// a light array: packing the sun as a directional entry would cost an explicit
// discriminator and waste position, range and cone on it, and hemispheric
// ambient is normal-dependent rather than a direction, so it could never join
// the loop anyway. Every colour here is linear radiance with its intensity
// already premultiplied.
struct SceneFrame {
    view: mat4x4<f32>,
    projection: mat4x4<f32>,
    viewProjection: mat4x4<f32>,
    cameraPosition: vec4<f32>,
    sunDirection: vec4<f32>,
    sunColor: vec4<f32>,
    ambientSky: vec4<f32>,
    ambientGround: vec4<f32>,
};

// SceneInstance is the 64-byte per-instance record. world0..world2 are the rows
// of the 4x3 world matrix, translation in w; the fourth row of an affine
// transform is known and is not sent.
struct SceneInstance {
    world0: vec4<f32>,
    world1: vec4<f32>,
    world2: vec4<f32>,
    animOffset: u32,
    flags: u32,
    spare: vec2<u32>,
};

// The runtime array is wrapped in a struct because reflection walks struct
// members: a global typed as a bare array is not reported, and an unreported
// binding is an unbound one.
struct SceneInstances {
    data: array<SceneInstance>,
};

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

@group(0) @binding(0) var<storage, read> sceneFrame: SceneFrame;
@group(0) @binding(1) var<storage, read> sceneInstances: SceneInstances;

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

// SCENE_NONUNIFORM marks an instance whose world matrix does not scale
// uniformly. Transforming a normal by such a matrix is wrong, so those
// instances - and only those - pay for an inverse-transpose. The branch is
// uniform across the whole instance.
const SCENE_NONUNIFORM: u32 = 1u;
// SCENE_NOSKIN marks a draw with no skin of its own. It is set on every
// buffer-built draw, which is every draw this shader can be asked to make.
const SCENE_NOSKIN: u32 = 2u;
// SCENE_NO_ANIM in animOffset means the instance animates nothing.
const SCENE_NO_ANIM: u32 = 0xffffffffu;

const SCENE_PI: f32 = 3.14159265359;
// SCENE_DIELECTRIC_F0 is the normal-incidence reflectance of a dielectric,
// which metallic lerps toward the base colour.
const SCENE_DIELECTRIC_F0: vec3<f32> = vec3<f32>(0.04, 0.04, 0.04);

// SceneVertexIn is the one vertex layout, glTF's eight core attributes at
// locations 0..7. All eight are declared even where this module does not read
// them: the layout is fixed for every scene mesh, and a shader input that is
// missing where the buffer supplies it is the mismatch that fails validation.
struct SceneVertexIn {
    @location(0) position: vec3<f32>,
    @location(1) normal: vec3<f32>,
    @location(2) tangent: vec4<f32>,
    @location(3) uv0: vec2<f32>,
    @location(4) uv1: vec2<f32>,
    @location(5) color: vec4<f32>,
    @location(6) joints: vec4<u32>,
    @location(7) weights: vec4<f32>,
};

struct SceneVertexOut {
    @builtin(position) position: vec4<f32>,
    @location(0) worldPosition: vec3<f32>,
    @location(1) normal: vec3<f32>,
    @location(2) tangent: vec4<f32>,
    @location(3) uv0: vec2<f32>,
    @location(4) uv1: vec2<f32>,
    @location(5) color: vec4<f32>,
};

// sceneWorldPosition transforms a local position by the instance's 4x3 world
// matrix.
fn sceneWorldPosition(instance: SceneInstance, position: vec3<f32>) -> vec3<f32> {
    let local = vec4<f32>(position, 1.0);
    return vec3<f32>(
        dot(instance.world0, local),
        dot(instance.world1, local),
        dot(instance.world2, local),
    );
}

// sceneWorldBasis is the instance's world matrix without its translation.
fn sceneWorldBasis(instance: SceneInstance) -> mat3x3<f32> {
    return mat3x3<f32>(
        instance.world0.xyz,
        instance.world1.xyz,
        instance.world2.xyz,
    );
}

// sceneWorldNormal transforms a local normal. The record carries no normal
// matrix - it would double the record for a case most instances do not have -
// so the inverse-transpose is derived here, for the instances that flagged it.
fn sceneWorldNormal(instance: SceneInstance, normal: vec3<f32>) -> vec3<f32> {
    let basis = sceneWorldBasis(instance);
    if (instance.flags & SCENE_NONUNIFORM) != 0u {
        return normalize(transpose(sceneInverse3(basis)) * normal);
    }
    return normalize(basis * normal);
}

// sceneWorldTangent transforms a local tangent. A tangent lies in the surface
// rather than across it, so it rides the plain basis even under non-uniform
// scale; the fragment stage re-orthogonalises it against the normal.
fn sceneWorldTangent(instance: SceneInstance, tangent: vec4<f32>) -> vec4<f32> {
    let world = sceneWorldBasis(instance) * tangent.xyz;
    return vec4<f32>(world, tangent.w);
}

// sceneInverse3 inverts a 3x3 basis by cofactors.
fn sceneInverse3(basis: mat3x3<f32>) -> mat3x3<f32> {
    let a = basis[0];
    let b = basis[1];
    let c = basis[2];
    let cofactor0 = cross(b, c);
    let cofactor1 = cross(c, a);
    let cofactor2 = cross(a, b);
    let determinant = dot(a, cofactor0);
    if abs(determinant) < 1e-12 {
        return basis;
    }
    return mat3x3<f32>(cofactor0, cofactor1, cofactor2) * (1.0 / determinant);
}

@vertex
fn vs_main(vertex: SceneVertexIn, @builtin(instance_index) index: u32) -> SceneVertexOut {
    let instance = sceneInstances.data[index];
    let world = sceneWorldPosition(instance, vertex.position);
    var out: SceneVertexOut;
    out.position = sceneFrame.viewProjection * vec4<f32>(world, 1.0);
    out.worldPosition = world;
    out.normal = sceneWorldNormal(instance, vertex.normal);
    out.tangent = sceneWorldTangent(instance, vertex.tangent);
    out.uv0 = vertex.uv0;
    out.uv1 = vertex.uv1;
    out.color = vertex.color;
    return out;
}

// SceneSurface is what lighting needs and nothing more. It carries no view
// vector - that is one normalise away from sceneCameraPosition(), one less
// field to get wrong - and no emissive, because emissive is the material's own
// output rather than lighting: a shader writes sceneShadeSurface(s) + emissive.
struct SceneSurface {
    position: vec3<f32>,
    normal: vec3<f32>,
    baseColor: vec3<f32>,
    metallic: f32,
    roughness: f32,
    occlusion: f32,
};

// SceneLightSample is one light's contribution at a point: the direction from
// the surface towards the light, and the radiance arriving along it.
struct SceneLightSample {
    direction: vec3<f32>,
    radiance: vec3<f32>,
};

// ScenePbrSurface is everything one set of texture fetches produces, returned
// in one call rather than through separate emissive and alpha helpers that
// would invite the same texture to be fetched two or three times.
struct ScenePbrSurface {
    surface: SceneSurface,
    emissive: vec3<f32>,
    alpha: f32,
};

fn sceneCameraPosition() -> vec3<f32> {
    return sceneFrame.cameraPosition.xyz;
}

// sceneAmbient is the hemispheric ambient a normal sees: ground below, sky
// above. It is the one light term that is normal-dependent rather than
// directional, which is why it can never be an entry in the light array.
fn sceneAmbient(normal: vec3<f32>) -> vec3<f32> {
    return mix(sceneFrame.ambientGround.rgb, sceneFrame.ambientSky.rgb, normal.y * 0.5 + 0.5);
}

// sceneSun is the directional light every camera carries. Its radiance is zero
// when the camera declared no sun direction, so the term costs a multiply by
// black rather than a branch.
fn sceneSun() -> SceneLightSample {
    return SceneLightSample(-sceneFrame.sunDirection.xyz, sceneFrame.sunColor.rgb);
}

// sceneD_GGX is the Trowbridge-Reitz microfacet distribution.
fn sceneD_GGX(nDotH: f32, alphaRoughness: f32) -> f32 {
    let alphaSquared = alphaRoughness * alphaRoughness;
    let f = nDotH * nDotH * (alphaSquared - 1.0) + 1.0;
    return alphaSquared / max(SCENE_PI * f * f, 1e-9);
}

// sceneV_SmithGGXCorrelated is Smith height-correlated visibility, which is the
// geometry term with the 1/(4 NdotL NdotV) denominator already folded in.
fn sceneV_SmithGGXCorrelated(nDotL: f32, nDotV: f32, alphaRoughness: f32) -> f32 {
    let alphaSquared = alphaRoughness * alphaRoughness;
    let lambdaV = nDotL * sqrt(nDotV * nDotV * (1.0 - alphaSquared) + alphaSquared);
    let lambdaL = nDotV * sqrt(nDotL * nDotL * (1.0 - alphaSquared) + alphaSquared);
    let sum = lambdaV + lambdaL;
    if sum <= 0.0 {
        return 0.0;
    }
    return 0.5 / sum;
}

// sceneF_Schlick is the Schlick Fresnel approximation.
fn sceneF_Schlick(f0: vec3<f32>, vDotH: f32) -> vec3<f32> {
    let scale = pow(clamp(1.0 - vDotH, 0.0, 1.0), 5.0);
    return f0 + (vec3<f32>(1.0, 1.0, 1.0) - f0) * scale;
}

// sceneEnvBRDFApprox is the analytic split-sum approximation of the environment
// specular term. It is what keeps ambient reaching metals: diffuseColor is
// baseColor * (1 - metallic), so a pure metal has zero diffuse and would render
// black everywhere the sun does not reach.
//
// Honest limitation: this approximates an environment that does not exist, so a
// mirror-smooth metal reflects a smooth gradient rather than the scene. Image-
// based lighting substitutes into exactly this term and the ambient diffuse one.
fn sceneEnvBRDFApprox(f0: vec3<f32>, roughness: f32, nDotV: f32) -> vec3<f32> {
    let c0 = vec4<f32>(-1.0, -0.0275, -0.572, 0.022);
    let c1 = vec4<f32>(1.0, 0.0425, 1.04, -0.04);
    let r = roughness * c0 + c1;
    let a004 = min(r.x * r.x, exp2(-9.28 * nDotV)) * r.x + r.y;
    let scaleBias = vec2<f32>(-1.04, 1.04) * a004 + r.zw;
    return f0 * scaleBias.x + vec3<f32>(scaleBias.y, scaleBias.y, scaleBias.y);
}

// scenePunctualContribution is the Khronos reference BRDF for one light:
// Lambert diffuse weighted by 1 - F, plus GGX specular.
fn scenePunctualContribution(
    light: SceneLightSample, normal: vec3<f32>, view: vec3<f32>, nDotV: f32,
    diffuseColor: vec3<f32>, f0: vec3<f32>, alphaRoughness: f32,
) -> vec3<f32> {
    let toLight = light.direction;
    let nDotL = dot(normal, toLight);
    if nDotL <= 0.0 {
        return vec3<f32>(0.0, 0.0, 0.0);
    }
    let halfway = normalize(toLight + view);
    let nDotH = clamp(dot(normal, halfway), 0.0, 1.0);
    let vDotH = clamp(dot(view, halfway), 0.0, 1.0);
    let fresnel = sceneF_Schlick(f0, vDotH);
    let diffuse = (vec3<f32>(1.0, 1.0, 1.0) - fresnel) * diffuseColor / SCENE_PI;
    let specular = fresnel *
        sceneV_SmithGGXCorrelated(nDotL, nDotV, alphaRoughness) *
        sceneD_GGX(nDotH, alphaRoughness);
    return (diffuse + specular) * light.radiance * nDotL;
}

// sceneShadeSurface is the sun plus hemispheric ambient scaled by the surface's
// occlusion. Punctual lights join this loop when they land; nothing else about
// the surface contract changes when they do.
fn sceneShadeSurface(s: SceneSurface) -> vec3<f32> {
    let view = normalize(sceneCameraPosition() - s.position);
    let nDotV = clamp(dot(s.normal, view), 1e-4, 1.0);
    let metallic = clamp(s.metallic, 0.0, 1.0);
    let roughness = clamp(s.roughness, 0.0, 1.0);
    let alphaRoughness = roughness * roughness;
    let diffuseColor = s.baseColor * (1.0 - metallic);
    let f0 = mix(SCENE_DIELECTRIC_F0, s.baseColor, metallic);

    var shaded = scenePunctualContribution(
        sceneSun(), s.normal, view, nDotV, diffuseColor, f0, alphaRoughness);

    // Ambient splits two ways, both scaled by occlusion, because the diffuse
    // half alone leaves a metal black.
    let ambientDiffuse = sceneAmbient(s.normal) * diffuseColor;
    let ambientSpecular = sceneAmbient(reflect(-view, s.normal)) *
        sceneEnvBRDFApprox(f0, roughness, nDotV);
    shaded += (ambientDiffuse + ambientSpecular) * s.occlusion;
    return shaded;
}

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

@fragment
fn fs_main(in: SceneVertexOut, @builtin(front_facing) frontFacing: bool) -> @location(0) vec4<f32> {
    let r = scenePbrSurface(in.uv0, in.uv1, in.normal, in.tangent, in.color,
        in.worldPosition, frontFacing);
    // The MASK cutoff, a no-op for an OPAQUE material because its cutoff is
    // zero and alpha is never below zero.
    if r.alpha < scenePbrMaterial.alphaCutoff {
        discard;
    }
    return vec4<f32>(sceneShadeSurface(r.surface) + r.emissive, r.alpha);
}
