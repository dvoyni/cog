// The storage vertex's decode: how the four bytes of normal and the four bytes
// of tangent become directions.
//
// DECLARES: fn sceneOctDecode, sceneDecodeNormal, sceneDecodeTangent; const
// SCENE_OCT_TANGENT_MAX, SCENE_TANGENT_Y_SHIFT, SCENE_TANGENT_HANDEDNESS. No
// binding and no struct, so it is includable by any material, extending or not.
//
// It is mounted at builtin/scene/vertexdecode.wgsl and published as
// scene.VertexDecodePath, so a custom scene material includes it by that
// absolute storage name and reads the directions the bundled shader reads
// rather than a re-typed approximation. A shader that declares
// @location(1) as vec3<f32> or @location(2) as vec4<f32> - which is what the
// float layout wanted - is refused at pipeline time now, because gfx requires a
// declared input's type to equal what the layout supplies.
//
// The encode is Go, at scene/vertexoct.go, and the two are inverses of one
// another with nothing holding them together but arithmetic small enough to
// read: three lines, no branches, no table. scene/vertexoct_test.go carries a
// transcription of this file and measures the round trip.
//
// Where it runs is contract too: at the TOP of the vertex stage, before morph
// and before skin. It cannot be folded anywhere cheaper - not into the world
// matrix, whose basis also derives the world normal; not into the joint
// matrices, which are per model where an encoding is per vertex; and not after
// the deform, which adds morph deltas and applies joints in mesh space.

// The tangent's 32-bit word: 15 bits of octahedral x from bit 0, 15 of y from
// bit 15, handedness at bit 30, and bit 31 reserved and never read. Handedness
// rides in the vertex because a primitive is allowed to carry both signs.
const SCENE_OCT_TANGENT_MAX: f32 = 32767.0;
const SCENE_TANGENT_Y_SHIFT: u32 = 15u;
const SCENE_TANGENT_HANDEDNESS: u32 = 0x40000000u;

// sceneOctDecode turns a point on the unfolded octahedron, both components in
// [-1, 1], back into the unit direction it names.
//
// The third component is what is left of the L1 norm; where that is negative
// the point is on the folded-out lower half, and folding it back is the same
// (1 - |other|) * sign the encode applied. Written as an add of a clamped
// amount rather than as a branch, which is the same arithmetic with nothing to
// diverge on.
//
// The octahedral origin is the one input worth naming: it decodes to +Z, which
// is where an unwritten normal or an unwritten tangent lands. No NaN, and no
// special case to produce one - normalize is never handed a zero vector,
// because the third component is 1 wherever the first two are 0.
fn sceneOctDecode(folded: vec2<f32>) -> vec3<f32> {
    var direction = vec3<f32>(folded, 1.0 - abs(folded.x) - abs(folded.y));
    let fold = max(-direction.z, 0.0);
    direction.x = direction.x - select(-fold, fold, direction.x >= 0.0);
    direction.y = direction.y - select(-fold, fold, direction.y >= 0.0);
    return normalize(direction);
}

// sceneDecodeNormal decodes @location(1): oct32 in a two-component 16-bit
// unorm, which the fetch unit has already divided by 65535 for us, so the only
// step left is the unorm-to-signed remap.
fn sceneDecodeNormal(stored: vec2<f32>) -> vec3<f32> {
    return sceneOctDecode(stored * 2.0 - 1.0);
}

// sceneDecodeTangent decodes @location(2): one 32-bit word, arriving as a u32
// because nothing in the fetch unit can unpack 15-bit fields. The xyz is the
// octahedral direction and w is +1 or -1, the handedness the fragment stage
// multiplies the bitangent by.
//
// This is where the normal and the tangent stop being symmetric: one decodes
// from a float pair and one by bit extraction. That asymmetry is the price of
// not paying four more bytes for a matching format, and it is deliberate.
fn sceneDecodeTangent(stored: u32) -> vec4<f32> {
    let x = f32(stored & 0x7fffu) / SCENE_OCT_TANGENT_MAX * 2.0 - 1.0;
    let y = f32((stored >> SCENE_TANGENT_Y_SHIFT) & 0x7fffu) / SCENE_OCT_TANGENT_MAX * 2.0 - 1.0;
    let handedness = select(-1.0, 1.0, (stored & SCENE_TANGENT_HANDEDNESS) != 0u);
    return vec4<f32>(sceneOctDecode(vec2<f32>(x, y)), handedness);
}
