// The debug shapes' shader: the bundled vertex stage, and a fragment stage that
// returns the material's baseColorFactor untouched, so no light - the sun and
// ambient included - reaches a debug shape.
//
// It declares the material's uniform block through the three published
// Material paths and none of the bundled material's textures, so a draw binds
// the block alone in group 1. The vertex stage reads the pass's sceneFrame,
// which the bundled shader brings in on its fragment side, so FramePath is
// included here.
//
// It is mounted at builtin/ecsscene/debug.wgsl, the path debugShaderPath
// spells.
//#include builtin/scene/frame.wgsl
//#include builtin/scene/materialprologue.wgsl
//#include builtin/scene/materialfields.wgsl
//#include builtin/scene/materialepilogue.wgsl
//#include builtin/scene/vertexstage.wgsl

@fragment
fn fs_main(in: SceneVertexOut) -> @location(0) vec4<f32> {
    return scenePbrMaterial.baseColorFactor;
}
