// The opening of the material's uniform block. The block is composed from
// three sources so a custom shader can add numbers of its own to it: this one
// opens the struct, a fields source lists its members, and materialepilogue
// closes it and binds it. gfx fills one uniform block per shader, by member
// name per draw, so a shader's own per-draw numbers belong in this block rather
// than in a second one.
//
// DECLARES: struct ScenePbrMaterial, opened here and closed by
// materialepilogue.wgsl, which also declares the binding scenePbrMaterial.
//
// It is mounted at builtin/model/materialprologue.wgsl and published as
// model.MaterialProloguePath. material.wgsl includes it, then
// materialfields.wgsl, then materialepilogue.wgsl, so the bundled block is the
// PBR numbers alone. An extending shader includes this, its own fields source
// (which includes model.MaterialFieldsPath and adds members after it), and
// model.MaterialEpiloguePath - before it includes material.wgsl or anything
// that does. Includes are once per path, so material.wgsl's own three are then
// skipped and the block is declared once, with every member. The other order
// does not compile: the extension's members land outside any struct.
struct ScenePbrMaterial {
