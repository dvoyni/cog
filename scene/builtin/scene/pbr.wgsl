// The BRDF and the lighting loop: everything between a shaded surface and the
// radiance leaving it. Nothing here names anything scene-specific beyond the
// frame it reads its lights from.
//#include ./frame.wgsl

const SCENE_PI: f32 = 3.14159265359;
// SCENE_DIELECTRIC_F0 is the normal-incidence reflectance of a dielectric,
// which metallic lerps toward the base colour.
const SCENE_DIELECTRIC_F0: vec3<f32> = vec3<f32>(0.04, 0.04, 0.04);

// SceneSurface is what lighting needs and nothing more. It carries no view
// vector - that is one normalise away from sceneCameraPosition(), one less
// field to get wrong - and no emissive, because emissive is the material's own
// output rather than lighting: a shader writes sceneShadeSurface(s) + emissive.
struct SceneSurface {
    position: vec3<f32>,
    normal: vec3<f32>,
    baseColor: vec3<f32>,
    metallic: f32,
    roughness: f32,
    occlusion: f32,
};

// ScenePbrSurface is everything one set of texture fetches produces, returned
// in one call rather than through separate emissive and alpha helpers that
// would invite the same texture to be fetched two or three times.
struct ScenePbrSurface {
    surface: SceneSurface,
    emissive: vec3<f32>,
    alpha: f32,
};

// sceneD_GGX is the Trowbridge-Reitz microfacet distribution.
fn sceneD_GGX(nDotH: f32, alphaRoughness: f32) -> f32 {
    let alphaSquared = alphaRoughness * alphaRoughness;
    let f = nDotH * nDotH * (alphaSquared - 1.0) + 1.0;
    return alphaSquared / max(SCENE_PI * f * f, 1e-9);
}

// sceneV_SmithGGXCorrelated is Smith height-correlated visibility, which is the
// geometry term with the 1/(4 NdotL NdotV) denominator already folded in.
fn sceneV_SmithGGXCorrelated(nDotL: f32, nDotV: f32, alphaRoughness: f32) -> f32 {
    let alphaSquared = alphaRoughness * alphaRoughness;
    let lambdaV = nDotL * sqrt(nDotV * nDotV * (1.0 - alphaSquared) + alphaSquared);
    let lambdaL = nDotV * sqrt(nDotL * nDotL * (1.0 - alphaSquared) + alphaSquared);
    let sum = lambdaV + lambdaL;
    if sum <= 0.0 {
        return 0.0;
    }
    return 0.5 / sum;
}

// sceneF_Schlick is the Schlick Fresnel approximation.
fn sceneF_Schlick(f0: vec3<f32>, vDotH: f32) -> vec3<f32> {
    let scale = pow(clamp(1.0 - vDotH, 0.0, 1.0), 5.0);
    return f0 + (vec3<f32>(1.0, 1.0, 1.0) - f0) * scale;
}

// sceneEnvBRDFApprox is the analytic split-sum approximation of the environment
// specular term. It is what keeps ambient reaching metals: diffuseColor is
// baseColor * (1 - metallic), so a pure metal has zero diffuse and would render
// black everywhere the sun does not reach.
//
// Honest limitation: this approximates an environment that does not exist, so a
// mirror-smooth metal reflects a smooth gradient rather than the scene. Image-
// based lighting substitutes into exactly this term and the ambient diffuse one.
fn sceneEnvBRDFApprox(f0: vec3<f32>, roughness: f32, nDotV: f32) -> vec3<f32> {
    let c0 = vec4<f32>(-1.0, -0.0275, -0.572, 0.022);
    let c1 = vec4<f32>(1.0, 0.0425, 1.04, -0.04);
    let r = roughness * c0 + c1;
    let a004 = min(r.x * r.x, exp2(-9.28 * nDotV)) * r.x + r.y;
    let scaleBias = vec2<f32>(-1.04, 1.04) * a004 + r.zw;
    return f0 * scaleBias.x + vec3<f32>(scaleBias.y, scaleBias.y, scaleBias.y);
}

// scenePunctualContribution is the Khronos reference BRDF for one light:
// Lambert diffuse weighted by 1 - F, plus GGX specular.
fn scenePunctualContribution(
    light: SceneLightSample, normal: vec3<f32>, view: vec3<f32>, nDotV: f32,
    diffuseColor: vec3<f32>, f0: vec3<f32>, alphaRoughness: f32,
) -> vec3<f32> {
    let toLight = light.direction;
    let nDotL = dot(normal, toLight);
    if nDotL <= 0.0 {
        return vec3<f32>(0.0, 0.0, 0.0);
    }
    let halfway = normalize(toLight + view);
    let nDotH = clamp(dot(normal, halfway), 0.0, 1.0);
    let vDotH = clamp(dot(view, halfway), 0.0, 1.0);
    let fresnel = sceneF_Schlick(f0, vDotH);
    let diffuse = (vec3<f32>(1.0, 1.0, 1.0) - fresnel) * diffuseColor / SCENE_PI;
    let specular = fresnel *
        sceneV_SmithGGXCorrelated(nDotL, nDotV, alphaRoughness) *
        sceneD_GGX(nDotH, alphaRoughness);
    return (diffuse + specular) * light.radiance * nDotL;
}

// sceneShadeSurface is the sun, every punctual light in the pass, and
// hemispheric ambient scaled by the surface's occlusion. The light loop is
// naive forward: every shaded fragment runs it whole, so each light in the
// pass costs every shaded pixel one BRDF evaluation.
fn sceneShadeSurface(s: SceneSurface) -> vec3<f32> {
    let view = normalize(sceneCameraPosition() - s.position);
    let nDotV = clamp(dot(s.normal, view), 1e-4, 1.0);
    let metallic = clamp(s.metallic, 0.0, 1.0);
    let roughness = clamp(s.roughness, 0.0, 1.0);
    let alphaRoughness = roughness * roughness;
    let diffuseColor = s.baseColor * (1.0 - metallic);
    let f0 = mix(SCENE_DIELECTRIC_F0, s.baseColor, metallic);

    var shaded = scenePunctualContribution(
        sceneSun(), s.normal, view, nDotV, diffuseColor, f0, alphaRoughness);
    for (var i = 0u; i < sceneLightCount(); i++) {
        shaded += scenePunctualContribution(
            sceneLightSample(i, s.position), s.normal, view, nDotV, diffuseColor, f0, alphaRoughness);
    }

    // Ambient splits two ways, both scaled by occlusion, because the diffuse
    // half alone leaves a metal black.
    let ambientDiffuse = sceneAmbient(s.normal) * diffuseColor;
    let ambientSpecular = sceneAmbient(reflect(-view, s.normal)) *
        sceneEnvBRDFApprox(f0, roughness, nDotV);
    shaded += (ambientDiffuse + ambientSpecular) * s.occlusion;
    return shaded;
}
