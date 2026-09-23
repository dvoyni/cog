// Package types declares the concrete types model's root aliases, and the
// machinery behind them: the authoring vertex and the storage layout it packs
// into, the unit geometry generators, and the glTF decoder's output and
// reports, which the decoder in internal/types/gltf declares and this package
// names; the Lookup with its two scoped facades, Config, which the Lookup
// holds, the model and texture caches with their loaders and unloads, and the
// conversion of the decoded glTF into vertices, baked poses, morph blocks and
// PBR numbers behind them; the mesh table with its minting, staging and
// deferred bakes, and the unit meshes; the bundled PBR material, which names
// no pass; the per-frame animation and morph resolution a renderer packs
// from; and every record the bundled shader reads, with its packer - the
// frame block and its lighting, the instance, the light and the pass's light
// selection, and the sceneAnim block - so that two renderers write the same
// bytes.
//
// A renderer reaches the Lookup's residency through the Lookup's own exported
// methods - ModelView, Mesh, EnsureUnit, EnsureBundled and DrainMeshes - and a
// MeshRef's through Source, Index and Generation, because nothing outside
// bundles/model can import this package.
//
// Go allows nothing outside bundles/model to import this package. Nothing
// declared here imports the root, which is what keeps the arrangement acyclic.
package types
