// The bundled scene shader: the vertex stage and the fragment stage, each
// published whole, and the one-line fragment entry point that calls the latter.
// A custom scene shader is this file with its own fs_main.
//
// Group numbering is scene's frequency convention, expressed here because gfx
// binds whatever reflection reports and never renumbers: group 0 is per pass,
// group 1 per material, group 2 per model, group 3 reserved for a custom
// shader's own bindings. Ascending frequency, lowest group changing least.
//
// Every numeric input is a storage buffer. gfx's uniform path gives every draw
// its own pooled 256-byte buffer and its own upload, so a thousand draws would
// be a thousand buffers against a 256-byte cap; scene abandons it entirely and
// binds ranges of one per-frame arena instead.
//
// A declared binding must be bound at draw time, or gfx drops the draw. So this
// module declares exactly what it reads - which is why the skinning and morph
// bindings are declared only in the variants that read them, and why scene
// binds all five texture slots on every draw, a white texel and a flat normal
// where a material names none.
//
// Two defines pick the variant: SCENE_SKIN and SCENE_MORPH. There is still one
// module's worth of source - the BRDF is written once, so a shading fix cannot
// land in some copies and not others - and the variants differ only in which
// declarations survive.
//#include ./vertexstage.wgsl
//#include ./fragmentstage.wgsl

@fragment
fn fs_main(in: SceneVertexOut, @builtin(front_facing) frontFacing: bool) -> @location(0) vec4<f32> {
    return scenePbrFragment(in, frontFacing);
}
