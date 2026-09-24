// The bundled vertex stage: vs_main, and the sources it is composed from.
//
// DECLARES: fn vs_main. It includes the private sources the stage reads -
// vertexdecode.wgsl, instance.wgsl, vertex.wgsl, anim.wgsl, skin.wgsl,
// morph.wgsl and deform.wgsl - so an includer gets every name those declare
// too, once: the structs SceneVertexIn, SceneVertexOut and SceneVertex among
// them, and the group 0 bindings sceneInstances, sceneAnim and sceneMeshes and,
// under SCENE_SKIN and SCENE_MORPH, the group 2 ones. Every one of those names
// begins scene, Scene or SCENE_, and they are not contract beyond that prefix.
//
// It is mounted at builtin/model/vertexstage.wgsl and published as
// model.VertexStagePath, so a custom scene shader places, deforms and decodes
// a vertex exactly as the bundled one does and writes only its fragment stage.
// Whether the skin and morph halves survive is the draw's variant, which the
// renderer supplies as SCENE_SKIN and SCENE_MORPH whatever shader is in effect.
//#include ./vertexdecode.wgsl
//#include ./instance.wgsl
//#include ./deform.wgsl

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
    // The UVs decode against this mesh's own record, one draw-uniform fetch.
    // They are not deformed, so where they decode is free - but they decode
    // here, beside the two that are, so the whole storage vertex becomes the
    // authored one in one place.
    let mesh = sceneMeshOf(instance);
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
    out.uv0 = sceneDecodeUV(vertex.uv0, mesh.uv0Scale, mesh.uv0Bias);
    out.uv1 = sceneDecodeUV(vertex.uv1, mesh.uv1Scale, mesh.uv1Bias);
    out.color = vertex.color;
    return out;
}
