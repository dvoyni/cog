// The vertex layout this module reads, the interpolants it hands to the
// fragment stage, and the vertex-mid-deformation the deform path passes
// around.

// SceneVertexIn is glTF's eight core attributes at locations 0..7. The first
// six are read by every variant; the joints and weights are declared only where
// SCENE_SKIN is, which is the cut inside a struct the conditional grammar was
// justified by.
//
// The Go side honours that same cut, and there are two named layouts because of
// it: the standard one supplies the first six at 32 bytes, and the skinned one
// supplies all eight at 40. Which one a mesh stores in is decided at load from
// the fact that decides this variant - a geometry takes the skinned layout iff
// some placement draws it under SCENE_SKIN - so a buffer supplying six can
// never reach the variant that declares eight. The other direction is legal and
// ordinary: a shader may read fewer attributes than the layout supplies, which
// is what a plain-bound placement's static sibling does. See
// ErrMeshCustomLayoutNeedsMaterial, which guards a layout that is neither.
//
// The normal, the tangent and both UV sets are stored encoded, four bytes each:
// the normal as oct32 in a two-component 16-bit unorm, the tangent as one word
// of oct 15/15 plus handedness, each UV set as a two-component 16-bit unorm
// against the mesh's own range. So the first two arrive as vec2<f32> and u32
// and are directions only after vertexdecode.wgsl has had them. These types
// must equal what both named layouts supply - gfx compares the pair at pipeline
// time and refuses the draw - so neither side can drift.
//
// The joints and the weights are stored narrow too - one byte a joint index,
// capping a skin at 256, and one unorm byte a weight - and neither needs a
// decode source, because the fetch unit hands over the same vec4<u32> and
// vec4<f32> the wide forms did. What eight-bit weights do need is
// sceneDeformVertex's divide by the accumulated total: four of them cannot sum
// to exactly one, and a malformed file's never did.
//
// The UVs and the weights are the exception to that guard, and it is worth
// naming: a Unorm16x2 and a Float32x2 both arrive as vec2<f32>, and a Unorm8x4
// and a Float32x4 both as vec4<f32>, so the declarations below are the same
// either way and the interface check cannot see either narrowing. What makes a
// raw uv0 wrong is the mesh record, not the type - see sceneDecodeUV - and what
// makes raw weights wrong is the missing divide.
struct SceneVertexIn {
    @location(0) position: vec3<f32>,
    @location(1) normal: vec2<f32>,
    @location(2) tangent: u32,
    @location(3) uv0: vec2<f32>,
    @location(4) uv1: vec2<f32>,
    @location(5) color: vec4<f32>,
//#if SCENE_SKIN
    @location(6) joints: vec4<u32>,
    @location(7) weights: vec4<f32>,
//#endif
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

// SceneVertex is a vertex mid-deformation: still in the model's own space,
// with the skin applied and the world matrix not yet.
struct SceneVertex {
    position: vec3<f32>,
    normal: vec3<f32>,
    tangent: vec4<f32>,
};
