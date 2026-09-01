package ui

import (
	"github.com/dvoyni/cog/canvas"
)

type frame struct {
	roots  []Element
	layers []canvas.Layer
}

// Add submits root on layer for the current update tick. The root value is
// copied; descendant slices remain borrowed until the UI plugin processes them.
func (frame *frame) Add(layer canvas.Layer, root ...Element) {
	frame.roots = append(frame.roots, root...)
	for range root {
		frame.layers = append(frame.layers, layer)
	}
}

func (frame *frame) clear() {
	frame.roots = frame.roots[:0]
	frame.layers = frame.layers[:0]
}
