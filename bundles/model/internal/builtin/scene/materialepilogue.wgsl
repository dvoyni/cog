// The close of the material's uniform block, and its binding.
//
// DECLARES: the binding scenePbrMaterial, the uniform block at @group(1)
// @binding(0), of the struct ScenePbrMaterial that materialprologue.wgsl opens.
//
// It is mounted at builtin/scene/materialepilogue.wgsl and published as
// model.MaterialEpiloguePath: an extending shader includes it after its own
// fields source. See materialprologue.wgsl for the composition.
};

@group(1) @binding(0) var<uniform> scenePbrMaterial: ScenePbrMaterial;
