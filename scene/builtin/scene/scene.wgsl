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
//
// The punctual lights are naive forward: lightCount bounds a loop every shaded
// fragment runs whole, so adding a light costs every shaded pixel in the pass.
// The array is a fixed 16 because the cap is a fixed constant, which is what
// lets it be declared here rather than runtime-sized. Nothing is reserved for
// shadows: no sun matrix, no comparison sampler.
struct SceneFrame {
    view: mat4x4<f32>,
    projection: mat4x4<f32>,
    viewProjection: mat4x4<f32>,
    cameraPosition: vec4<f32>,
    sunDirection: vec4<f32>,
    sunColor: vec4<f32>,
    ambientSky: vec4<f32>,
    ambientGround: vec4<f32>,
    lightCount: u32,
    lights: array<SceneLight, 16>,
};

// SceneLight is one punctual light, 48 bytes, with no kind field. A point light
// is a spot whose cone is always on: direction an actual zero vector, spotScale
// 0 and spotOffset 1, so the cone term below is saturate(0 + 1). That relies on
// x * 0 == 0, which is false for NaN, which is why the direction is a real
// zero and never left uninitialised.
//
// invRange4 is 1/range^4, and 0 for an infinite range: saturate(1 - d^4 * 0)
// is exactly 1, so infinity costs no branch and no select. color is linear
// radiance with the light's intensity already premultiplied.
struct SceneLight {
    position: vec3<f32>,
    invRange4: f32,
    direction: vec3<f32>,
    spotScale: f32,
    color: vec3<f32>,
    spotOffset: f32,
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

// ScenePose is the 48-byte baked pose record - three aligned vec4 loads -
// holding globalJoint alone, unpremultiplied by the inverse bind. Rotation is
// a unit quaternion as xyzw; translation and scale use xyz and leave w spare.
//
// Scale is a vec3 rather than a scalar in translation.w. Squash-and-stretch is
// animated non-uniform scale, a mainstream idiom, and unlike Transform there is
// no Matrix escape hatch to correct it at.
struct ScenePose {
    rotation: vec4<f32>,
    translation: vec4<f32>,
    scale: vec4<f32>,
};

struct ScenePoses {
    data: array<ScenePose>,
};

// SceneSkinJoint is the 112-byte per-joint record: the inverse bind and its
// normal matrix interleaved. They share a joint index and are fetched together
// on every influence, so one buffer is one address computation instead of two
// and lands both halves adjacent for all four influences.
//
// The columns are explicit vec4s rather than mat4x3 and mat3x3 because WGSL
// pads every matrix column to 16 bytes anyway - the record already carries 28
// bytes matrix syntax cannot address, and normal0.w spends four of them on the
// tangent handedness the bind pose's determinant decides.
struct SceneSkinJoint {
    inverseBind0: vec4<f32>,
    inverseBind1: vec4<f32>,
    inverseBind2: vec4<f32>,
    inverseBind3: vec4<f32>,
    normal0: vec4<f32>,
    normal1: vec4<f32>,
    normal2: vec4<f32>,
};

struct SceneSkinJoints {
    data: array<SceneSkinJoint>,
};

// SceneMorphDeltas is one model's whole morph delta store: every morphed
// primitive's targets concatenated and reached by the block's own morphBase.
// One buffer per model rather than per primitive, because a buffer per
// primitive would mean a bind group per primitive.
//
// Records are vec4-aligned and masked, stride 16 / 32 / 48 bytes, in the fixed
// slot order position, normal, tangent - so morphStride alone says which slots
// a record holds, and the mask never has to be transmitted. A tightly packed
// 12 / 24 / 36 layout would be smaller and would force scalar indexing: nine
// loads per target per vertex instead of three.
struct SceneMorphDeltas {
    data: array<vec4<f32>>,
};

// SceneAnim is the per-frame animation arena, addressed absolutely by
// sceneInstance.animOffset in vec4 units:
//
//     vec4 0: { playCount, targetCount, morphBase, morphStride }
//     vec4 1: { morphTargetStride, 3 reserved words }
//     then  : playCount x { baseRow0, baseRow1, w0, w1 }
//     then  : targetCount x { targetIndex, weight }, two to a vec4
//
// The morph weights are a count-prefixed sparse list rather than a dense block:
// the CPU knows which entries are non-zero before it writes anything, so a
// 52-shape face with five active shapes costs 40 bytes instead of 256 and this
// loop runs five times over real work instead of 64 times with a continue. The
// zero-skip branch is gone entirely, because zeros never reach the GPU.
//
// It is indirect because sceneInstances is bound once per pass and shared by
// every draw in it: a fixed per-instance record would have to be sized for the
// worst case and put a ~320 byte tax on every debug line.
//
// It is typed u32 because everything that addresses is a count or a row, and
// only the two folded weights are floats - so the two bitcasts are on the
// arithmetic rather than on the addressing.
struct SceneAnim {
    data: array<vec4<u32>>,
};

@group(0) @binding(0) var<storage, read> sceneFrame: SceneFrame;
@group(0) @binding(1) var<storage, read> sceneInstances: SceneInstances;
@group(0) @binding(2) var<storage, read> sceneAnim: SceneAnim;

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

// Group 2 is per model: the baked poses and the per-joint records a skinned
// draw reads. Every draw binds them, skinned or not - a declared binding must
// be bound or CreateBindGroup fails the entry-count rule, its error is
// swallowed, and the whole frame's command buffer vanishes with no error
// anywhere. A draw with no skin of its own binds one shared null skin: a
// single identity pose row and a single identity joint.
//
// Three bindings, not four: the inverse bind and the normal matrix interleave
// above, which recovered the slot that keeps the whole module inside the
// browser core-adapter's floor of eight storage buffers per stage. Every
// reflected binding is emitted Vertex|Fragment unconditionally, so these count
// against the fragment stage too, and sceneMorphDeltas is the seventh and last
// one this module may ever declare - the eighth stays reserved.
@group(2) @binding(0) var<storage, read> scenePoses: ScenePoses;
@group(2) @binding(1) var<storage, read> sceneSkinJoints: SceneSkinJoints;
@group(2) @binding(2) var<storage, read> sceneMorphDeltas: SceneMorphDeltas;

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
// locations 0..7. Every scene mesh supplies all eight - scene.Vertex is one
// struct with a fixed stride - and this module reads all eight, so the two
// match exactly and neither direction of the layout rule is leaned on here.
// The direction that does fail validation is a shader input no attribute
// supplies; extra attributes the shader never declares are permitted, which
// is why a replacement material may declare a subset of these locations and
// still draw a standard mesh - see ErrMeshCustomLayoutNeedsMaterial, which
// guards only the other way round.
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
//
// world0..world2 are rows and mat3x3's arguments are columns, so the basis is
// transposed into place here rather than passed straight through. Passing them
// through returns M-transpose, which for the rotation most draws carry is the
// inverse rotation - normals that counter-rotate, on every draw whose basis is
// not symmetric.
fn sceneWorldBasis(instance: SceneInstance) -> mat3x3<f32> {
    return mat3x3<f32>(
        vec3<f32>(instance.world0.x, instance.world1.x, instance.world2.x),
        vec3<f32>(instance.world0.y, instance.world1.y, instance.world2.y),
        vec3<f32>(instance.world0.z, instance.world1.z, instance.world2.z),
    );
}

// sceneWorldNormal transforms a local normal. The record carries no normal
// matrix - it would double the record for a case most instances do not have -
// so the inverse-transpose is derived here, for the instances that flagged it.
//
// A uniform scale leaves the normal matrix parallel to the basis, and the
// normalize below discards the scale that separates them, so the flag buys the
// cofactors only where they change a direction.
fn sceneWorldNormal(instance: SceneInstance, normal: vec3<f32>) -> vec3<f32> {
    let basis = sceneWorldBasis(instance);
    if (instance.flags & SCENE_NONUNIFORM) != 0u {
        return normalize(sceneInverseTranspose3(basis) * normal);
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

// sceneInverseTranspose3 returns the normal matrix for a basis: the inverse
// transposed, by cofactors.
//
// The transpose costs nothing and must not be applied again by the caller. The
// three cross products below are the adjugate's rows, and the adjugate over the
// determinant is the inverse; writing them as mat3x3's columns instead is what
// transposes it, so the result is already inverse-transpose.
//
// A basis too flat to invert has no normal matrix at all. It returns the basis
// unchanged there - the wrong matrix, but a finite one, where the cofactors
// over a zero determinant would hand every downstream normalize a NaN.
fn sceneInverseTranspose3(basis: mat3x3<f32>) -> mat3x3<f32> {
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

// SCENE_ANIM_HEADER is how many vec4s the sceneAnim block spends before its
// first play record.
const SCENE_ANIM_HEADER: u32 = 2u;

// SceneVertex is a vertex mid-deformation: still in the model's own space,
// with the skin applied and the world matrix not yet.
struct SceneVertex {
    position: vec3<f32>,
    normal: vec3<f32>,
    tangent: vec4<f32>,
};

// SceneJointPose is one joint's blended transform before its inverse bind: the
// weighted mean of every playing frame's TRS.
struct SceneJointPose {
    rotation: vec4<f32>,
    translation: vec3<f32>,
    scale: vec3<f32>,
};

// sceneAnimPlayCount reads how many plays an instance is blending. An instance
// that animates nothing carries SCENE_NO_ANIM and blends none, which is the
// rest frame - a real pose rather than a collapse, because row 0 of every
// model is the authored hierarchy resolved once.
fn sceneAnimPlayCount(animOffset: u32) -> u32 {
    if animOffset == SCENE_NO_ANIM {
        return 0u;
    }
    return sceneAnim.data[animOffset].x;
}

// sceneBlendJoint accumulates one joint's pose across the instance's plays.
//
// The shader does no clip-length, wrap or normalisation arithmetic: the CPU
// folded weight * (1 - frac) and weight * frac into the two scalars and
// resolved the two rows, so this is a multiply and an add per frame. The bake
// fixed quaternion hemisphere continuity within each clip, so only the
// cross-play accumulation needs a sign check - two clips are two independent
// chains and may disagree.
fn sceneBlendJoint(animOffset: u32, playCount: u32, joint: u32) -> SceneJointPose {
    if playCount == 0u {
        // Row 0 is the rest frame, so the joint's own index is its row.
        let rest = scenePoses.data[joint];
        return SceneJointPose(rest.rotation, rest.translation.xyz, rest.scale.xyz);
    }
    var rotation = vec4<f32>(0.0);
    var translation = vec3<f32>(0.0);
    var scale = vec3<f32>(0.0);
    let base = animOffset + SCENE_ANIM_HEADER;
    for (var play = 0u; play < playCount; play = play + 1u) {
        let record = sceneAnim.data[base + play];
        let first = scenePoses.data[record.x + joint];
        let second = scenePoses.data[record.y + joint];
        let w0 = bitcast<f32>(record.z);
        let w1 = bitcast<f32>(record.w);
        var blended = first.rotation * w0 + second.rotation * w1;
        if dot(rotation, blended) < 0.0 {
            blended = -blended;
        }
        rotation = rotation + blended;
        translation = translation + first.translation.xyz * w0 + second.translation.xyz * w1;
        scale = scale + first.scale.xyz * w0 + second.scale.xyz * w1;
    }
    return SceneJointPose(normalize(rotation), translation, scale);
}

// sceneQuatRotate turns a vector by a unit quaternion in two cross products and
// no matrix build, which is what costs least when the rotation is used once -
// and a per-influence normal uses it once.
fn sceneQuatRotate(quaternion: vec4<f32>, vector: vec3<f32>) -> vec3<f32> {
    let axis = quaternion.xyz;
    let turned = cross(axis, vector) + quaternion.w * vector;
    return vector + 2.0 * cross(axis, turned);
}

fn sceneQuatBasis(quaternion: vec4<f32>) -> mat3x3<f32> {
    let x = quaternion.x;
    let y = quaternion.y;
    let z = quaternion.z;
    let w = quaternion.w;
    return mat3x3<f32>(
        vec3<f32>(1.0 - 2.0 * (y * y + z * z), 2.0 * (x * y + z * w), 2.0 * (x * z - y * w)),
        vec3<f32>(2.0 * (x * y - z * w), 1.0 - 2.0 * (x * x + z * z), 2.0 * (y * z + x * w)),
        vec3<f32>(2.0 * (x * z + y * w), 2.0 * (y * z - x * w), 1.0 - 2.0 * (x * x + y * y)),
    );
}

// sceneJointMatrix is one joint's fully blended transform with its inverse bind
// applied: the 4x3 that takes a bind-pose vertex into the model's own space.
//
// The inverse bind is applied here, after the cross-play blend, rather than
// premultiplied into the pose record at bake. Premultiplying injects the bind
// pose's routine non-uniform scale into a record that is then decomposed to
// TRS, which cannot represent shear at all.
fn sceneJointMatrix(pose: SceneJointPose, joint: SceneSkinJoint) -> mat4x3<f32> {
    let basis = sceneQuatBasis(pose.rotation);
    let scaled = mat3x3<f32>(
        basis[0] * pose.scale.x,
        basis[1] * pose.scale.y,
        basis[2] * pose.scale.z,
    );
    // The inverse bind's fourth row is (0, 0, 0, 1), so concatenating the two
    // 4x3s is three basis products plus one point transform.
    return mat4x3<f32>(
        scaled * joint.inverseBind0.xyz,
        scaled * joint.inverseBind1.xyz,
        scaled * joint.inverseBind2.xyz,
        scaled * joint.inverseBind3.xyz + pose.translation,
    );
}

// sceneMorphVertex adds this instance's active morph targets to a vertex.
//
// The shader never sees a play here. Morphing is linear in the weights, so the
// CPU blends every play's weight vector into one and this applies the deltas
// once, which is exactly equal to morphing per play and blending the results -
// unlike the pose case, there is no approximation traded away.
//
// It adds deltas and normalises nothing. The naive reading of glTF renormalises
// the normal after morphing and again after skinning; the first is dead work,
// because skinning is linear. The tangent delta adds to xyz and leaves w - the
// handedness - untouched.
fn sceneMorphVertex(
    instance: SceneInstance, vertexIndex: u32, vertex: SceneVertex,
) -> SceneVertex {
    if instance.animOffset == SCENE_NO_ANIM {
        return vertex;
    }
    let header = sceneAnim.data[instance.animOffset];
    let targetCount = header.y;
    if targetCount == 0u {
        return vertex;
    }
    let morphBase = header.z;
    let morphStride = header.w;
    let targetStride = sceneAnim.data[instance.animOffset + 1u].x;
    // The list sits after the play records, and two of its eight-byte entries
    // share one vec4.
    let list = instance.animOffset + SCENE_ANIM_HEADER + header.x;
    var out = vertex;
    for (var i = 0u; i < targetCount; i = i + 1u) {
        let packed = sceneAnim.data[list + i / 2u];
        let entry = select(packed.zw, packed.xy, (i & 1u) == 0u);
        let weight = bitcast<f32>(entry.y);
        // No base-vertex correction: gfx.MeshDescr owns its buffers and binds
        // them at offset 0, so vertexIndex is 0-based within the primitive and
        // targetStride is vertexCount * morphStride, folded on the CPU.
        let at = morphBase + entry.x * targetStride + vertexIndex * morphStride;
        out.position = out.position + weight * sceneMorphDeltas.data[at].xyz;
        if morphStride > 1u {
            out.normal = out.normal + weight * sceneMorphDeltas.data[at + 1u].xyz;
        }
        if morphStride > 2u {
            out.tangent = vec4<f32>(
                out.tangent.xyz + weight * sceneMorphDeltas.data[at + 2u].xyz,
                out.tangent.w,
            );
        }
    }
    return out;
}

// sceneDeformVertex runs linear blend skinning over the vertex's four
// influences, or hands the vertex back untouched for a draw with no skin.
//
// Normals take the precomputed normal matrix and then the blended rotation,
// never a shader inverse: the inverse bind can be non-orthonormal and so can
// the composed skinning matrix, and inverting it per vertex per influence is
// exactly the cost the load already paid once per joint. The tangent takes the
// same path plus Gram-Schmidt against the skinned normal, and its handedness
// follows the inverse bind's determinant sign, accumulated by weight so the
// answer is the majority influence's with no search.
fn sceneDeformVertex(
    instance: SceneInstance, vertexIndex: u32, vertex: SceneVertexIn,
) -> SceneVertex {
    // Morph, then skin, per the glTF order: the shapes reshape the mesh in its
    // own bind space and the skin then poses that.
    let base = sceneMorphVertex(
        instance, vertexIndex,
        SceneVertex(vertex.position, vertex.normal, vertex.tangent),
    );
    if (instance.flags & SCENE_NOSKIN) != 0u {
        return base;
    }
    let playCount = sceneAnimPlayCount(instance.animOffset);
    var position = vec3<f32>(0.0);
    var normal = vec3<f32>(0.0);
    var tangent = vec3<f32>(0.0);
    var handedness = 0.0;
    var total = 0.0;
    for (var influence = 0u; influence < 4u; influence = influence + 1u) {
        let weight = vertex.weights[influence];
        if weight == 0.0 {
            continue;
        }
        total = total + weight;
        let index = vertex.joints[influence];
        let joint = sceneSkinJoints.data[index];
        let pose = sceneBlendJoint(instance.animOffset, playCount, index);
        let skin = sceneJointMatrix(pose, joint);
        position = position + weight * (skin * vec4<f32>(base.position, 1.0));
        let normalMatrix = mat3x3<f32>(joint.normal0.xyz, joint.normal1.xyz, joint.normal2.xyz);
        normal = normal + weight * sceneQuatRotate(pose.rotation, normalMatrix * base.normal);
        tangent = tangent + weight * sceneQuatRotate(pose.rotation, normalMatrix * base.tangent.xyz);
        handedness = handedness + weight * joint.normal0.w;
    }
    // A vertex with no influence at all under a skinned draw is a malformed
    // file rather than a case to be correct about. It would collapse to the
    // origin with a zero normal, and normalize would turn that into a NaN that
    // spreads through the whole fragment stage, so it keeps its bind pose.
    if total == 0.0 {
        return base;
    }
    let skinnedNormal = normalize(normal);
    var skinnedTangent = base.tangent;
    let orthogonal = tangent - skinnedNormal * dot(skinnedNormal, tangent);
    if dot(orthogonal, orthogonal) > 1e-12 {
        skinnedTangent = vec4<f32>(normalize(orthogonal), base.tangent.w * sign(handedness));
    }
    return SceneVertex(position, skinnedNormal, skinnedTangent);
}

@vertex
fn vs_main(
    vertex: SceneVertexIn,
    @builtin(instance_index) index: u32,
    @builtin(vertex_index) vertexIndex: u32,
) -> SceneVertexOut {
    let instance = sceneInstances.data[index];
    // Deformation first, then the instance: morphing and skinning resolve a
    // vertex into the model's own space, and the world matrix - re-root already
    // folded in - takes that to the world.
    let deformed = sceneDeformVertex(instance, vertexIndex, vertex);
    let world = sceneWorldPosition(instance, deformed.position);
    var out: SceneVertexOut;
    out.position = sceneFrame.viewProjection * vec4<f32>(world, 1.0);
    out.worldPosition = world;
    out.normal = sceneWorldNormal(instance, deformed.normal);
    out.tangent = sceneWorldTangent(instance, deformed.tangent);
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

// sceneLightCount is how many punctual lights this pass carries, after scene
// culled them against the pass's frustum and capped them at 16.
fn sceneLightCount() -> u32 {
    return sceneFrame.lightCount;
}

// sceneLightSample is one punctual light's contribution at a point: glTF's
// falloff geometry, branchless. The range window is exactly 1 for an infinite
// range because invRange4 is 0 there; the cone is exactly 1 for a point light
// because its direction is zero and spotOffset is 1. Intensity is unitless -
// radiance at one world unit - so the inverse square lands in 0..1 directly.
//
// max(d2, 1e-6) is a robustness guard for a light sitting on the surface, not
// a falloff parameter. Scene's cap ranks lights by this same expression
// evaluated at the eye, so the two must stay in step.
fn sceneLightSample(i: u32, position: vec3<f32>) -> SceneLightSample {
    let light = sceneFrame.lights[i];
    let toLight = light.position - position;
    let d2 = dot(toLight, toLight);
    let direction = toLight * inverseSqrt(max(d2, 1e-12));
    let window = saturate(1.0 - d2 * d2 * light.invRange4);
    let cone = saturate(dot(-direction, light.direction) * light.spotScale + light.spotOffset);
    return SceneLightSample(direction, light.color * (window * cone / max(d2, 1e-6)));
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

// sceneShadeSurface is the sun, every punctual light in the pass, and
// hemispheric ambient scaled by the surface's occlusion. The light loop is
// naive forward: every shaded fragment runs it whole, so each light in the
// pass costs every shaded pixel one BRDF evaluation.
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
    for (var i = 0u; i < sceneLightCount(); i++) {
        shaded += scenePunctualContribution(
            sceneLightSample(i, s.position), s.normal, view, nDotV, diffuseColor, f0, alphaRoughness);
    }

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
