package internal

import "github.com/dvoyni/cog/bundles/canvas"

// The friend functions: what ui's internal/, and the tests, read from or do
// to a public type's unexported state. Only ui's root and internal/ can import
// this package, so these are not public API. Each is a field read or a direct
// call, so processing pays nothing for going through one.

// FrameRoots reads Frame.roots for the plugin: the roots declared this tick, in
// declaration order.
func FrameRoots(frame *Frame) []Element { return frame.roots }

// FrameLayers reads Frame.layers for the plugin: the base layer of the root at the
// same index of FrameRoots.
func FrameLayers(frame *Frame) []canvas.Layer { return frame.layers }

// FrameMaterials reads Frame.materials for the plugin: the material set every root
// of this tick inherits.
func FrameMaterials(frame *Frame) canvas.MaterialSet { return frame.materials }

// ClearFrame calls Frame.clear for the plugin, which consumes the frame at the end
// of every tick while keeping its capacity.
func ClearFrame(frame *Frame) { frame.clear() }

// PublishInteractions hands the interactions processor resolved this tick to
// interactions, and takes interactions' previous buffer back for the next tick,
// so neither side allocates.
func PublishInteractions(processor *Processor, interactions *Interactions) {
	interactions.values, processor.interactions = processor.interactions, interactions.values
}
