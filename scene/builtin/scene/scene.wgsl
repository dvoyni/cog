// The bundled scene shader: the two entry points, and the sources they are
// composed from.
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
// error anywhere. So this module declares exactly what it reads - which is why
// the skinning and morph bindings are declared only in the variants that read
// them, and why scene binds all five texture slots on every draw, a white
// texel and a flat normal where a material names none.
//
// Two defines pick the variant: SCENE_SKIN and SCENE_MORPH. There is still one
// module's worth of source - the BRDF below is written once, so a shading fix
// cannot land in some copies and not others - and the variants differ only in
// which declarations survive.
//#include ./vertexdecode.wgsl
//#include ./instance.wgsl
//#include ./deform.wgsl
//#include ./material.wgsl

@vertex
fn vs_main(
    vertex: SceneVertexIn,
    @builtin(instance_index) index: u32,
    @builtin(vertex_index) vertexIndex: u32,
) -> SceneVertexOut {
    let instance = sceneInstances.data[index];
    // Decode first, before anything deforms: the normal and the tangent are
    // stored quantised and every stage below - the morph deltas, the skin, the
    // world matrix - is arithmetic on directions. There is nowhere cheaper to
    // put this; see vertexdecode.wgsl, which says why each of the three
    // tempting foldings does not work.
    let decoded = SceneVertex(
        vertex.position,
        sceneDecodeNormal(vertex.normal),
        sceneDecodeTangent(vertex.tangent),
    );
    // Deformation next, then the instance: morphing and skinning resolve a
    // vertex into the model's own space, and the world matrix - re-root already
    // folded in - takes that to the world.
    let deformed = sceneDeformVertex(instance, vertexIndex, vertex, decoded);
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
