// The deform path: morph, then skin, per the glTF order.
//#include ./instance.wgsl
//#include ./vertex.wgsl
//#include ./skin.wgsl
//#include ./morph.wgsl

// sceneDeformVertex runs linear blend skinning over the vertex's four
// influences, or hands the vertex back untouched for a draw with no skin.
//
// Normals take the precomputed normal matrix and then the blended rotation,
// never a shader inverse: the inverse bind can be non-orthonormal and so can
// the composed skinning matrix, and inverting it per vertex per influence is
// exactly the cost the load already paid once per joint. The tangent takes the
// same path plus Gram-Schmidt against the skinned normal, and its handedness
// follows the inverse bind's determinant sign, accumulated by weight so the
// answer is the majority influence's with no search.
fn sceneDeformVertex(
    instance: SceneInstance, vertexIndex: u32, vertex: SceneVertexIn,
) -> SceneVertex {
    // Morph, then skin, per the glTF order: the shapes reshape the mesh in its
    // own bind space and the skin then poses that.
//#if SCENE_MORPH
    let base = sceneMorphVertex(
        instance, vertexIndex,
        SceneVertex(vertex.position, vertex.normal, vertex.tangent),
    );
//#else
    let base = SceneVertex(vertex.position, vertex.normal, vertex.tangent);
//#endif
//#if SCENE_SKIN
    if (instance.flags & SCENE_NOSKIN) != 0u {
        return base;
    }
    let playCount = sceneAnimPlayCount(instance.animOffset);
    // A plain-bound placement - an animated mesh node, which is how glTF
    // authors a wheel or a door - names its one joint on the instance and
    // rides it at full weight. Its vertices carry no binding of their own, so
    // the loop below has nothing to read: it is a flag inside this path rather
    // than a fifth variant, and the variants stay four.
    if (instance.flags & SCENE_PLAINJOINT) != 0u {
        return scenePlainJointVertex(instance, playCount, base);
    }
    var position = vec3<f32>(0.0);
    var normal = vec3<f32>(0.0);
    var tangent = vec3<f32>(0.0);
    var handedness = 0.0;
    var total = 0.0;
    for (var influence = 0u; influence < 4u; influence = influence + 1u) {
        let weight = vertex.weights[influence];
        if weight == 0.0 {
            continue;
        }
        total = total + weight;
        let index = vertex.joints[influence];
        let joint = sceneSkinJoints.data[index];
        let pose = sceneBlendJoint(instance.animOffset, playCount, index);
        let skin = sceneJointMatrix(pose, joint);
        position = position + weight * (skin * vec4<f32>(base.position, 1.0));
        let normalMatrix = mat3x3<f32>(joint.normal0.xyz, joint.normal1.xyz, joint.normal2.xyz);
        normal = normal + weight * sceneQuatRotate(pose.rotation, normalMatrix * base.normal);
        tangent = tangent + weight * sceneQuatRotate(pose.rotation, normalMatrix * base.tangent.xyz);
        handedness = handedness + weight * joint.normal0.w;
    }
    // A vertex with no influence at all under a skinned draw is a malformed
    // file rather than a case to be correct about. It would collapse to the
    // origin with a zero normal, and normalize would turn that into a NaN that
    // spreads through the whole fragment stage, so it keeps its bind pose.
    if total == 0.0 {
        return base;
    }
    let skinnedNormal = normalize(normal);
    var skinnedTangent = base.tangent;
    let orthogonal = tangent - skinnedNormal * dot(skinnedNormal, tangent);
    if dot(orthogonal, orthogonal) > 1e-12 {
        skinnedTangent = vec4<f32>(normalize(orthogonal), base.tangent.w * sign(handedness));
    }
    return SceneVertex(position, skinnedNormal, skinnedTangent);
//#else
    return base;
//#endif
}
