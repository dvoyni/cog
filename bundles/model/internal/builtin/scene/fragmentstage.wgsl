// The bundled fragment stage as a function: the surface the file's material
// describes, its alpha-cutoff discard, and the shading, for a fragment entry
// point to call.
//
// DECLARES: fn scenePbrFragment. It includes vertex.wgsl for SceneVertexOut,
// the struct it reads, and material.wgsl, which brings in PbrPath and FramePath
// and declares the material's bindings: scenePbrMaterial, the uniform block at
// @group(1) @binding(0), and the five textures and five samplers at
// @group(1) @binding(1) to (10), named as model.PbrSlots names them. The
// material fills every one of them on every draw, from the file or from the
// bundled defaults. Every other name it brings in begins scene, Scene
// or SCENE_.
//
// It is mounted at builtin/scene/fragmentstage.wgsl and published as
// model.FragmentStagePath, so a custom scene shader is the bundled PBR plus
// whatever it does after: its fs_main calls scenePbrFragment and changes the
// colour, and a shading fix here reaches it with nothing copied. Group 3 is
// the one bind group left for its own bindings, and its per-draw numbers are
// members it adds to scenePbrMaterial by composing the block before including
// this: see materialprologue.wgsl.
//#include ./vertex.wgsl
//#include ./material.wgsl

// scenePbrFragment is the bundled shading of one fragment: linear radiance in
// rgb and coverage in a. It discards below the material's MASK cutoff, a no-op
// for an OPAQUE material because its cutoff is zero and alpha is never below
// zero, so it is callable only from a fragment stage.
fn scenePbrFragment(in: SceneVertexOut, frontFacing: bool) -> vec4<f32> {
    let r = scenePbrSurface(in.uv0, in.uv1, in.normal, in.tangent, in.color,
        in.worldPosition, frontFacing);
    if r.alpha < scenePbrMaterial.alphaCutoff {
        discard;
    }
    return vec4<f32>(sceneShadeSurface(r.surface) + r.emissive, r.alpha);
}
