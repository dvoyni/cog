// Morph targets: the delta store and the blend that adds this instance's
// active targets to a vertex.
//#include ./anim.wgsl
//#include ./instance.wgsl
//#include ./vertex.wgsl

//#if SCENE_MORPH

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

// The morph delta store is declared only where SCENE_MORPH is. A morph-only
// draw therefore declares group 2 binding 2 with no bindings 0 or 1: WebGPU
// permits non-contiguous binding numbers and gfx builds its layouts from
// reflection rather than by counting, so the gap costs nothing.
@group(2) @binding(2) var<storage, read> sceneMorphDeltas: SceneMorphDeltas;

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

//#endif
