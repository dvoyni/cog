// Package handle holds render's handle types, which render names with
// public aliases, the slot map that hands out their slots, and the functions
// that write a handle into a gfx object's label and read it back, which only
// render and render/rendertest reach: rendertest's reader makes the handle it
// finds behind a gfx object from the label render gave that object.
package handle

import (
	"fmt"
	"strings"
)

// handle is what every one of render's handles is: a slot in a SlotMap and
// the generation the slot was handed out under, never zero, so the zero
// handle names none. Each handle type embeds it, so they stay distinct types.
type handle struct{ index, generation uint32 }

// Handle is a handle type: one embedding handle alone, with the kind of asset
// it names in the labels of its gfx objects.
type Handle interface {
	~struct{ handle }
	// labelKind answers the kind of asset the handle names, such as
	// "texture", in the labels of its gfx objects.
	labelKind() string
}

// Texture names one of render's textures: pointer-free and comparable, its
// zero value naming none.
type Texture struct{ handle }

// labelKind answers "texture", the kind its gfx textures' labels name.
func (Texture) labelKind() string { return "texture" }

// Buffer names one of render's buffers: pointer-free and comparable, its
// zero value naming none.
type Buffer struct{ handle }

// labelKind answers "buffer", the kind its gfx buffers' labels name.
func (Buffer) labelKind() string { return "buffer" }

// Shader names one of render's shaders: pointer-free and comparable, its
// zero value naming none.
type Shader struct{ handle }

// labelKind answers "shader", the kind its gfx shader modules' labels name.
func (Shader) labelKind() string { return "shader" }

// Mesh names one of render's meshes: pointer-free and comparable, its zero
// value naming none.
type Mesh struct{ handle }

// labelKind answers "mesh", the kind a mesh's reports name.
func (Mesh) labelKind() string { return "mesh" }

// Material names one of render's materials: pointer-free and comparable, its
// zero value naming none.
type Material struct{ handle }

// labelKind answers "material", the kind a material's reports name.
func (Material) labelKind() string { return "material" }

// MakeLabel answers the label of the gfx object behind h, naming h and then
// label, the one its user gave.
func MakeLabel[H Handle](h H, label string) string {
	key := struct{ handle }(h).handle
	return fmt.Sprintf("render %s %d.%d: %s", h.labelKind(), key.index, key.generation, label)
}

// ParseLabel answers the handle a gfx object's label names, and false for a
// label MakeLabel did not make for an H.
func ParseLabel[H Handle](label string) (H, bool) {
	var h H
	rest, found := strings.CutPrefix(label, "render "+h.labelKind()+" ")
	if !found {
		return h, false
	}
	var key handle
	if _, err := fmt.Sscanf(rest, "%d.%d", &key.index, &key.generation); err != nil {
		return h, false
	}
	return H{key}, true
}
