// Package types declares the concrete types model's root aliases, and the
// machinery behind them: the authoring vertex and the storage layout it packs
// into, the unit geometry generators, and the glTF decoder's output and
// reports, which the decoder in internal/types/gltf declares and this package
// names.
//
// Go allows nothing outside bundles/model to import this package. Nothing
// declared here imports the root, which is what keeps the arrangement acyclic.
package types
