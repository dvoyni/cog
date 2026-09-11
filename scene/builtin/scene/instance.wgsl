// The per-instance record and the transforms that take a vertex from the
// model's own space to the world.

// SceneInstance is the 64-byte per-instance record. world0..world2 are the rows
// of the 4x3 world matrix, translation in w; the fourth row of an affine
// transform is known and is not sent.
struct SceneInstance {
    world0: vec4<f32>,
    world1: vec4<f32>,
    world2: vec4<f32>,
    animOffset: u32,
    flags: u32,
    // joint is the model joint a plain-bound placement rides at full weight,
    // and is meaningful only under SCENE_PLAINJOINT. It spent the first of the
    // record's two spare words; spare is the one that is left.
    joint: u32,
    spare: u32,
};

// The runtime array is wrapped in a struct because reflection walks struct
// members: a global typed as a bare array is not reported, and an unreported
// binding is an unbound one.
struct SceneInstances {
    data: array<SceneInstance>,
};

@group(0) @binding(1) var<storage, read> sceneInstances: SceneInstances;

// SCENE_NONUNIFORM marks an instance whose world matrix does not scale
// uniformly. Transforming a normal by such a matrix is wrong, so those
// instances - and only those - pay for an inverse-transpose. The branch is
// uniform across the whole instance.
const SCENE_NONUNIFORM: u32 = 1u;
// SCENE_NOSKIN marks a draw with no skin of its own. It is set on every
// buffer-built draw, which is every draw this shader can be asked to make.
const SCENE_NOSKIN: u32 = 2u;
// SCENE_PLAINJOINT marks a placement bound to the joint in `joint` at full
// weight - an animated mesh node, which is how glTF authors a wheel, a door or
// a propeller. The vertices carry no binding of their own, so the geometry
// under it is the geometry every other node referencing that mesh draws.
//
// It is mutually exclusive with SCENE_NOSKIN: a plain-bound placement is a
// skinned draw, and the packer sets one flag or neither.
const SCENE_PLAINJOINT: u32 = 4u;
// SCENE_NO_ANIM in animOffset means the instance animates nothing.
const SCENE_NO_ANIM: u32 = 0xffffffffu;

// sceneWorldPosition transforms a local position by the instance's 4x3 world
// matrix.
fn sceneWorldPosition(instance: SceneInstance, position: vec3<f32>) -> vec3<f32> {
    let local = vec4<f32>(position, 1.0);
    return vec3<f32>(
        dot(instance.world0, local),
        dot(instance.world1, local),
        dot(instance.world2, local),
    );
}

// sceneWorldBasis is the instance's world matrix without its translation.
//
// world0..world2 are rows and mat3x3's arguments are columns, so the basis is
// transposed into place here rather than passed straight through. Passing them
// through returns M-transpose, which for the rotation most draws carry is the
// inverse rotation - normals that counter-rotate, on every draw whose basis is
// not symmetric.
fn sceneWorldBasis(instance: SceneInstance) -> mat3x3<f32> {
    return mat3x3<f32>(
        vec3<f32>(instance.world0.x, instance.world1.x, instance.world2.x),
        vec3<f32>(instance.world0.y, instance.world1.y, instance.world2.y),
        vec3<f32>(instance.world0.z, instance.world1.z, instance.world2.z),
    );
}

// sceneWorldNormal transforms a local normal. The record carries no normal
// matrix - it would double the record for a case most instances do not have -
// so the inverse-transpose is derived here, for the instances that flagged it.
//
// A uniform scale leaves the normal matrix parallel to the basis, and the
// normalize below discards the scale that separates them, so the flag buys the
// cofactors only where they change a direction.
fn sceneWorldNormal(instance: SceneInstance, normal: vec3<f32>) -> vec3<f32> {
    let basis = sceneWorldBasis(instance);
    if (instance.flags & SCENE_NONUNIFORM) != 0u {
        return normalize(sceneInverseTranspose3(basis) * normal);
    }
    return normalize(basis * normal);
}

// sceneWorldTangent transforms a local tangent. A tangent lies in the surface
// rather than across it, so it rides the plain basis even under non-uniform
// scale; the fragment stage re-orthogonalises it against the normal.
fn sceneWorldTangent(instance: SceneInstance, tangent: vec4<f32>) -> vec4<f32> {
    let world = sceneWorldBasis(instance) * tangent.xyz;
    return vec4<f32>(world, tangent.w);
}

// sceneInverseTranspose3 returns the normal matrix for a basis: the inverse
// transposed, by cofactors.
//
// The transpose costs nothing and must not be applied again by the caller. The
// three cross products below are the adjugate's rows, and the adjugate over the
// determinant is the inverse; writing them as mat3x3's columns instead is what
// transposes it, so the result is already inverse-transpose.
//
// A basis too flat to invert has no normal matrix at all. It returns the basis
// unchanged there - the wrong matrix, but a finite one, where the cofactors
// over a zero determinant would hand every downstream normalize a NaN.
fn sceneInverseTranspose3(basis: mat3x3<f32>) -> mat3x3<f32> {
    let a = basis[0];
    let b = basis[1];
    let c = basis[2];
    let cofactor0 = cross(b, c);
    let cofactor1 = cross(c, a);
    let cofactor2 = cross(a, b);
    let determinant = dot(a, cofactor0);
    if abs(determinant) < 1e-12 {
        return basis;
    }
    return mat3x3<f32>(cofactor0, cofactor1, cofactor2) * (1.0 / determinant);
}
