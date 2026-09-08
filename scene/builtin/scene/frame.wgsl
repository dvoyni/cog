// The per-pass view of the world: the camera, the sun, the hemispheric
// ambient and the punctual lights, plus the accessors that read them.

// SCENE_MAX_LIGHTS is how many punctual lights one pass may carry. It is
// declared here, in the source that owns the array, as this module's default;
// scene supplies its own cap over the top, which is what keeps scene.maxLights
// and this array in step instead of pinned together by a test.
//#const SCENE_MAX_LIGHTS=16

// SceneFrame is one pass's view of the world. View and projection ride along
// beside their product because a shader wanting view-space depth cannot
// recover them from viewProjection.
//
// The sun and hemispheric ambient are per-camera fields rather than entries in
// a light array: packing the sun as a directional entry would cost an explicit
// discriminator and waste position, range and cone on it, and hemispheric
// ambient is normal-dependent rather than a direction, so it could never join
// the loop anyway. Every colour here is linear radiance with its intensity
// already premultiplied.
//
// The punctual lights are naive forward: lightCount bounds a loop every shaded
// fragment runs whole, so adding a light costs every shaded pixel in the pass.
// The array is sized by SCENE_MAX_LIGHTS, which scene supplies from its own
// cap, so the two cannot drift. Nothing is reserved for shadows: no sun
// matrix, no comparison sampler.
struct SceneFrame {
    view: mat4x4<f32>,
    projection: mat4x4<f32>,
    viewProjection: mat4x4<f32>,
    cameraPosition: vec4<f32>,
    sunDirection: vec4<f32>,
    sunColor: vec4<f32>,
    ambientSky: vec4<f32>,
    ambientGround: vec4<f32>,
    lightCount: u32,
    lights: array<SceneLight, SCENE_MAX_LIGHTS>,
};

// SceneLight is one punctual light, 48 bytes, with no kind field. A point light
// is a spot whose cone is always on: direction an actual zero vector, spotScale
// 0 and spotOffset 1, so the cone term below is saturate(0 + 1). That relies on
// x * 0 == 0, which is false for NaN, which is why the direction is a real
// zero and never left uninitialised.
//
// invRange4 is 1/range^4, and 0 for an infinite range: saturate(1 - d^4 * 0)
// is exactly 1, so infinity costs no branch and no select. color is linear
// radiance with the light's intensity already premultiplied.
struct SceneLight {
    position: vec3<f32>,
    invRange4: f32,
    direction: vec3<f32>,
    spotScale: f32,
    color: vec3<f32>,
    spotOffset: f32,
};

// SceneLightSample is one light's contribution at a point: the direction from
// the surface towards the light, and the radiance arriving along it.
struct SceneLightSample {
    direction: vec3<f32>,
    radiance: vec3<f32>,
};

@group(0) @binding(0) var<storage, read> sceneFrame: SceneFrame;

fn sceneCameraPosition() -> vec3<f32> {
    return sceneFrame.cameraPosition.xyz;
}

// sceneAmbient is the hemispheric ambient a normal sees: ground below, sky
// above. It is the one light term that is normal-dependent rather than
// directional, which is why it can never be an entry in the light array.
fn sceneAmbient(normal: vec3<f32>) -> vec3<f32> {
    return mix(sceneFrame.ambientGround.rgb, sceneFrame.ambientSky.rgb, normal.y * 0.5 + 0.5);
}

// sceneSun is the directional light every camera carries. Its radiance is zero
// when the camera declared no sun direction, so the term costs a multiply by
// black rather than a branch.
fn sceneSun() -> SceneLightSample {
    return SceneLightSample(-sceneFrame.sunDirection.xyz, sceneFrame.sunColor.rgb);
}

// sceneLightCount is how many punctual lights this pass carries, after scene
// culled them against the pass's frustum and capped them at 16.
fn sceneLightCount() -> u32 {
    return sceneFrame.lightCount;
}

// sceneLightSample is one punctual light's contribution at a point: glTF's
// falloff geometry, branchless. The range window is exactly 1 for an infinite
// range because invRange4 is 0 there; the cone is exactly 1 for a point light
// because its direction is zero and spotOffset is 1. Intensity is unitless -
// radiance at one world unit - so the inverse square lands in 0..1 directly.
//
// max(d2, 1e-6) is a robustness guard for a light sitting on the surface, not
// a falloff parameter. Scene's cap ranks lights by this same expression
// evaluated at the eye, so the two must stay in step.
fn sceneLightSample(i: u32, position: vec3<f32>) -> SceneLightSample {
    let light = sceneFrame.lights[i];
    let toLight = light.position - position;
    let d2 = dot(toLight, toLight);
    let direction = toLight * inverseSqrt(max(d2, 1e-12));
    let window = saturate(1.0 - d2 * d2 * light.invRange4);
    let cone = saturate(dot(-direction, light.direction) * light.spotScale + light.spotOffset);
    return SceneLightSample(direction, light.color * (window * cone / max(d2, 1e-6)));
}
