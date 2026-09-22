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
// Callers name model's types from here: no renderer re-exports them. What the
// root keeps, and why, is settled in bundles/model/docs/specs/model.md under
// "model's root after the sweep".
package model
