// Package model is the plugin that owns everything a model file can contain:
// glTF decode, geometry generation, vertex and morph packing, GPU upload, the
// model and texture caches, and the one mesh table every renderer draws from.
// Its plugin, constructed by modelplugin.New, registers the *Lookup resource,
// takes Config under Name, and mounts the bundled PBR shader's sources in
// storage as the StorageReadMount Adapter. Register it after storage and before
// any renderer.
//
// The carve out of scene is in progress (bundles/model/docs/specs/model.md):
// some of what the root exports - the bundled shader's records, their sizes
// and packers, and the Lookup's methods a renderer's flush calls - is here
// because scene has to name it, and its final shape lands with the stages
// that follow.
package model
