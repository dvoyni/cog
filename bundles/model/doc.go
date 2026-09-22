// Package model is the plugin that owns everything a model file can contain:
// glTF decode, geometry generation, vertex and morph packing, GPU upload, the
// model and texture caches, and the one mesh table every renderer draws from.
// Its plugin, constructed by modelplugin.New, registers the *Lookup resource,
// takes Config under Name, and mounts the bundled PBR shader's sources in
// storage as the StorageReadMount Adapter. Register it after storage and before
// any renderer.
//
// Every record the bundled shader reads is declared here with its packer and
// its size, and each binding name is a constant, so that any renderer drawing
// through the shader writes the same bytes. A renderer keeps its own arenas,
// culling, sorting and emission, and appends what the packers return.
//
// The carve out of scene is in progress (bundles/model/docs/specs/model.md):
// some of what the root exports - the Lookup's methods a renderer's flush
// calls among them - is here because scene has to name it, and its final
// shape lands with the stages that follow.
package model
