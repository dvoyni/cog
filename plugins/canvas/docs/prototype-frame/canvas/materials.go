package canvas

import (
	"reflect"

	"github.com/dvoyni/cog/libs/m"
	"prototype.invalid/canvasframe/render"
)

// MaterialDescriptor describes a 2D material.
type MaterialDescriptor struct {
	Label string
	// Shader is the path of a whole WGSL file that includes canvas's
	// "cog.canvas/canvas.wgsl", which declares every binding of @group(0) and
	// @group(2); the shader declares only @group(1), its parameters, and the
	// struct of its argument, named Argument.
	Shader string
	// VertexEntryPoint may be empty: the material then uses canvas's vertex
	// stages, a sprite's and a canvas.Vertex mesh's.
	VertexEntryPoint, FragmentEntryPoint string
	// Blend is premultiplied; zero means BlendNormal().
	Blend render.BlendState
	// Argument is the type each entry gives the material, zero meaning none.
	Argument ArgumentType
	// CullMargin grows its entries' bounds, in world units, for a vertex stage
	// that moves vertices.
	CullMargin float32
}

// ArgumentType is the type of a material's argument.
type ArgumentType struct{ goType reflect.Type }

// ArgumentOf answers T as a material's argument type: a struct of any number
// of fields, or a plain value. It is checked field by field against the
// shader's Argument struct.
func ArgumentOf[T any]() ArgumentType { return ArgumentType{reflect.TypeFor[T]()} }

// The materials canvas ships, beside the zero Material, the default one
// (texture × tint).
var (
	// SDFMaterial draws a distance field through a Profile, its argument.
	SDFMaterial = Material{id: 1}
	// TextMaterial is a run's when its Material is zero; its argument is the
	// Profile drawn under the fill, zero drawing none.
	TextMaterial = Material{id: 2}
)

// Profile maps a distance to a colour: an outline's hard band, a halo's
// plateau and falloff, a glow's falloff, or a drop shadow's offset band, in
// field texels, its offset in world space.
type Profile struct {
	Inner, Outer, Plateau, Falloff float32
	Color                          m.Color
	Offset                         m.Vec2
}

// Outline answers a hard band width wide outside the edge.
func Outline(width float32, color m.Color) Profile {
	return Profile{Outer: width, Falloff: 0, Color: color}
}

// Halo answers a band solid to plateau, falling off to reach.
func Halo(plateau, reach float32, color m.Color) Profile {
	return Profile{Outer: reach, Plateau: plateau, Falloff: 2, Color: color}
}

// Glow answers a falloff from the edge to reach, added to what is under it.
func Glow(reach float32, color m.Color) Profile {
	return Profile{Outer: reach, Falloff: 1, Color: color}
}

// DropShadow answers a soft band offset in world space.
func DropShadow(offset m.Vec2, softness float32, color m.Color) Profile {
	return Profile{Outer: softness, Falloff: 1, Color: color, Offset: offset}
}

// The blend modes, premultiplied.
func BlendNormal() render.BlendState {
	return blend(render.BlendFactorOne, render.BlendFactorOneMinusSrcAlpha)
}
func BlendAdditive() render.BlendState { return blend(render.BlendFactorOne, render.BlendFactorOne) }
func BlendMultiply() render.BlendState {
	return blend(render.BlendFactorDst, render.BlendFactorOneMinusSrcAlpha)
}
func BlendScreen() render.BlendState {
	return blend(render.BlendFactorOne, render.BlendFactorOneMinusSrc)
}
func BlendOpaque() render.BlendState { return blend(render.BlendFactorOne, render.BlendFactorZero) }

func blend(source, destination render.BlendFactor) render.BlendState {
	component := render.BlendComponent{Operation: render.BlendOperationAdd, SrcFactor: source, DstFactor: destination}
	return render.BlendState{Color: component, Alpha: component}
}

// Assets is canvas's parameter for what a task makes, loads and unloads,
// as render's Inventory is render's: it takes canvas's tables for read, each
// locked within.
type Assets struct{}

// MakeMaterial answers a 2D material of descriptor. canvas makes the render
// material underneath, which its users never hold.
func (Assets) MakeMaterial(descriptor MaterialDescriptor) Material {
	return tables.makeMaterial(descriptor)
}

// ReleaseMaterial gives material back.
func (Assets) ReleaseMaterial(material Material) {}

// SetMaterialTexture sets material's @group(1) texture name. A draw uses the
// material's parameters as they stand when canvas draws, at the frame's end.
func (Assets) SetMaterialTexture(material Material, name string, texture render.Texture) {}

// SetMaterialUniform sets material's @group(1) uniform name, such as the time
// a swaying material animates by, from whichever clock owns it.
func (Assets) SetMaterialUniform[T any](material Material, name string, value T) {}

// Preload loads and packs image now, as a loading screen does, rather than at
// its first draw.
func (Assets) Preload(image Image) { tables.loadImage(image) }

// Unload unloads every image and mesh file at path or under it, a prefix
// ending in "/", such as "level1/". A later draw loads it again.
func (Assets) Unload(path string) { tables.unload(path) }

// ImageSize answers image's size in texels, its frame's for a sheet, read
// from the file's header alone, and whether the file was found.
func (Assets) ImageSize(image Image) (m.Vec2, bool) {
	return tables.images[image.id].frameSize(), true
}

// RequestInspection asks for the next frame canvas draws, and render's of it.
func (Assets) RequestInspection() {}

// ReloadChanged reloads every image, font file and mesh file whose file
// changed, and render's shaders.
func (Assets) ReloadChanged() {}
