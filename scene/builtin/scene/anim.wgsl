// The per-frame animation arena, which both skinning and morphing read. The
// whole body is guarded: a source that owns a feature guards itself and is
// included unconditionally, which is what keeps the include graph a pure
// function of the root, independent of the defines.
//
// The guard is a condition rather than a third define, because the condition
// can say it: this arena is live exactly when either feature is.
//#if SCENE_SKIN | SCENE_MORPH

// SceneAnim is the per-frame animation arena, addressed absolutely by
// sceneInstance.animOffset in vec4 units:
//
//     vec4 0: { playCount, targetCount, morphBase, morphStride }
//     vec4 1: 4 reserved words
//     then  : playCount x { baseRow0, baseRow1, w0, w1 }
//     then  : targetCount x { targetIndex, weight }, two to a vec4
//
// The second vec4 is wholly reserved. It carried morphTargetStride - the
// vertexCount * morphStride the dense delta address multiplied by - and a morph
// target now stores records only for the span of vertices it moves, so each one
// carries its own base in its block's header and no per-primitive stride exists
// to fold.
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

@group(0) @binding(2) var<storage, read> sceneAnim: SceneAnim;

// SCENE_ANIM_HEADER is how many vec4s the sceneAnim block spends before its
// first play record.
const SCENE_ANIM_HEADER: u32 = 2u;

//#endif
