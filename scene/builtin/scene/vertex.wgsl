// The vertex layout this module reads, the interpolants it hands to the
// fragment stage, and the vertex-mid-deformation the deform path passes
// around.

// SceneVertexIn is the one vertex layout, glTF's eight core attributes at
// locations 0..7. The first six are read by every variant; the joints and
// weights are declared only where SCENE_SKIN is, which is the cut inside a
// struct the conditional grammar was justified by. A scene mesh supplies all
// eight regardless - scene.Vertex is one struct with a fixed stride - and the
// direction that fails validation is a shader input no attribute supplies,
// never the other way round, so declaring six of the eight is legal. See
// ErrMeshCustomLayoutNeedsMaterial, which guards only the other way round.
//
// The normal and the tangent are stored encoded, four bytes each: the normal as
// oct32 in a two-component 16-bit unorm, the tangent as one word of oct 15/15
// plus handedness. So they arrive as vec2<f32> and u32 and are directions only
// after vertexdecode.wgsl has had them. These types must equal what scene's
// standardVertexLayout supplies - gfx compares the pair at pipeline time and
// refuses the draw - so neither side can drift.
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
