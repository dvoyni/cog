// Package gltf is model's glTF decoder: one glTF or GLB file in, plain data
// out.
//
// The seam is thin. The decoder hands over vertex attribute arrays and index
// lists, unbaked animation curves and skins, morph target floats, material
// parameters as plain values, image references, lights, and the flattened
// scene walk. It does no GPU-layout work: packing vertices into the storage
// layout, baking clips onto the pose grid, packing morph blocks and filling the
// PBR record all happen in model, after the decoder returns. Vertex data
// crosses as the glTF library's own typed slices, structure of arrays, handed
// over untouched wherever the file already stored floats.
//
// It names images and never decodes one, so it does not import libs/assets:
// each distinct picture and colour space is one plain reference, and model keys
// its texture cache by it.
//
// The decoder imports only libs/m, qmuntal/gltf and slots/gfx, for the sampler
// and topology enums. It declares its own report types, which model re-exports
// under the names scene has always given them. Nothing in model's internal/types
// is imported here; that package imports this one.
package gltf
