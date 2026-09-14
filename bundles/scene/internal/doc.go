// Package internal is the scene plugin: New, the resolution of scene.Config,
// the flush that turns a tick's recorded scene.OpQueue into gfx passes and
// draws - model expansion, light selection, culling, sorting, material
// interning and instance packing - the handlers of the two-hop model load, and
// the Start mount of the bundled shaders. Composition roots and tests reach New
// through sceneplugin; everything else reaches scene through its root.
//
// The plugin requires no Adapter and contributes none.
package internal
