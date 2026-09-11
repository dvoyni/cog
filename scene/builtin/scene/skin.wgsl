// Linear blend skinning: the baked poses, the per-joint records, and the
// blend that turns an instance's plays into one joint transform.
//#include ./anim.wgsl
//#include ./instance.wgsl

//#if SCENE_SKIN

// ScenePose is the 48-byte baked pose record - three aligned vec4 loads -
// holding globalJoint alone, unpremultiplied by the inverse bind. Rotation is
// a unit quaternion as xyzw; translation and scale use xyz and leave w spare.
//
// Scale is a vec3 rather than a scalar in translation.w. Squash-and-stretch is
// animated non-uniform scale, a mainstream idiom, and unlike Transform there is
// no Matrix escape hatch to correct it at.
struct ScenePose {
    rotation: vec4<f32>,
    translation: vec4<f32>,
    scale: vec4<f32>,
};

struct ScenePoses {
    data: array<ScenePose>,
};

// SceneSkinJoint is the 112-byte per-joint record: the inverse bind and its
// normal matrix interleaved. They share a joint index and are fetched together
// on every influence, so one buffer is one address computation instead of two
// and lands both halves adjacent for all four influences.
//
// The columns are explicit vec4s rather than mat4x3 and mat3x3 because WGSL
// pads every matrix column to 16 bytes anyway - the record already carries 28
// bytes matrix syntax cannot address, and normal0.w spends four of them on the
// tangent handedness the bind pose's determinant decides.
struct SceneSkinJoint {
    inverseBind0: vec4<f32>,
    inverseBind1: vec4<f32>,
    inverseBind2: vec4<f32>,
    inverseBind3: vec4<f32>,
    normal0: vec4<f32>,
    normal1: vec4<f32>,
    normal2: vec4<f32>,
};

struct SceneSkinJoints {
    data: array<SceneSkinJoint>,
};

// Group 2 is per model: the baked poses and the per-joint records a skinned
// draw reads. They are declared only where SCENE_SKIN is, so a draw with no
// skin of its own declares no group 2 at all and has nothing to bind - which
// is what removes the whole class of failure where a declared binding goes
// unbound, CreateBindGroup fails, its error is swallowed, and the frame's
// command buffer vanishes with no error anywhere.
//
// Two bindings here, not three: the inverse bind and the normal matrix
// interleave above, which recovered a slot. Every reflected binding is emitted
// Vertex|Fragment unconditionally, so these count against the fragment stage
// too, and the budget they sit in is the browser core adapter's floor of eight
// storage buffers per stage.
@group(2) @binding(0) var<storage, read> scenePoses: ScenePoses;
@group(2) @binding(1) var<storage, read> sceneSkinJoints: SceneSkinJoints;

// SceneJointPose is one joint's blended transform before its inverse bind: the
// weighted mean of every playing frame's TRS.
struct SceneJointPose {
    rotation: vec4<f32>,
    translation: vec3<f32>,
    scale: vec3<f32>,
};

// sceneAnimPlayCount reads how many plays an instance is blending. An instance
// that animates nothing carries SCENE_NO_ANIM and blends none, which is the
// rest frame - a real pose rather than a collapse, because row 0 of every
// model is the authored hierarchy resolved once.
fn sceneAnimPlayCount(animOffset: u32) -> u32 {
    if animOffset == SCENE_NO_ANIM {
        return 0u;
    }
    return sceneAnim.data[animOffset].x;
}

// sceneBlendJoint accumulates one joint's pose across the instance's plays.
//
// The shader does no clip-length, wrap or normalisation arithmetic: the CPU
// folded weight * (1 - frac) and weight * frac into the two scalars and
// resolved the two rows, so this is a multiply and an add per frame. The bake
// fixed quaternion hemisphere continuity within each clip, so only the
// cross-play accumulation needs a sign check - two clips are two independent
// chains and may disagree.
fn sceneBlendJoint(animOffset: u32, playCount: u32, joint: u32) -> SceneJointPose {
    if playCount == 0u {
        // Row 0 is the rest frame, so the joint's own index is its row.
        let rest = scenePoses.data[joint];
        return SceneJointPose(rest.rotation, rest.translation.xyz, rest.scale.xyz);
    }
    var rotation = vec4<f32>(0.0);
    var translation = vec3<f32>(0.0);
    var scale = vec3<f32>(0.0);
    let base = animOffset + SCENE_ANIM_HEADER;
    for (var play = 0u; play < playCount; play = play + 1u) {
        let record = sceneAnim.data[base + play];
        let first = scenePoses.data[record.x + joint];
        let second = scenePoses.data[record.y + joint];
        let w0 = bitcast<f32>(record.z);
        let w1 = bitcast<f32>(record.w);
        var blended = first.rotation * w0 + second.rotation * w1;
        if dot(rotation, blended) < 0.0 {
            blended = -blended;
        }
        rotation = rotation + blended;
        translation = translation + first.translation.xyz * w0 + second.translation.xyz * w1;
        scale = scale + first.scale.xyz * w0 + second.scale.xyz * w1;
    }
    return SceneJointPose(normalize(rotation), translation, scale);
}

// sceneQuatRotate turns a vector by a unit quaternion in two cross products and
// no matrix build, which is what costs least when the rotation is used once -
// and a per-influence normal uses it once.
fn sceneQuatRotate(quaternion: vec4<f32>, vector: vec3<f32>) -> vec3<f32> {
    let axis = quaternion.xyz;
    let turned = cross(axis, vector) + quaternion.w * vector;
    return vector + 2.0 * cross(axis, turned);
}

fn sceneQuatBasis(quaternion: vec4<f32>) -> mat3x3<f32> {
    let x = quaternion.x;
    let y = quaternion.y;
    let z = quaternion.z;
    let w = quaternion.w;
    return mat3x3<f32>(
        vec3<f32>(1.0 - 2.0 * (y * y + z * z), 2.0 * (x * y + z * w), 2.0 * (x * z - y * w)),
        vec3<f32>(2.0 * (x * y - z * w), 1.0 - 2.0 * (x * x + z * z), 2.0 * (y * z + x * w)),
        vec3<f32>(2.0 * (x * z + y * w), 2.0 * (y * z - x * w), 1.0 - 2.0 * (x * x + y * y)),
    );
}

// sceneJointMatrix is one joint's fully blended transform with its inverse bind
// applied: the 4x3 that takes a bind-pose vertex into the model's own space.
//
// The inverse bind is applied here, after the cross-play blend, rather than
// premultiplied into the pose record at bake. Premultiplying injects the bind
// pose's routine non-uniform scale into a record that is then decomposed to
// TRS, which cannot represent shear at all.
fn sceneJointMatrix(pose: SceneJointPose, joint: SceneSkinJoint) -> mat4x3<f32> {
    let basis = sceneQuatBasis(pose.rotation);
    let scaled = mat3x3<f32>(
        basis[0] * pose.scale.x,
        basis[1] * pose.scale.y,
        basis[2] * pose.scale.z,
    );
    // The inverse bind's fourth row is (0, 0, 0, 1), so concatenating the two
    // 4x3s is three basis products plus one point transform.
    return mat4x3<f32>(
        scaled * joint.inverseBind0.xyz,
        scaled * joint.inverseBind1.xyz,
        scaled * joint.inverseBind2.xyz,
        scaled * joint.inverseBind3.xyz + pose.translation,
    );
}

// scenePlainJointVertex poses a vertex under the one joint its instance names,
// at full weight. It is the whole of what an animated mesh node needs, and it
// is what sceneDeformVertex's four-influence loop did for such a draw at four
// times the cost: three of its iterations hit a zero-weight continue, and the
// one that did the work fetched a constant joint index through a vertex
// attribute.
//
// The arithmetic is that loop's with the weight fixed at one, so the two agree
// exactly: the weighted sums collapse to their single terms, the normalize is
// the same normalize, and the handedness is this joint's own rather than a
// weighted majority of four.
fn scenePlainJointVertex(
    instance: SceneInstance, playCount: u32, base: SceneVertex,
) -> SceneVertex {
    let index = instance.joint;
    let joint = sceneSkinJoints.data[index];
    let pose = sceneBlendJoint(instance.animOffset, playCount, index);
    let skin = sceneJointMatrix(pose, joint);
    let position = skin * vec4<f32>(base.position, 1.0);
    let normalMatrix = mat3x3<f32>(joint.normal0.xyz, joint.normal1.xyz, joint.normal2.xyz);
    let normal = normalize(sceneQuatRotate(pose.rotation, normalMatrix * base.normal));
    let turned = sceneQuatRotate(pose.rotation, normalMatrix * base.tangent.xyz);
    var tangent = base.tangent;
    let orthogonal = turned - normal * dot(normal, turned);
    if dot(orthogonal, orthogonal) > 1e-12 {
        tangent = vec4<f32>(normalize(orthogonal), base.tangent.w * sign(joint.normal0.w));
    }
    return SceneVertex(position, normal, tangent);
}

//#endif
