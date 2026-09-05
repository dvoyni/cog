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
// error anywhere. So this module declares exactly what it reads.

// SceneFrame is one pass's view of the world. View and projection ride along
// beside their product because a shader wanting view-space depth cannot
// recover them from viewProjection.
struct SceneFrame {
    view: mat4x4<f32>,
    projection: mat4x4<f32>,
    viewProjection: mat4x4<f32>,
    cameraPosition: vec4<f32>,
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
struct ScenePbrMaterial {
    baseColorFactor: vec4<f32>,
};

@group(0) @binding(0) var<storage, read> sceneFrame: SceneFrame;
@group(0) @binding(1) var<storage, read> sceneInstances: SceneInstances;
@group(1) @binding(0) var<storage, read> scenePbrMaterial: ScenePbrMaterial;

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
    @location(2) uv0: vec2<f32>,
    @location(3) color: vec4<f32>,
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

// sceneWorldNormal transforms a local normal. The record carries no normal
// matrix - it would double the record for a case most instances do not have -
// so the inverse-transpose is derived here, for the instances that flagged it.
fn sceneWorldNormal(instance: SceneInstance, normal: vec3<f32>) -> vec3<f32> {
    let basis = mat3x3<f32>(
        instance.world0.xyz,
        instance.world1.xyz,
        instance.world2.xyz,
    );
    if (instance.flags & SCENE_NONUNIFORM) != 0u {
        return normalize(transpose(sceneInverse3(basis)) * normal);
    }
    return normalize(basis * normal);
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
    out.uv0 = vertex.uv0;
    out.color = vertex.color;
    return out;
}

// The tracer is deliberately unlit: it is the narrowest complete path from a
// recorded box to a pixel, and gating the first pixel on the BRDF would mean
// debugging both at once. Lighting and the glTF texture slots land with the
// bundled PBR.
@fragment
fn fs_main(in: SceneVertexOut) -> @location(0) vec4<f32> {
    return scenePbrMaterial.baseColorFactor * in.color;
}
