// Morph targets: the delta store and the blend that adds this instance's
// active targets to a vertex.
//#include ./anim.wgsl
//#include ./instance.wgsl
//#include ./vertex.wgsl

//#if SCENE_MORPH

// SceneMorphDeltas is one model's whole morph delta store: every morphed
// primitive's block concatenated and reached by the block's own morphBase. One
// buffer per model rather than per primitive, because a buffer per primitive
// would mean a bind group per primitive.
//
// It is a raw word array, because a block is not a uniform run of records. One
// block, starting at morphBase:
//
//     ranges       3 words per present slot, one f32 per axis, bitcast
//     per target   3 words: base, first, count
//     records      target t holds count_t of them, dense over its span
//
// A record is 8 bytes for position, 4 for normal, 4 for tangent, in that fixed
// slot order - prefix sums 2 / 3 / 4 words, which stay distinct, so morphStride
// alone still says which slots a record holds and the mask never has to be
// transmitted. Position keeps 16 bits an axis and the two directions 8: their
// ranges differ by up to 60x on one primitive, and a position error is a
// displacement where a normal error is an angle.
//
// The spare bits - 16 in the position slot, 8 in each direction slot - are
// reserved and never read. Absence is a span that does not cover the vertex,
// not a sentinel value.
struct SceneMorphDeltas {
    data: array<u32>,
};

// The morph delta store is declared only where SCENE_MORPH is. A morph-only
// draw therefore declares group 2 binding 2 with no bindings 0 or 1: WebGPU
// permits non-contiguous binding numbers and gfx builds its layouts from
// reflection rather than by counting, so the gap costs nothing.
@group(2) @binding(2) var<storage, read> sceneMorphDeltas: SceneMorphDeltas;

// sceneMorphRange reads one slot's three-axis range out of a block header.
fn sceneMorphRange(at: u32) -> vec3<f32> {
    return vec3<f32>(
        bitcast<f32>(sceneMorphDeltas.data[at]),
        bitcast<f32>(sceneMorphDeltas.data[at + 1u]),
        bitcast<f32>(sceneMorphDeltas.data[at + 2u]),
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
// handedness - untouched, which is also why a target stores no fourth
// component: handedness is not a thing a shape can move.
//
// A target stores records only for the span of vertices it moves. 93% of the
// float store was exactly zero, so 93% of (vertex, active target) pairs used to
// load a record and add three weighted zeros; they are one unsigned compare
// now. Runs and a per-record vertex index are both smaller and both need a walk
// or a binary search in this loop, which is the wrong direction by this path's
// own standard.
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
    let recordWords = header.w;
    // The record is a prefix of position, normal, tangent at 2 / 1 / 1 words,
    // so the slot count is the stride less the position slot's extra word.
    let slots = recordWords - 1u;
    let positionRange = sceneMorphRange(morphBase);
    var normalRange = vec3<f32>(0.0);
    if recordWords > 2u {
        normalRange = sceneMorphRange(morphBase + 3u);
    }
    var tangentRange = vec3<f32>(0.0);
    if recordWords > 3u {
        tangentRange = sceneMorphRange(morphBase + 6u);
    }
    let targets = morphBase + 3u * slots;
    // The list sits after the play records, and two of its eight-byte entries
    // share one vec4.
    let list = instance.animOffset + SCENE_ANIM_HEADER + header.x;
    var out = vertex;
    for (var i = 0u; i < targetCount; i = i + 1u) {
        let packed = sceneAnim.data[list + i / 2u];
        let entry = select(packed.zw, packed.xy, (i & 1u) == 0u);
        let head = targets + entry.x * 3u;
        // One compare, no search, no extra load. The subtraction is unsigned,
        // so a vertex below first wraps to a huge offset and fails the same
        // test the vertices above first + count fail.
        let local = vertexIndex - sceneMorphDeltas.data[head + 1u];
        if local >= sceneMorphDeltas.data[head + 2u] {
            continue;
        }
        let weight = bitcast<f32>(entry.y);
        // No base-vertex correction: gfx.MeshDescr owns its buffers and binds
        // them at offset 0, so vertexIndex is 0-based within the primitive, and
        // the target's base is already absolute in this buffer.
        let at = sceneMorphDeltas.data[head] + local * recordWords;
        let xy = unpack2x16snorm(sceneMorphDeltas.data[at]);
        let z = unpack2x16snorm(sceneMorphDeltas.data[at + 1u]);
        out.position = out.position + weight * vec3<f32>(xy, z.x) * positionRange;
        if recordWords > 2u {
            let normal = unpack4x8snorm(sceneMorphDeltas.data[at + 2u]);
            out.normal = out.normal + weight * normal.xyz * normalRange;
        }
        if recordWords > 3u {
            let tangent = unpack4x8snorm(sceneMorphDeltas.data[at + 3u]);
            out.tangent = vec4<f32>(
                out.tangent.xyz + weight * tangent.xyz * tangentRange,
                out.tangent.w,
            );
        }
    }
    return out;
}

//#endif
