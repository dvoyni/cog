// Package sceneimpl is the scene plugin: New, its Config, the flush that turns a
// tick's recorded scene.OpQueue into gfx passes and draws - model expansion,
// light selection, culling, sorting, material interning and instance packing -
// the handlers of the two-hop model load, and the Start mount of the bundled
// shaders. Only composition roots and tests import it; everything else reaches
// scene through its contract root.
//
// The plugin requires no Adapter and contributes none.
package sceneimpl
