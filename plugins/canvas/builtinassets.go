package canvas

import (
	"embed"
	"io/fs"
)

const (
	// assetPrefix is the folder of platform's FS canvas's built-in assets
	// appear in, where a 2D material's shader includes canvas's include
	// from, as "cog.canvas/canvas.wgsl".
	assetPrefix = "cog.canvas"
	// defaultShaderPath is the path in platform's FS of the default
	// material's shader.
	defaultShaderPath = assetPrefix + "/default.wgsl"
	// sdfShaderPath is the path in platform's FS of SDFMaterial's shader,
	// and sdfVertexEntryPoint and sdfFragmentEntryPoint its stages: a
	// vertex stage growing a shape's quad by its profile's reach, and the
	// band drawn.
	sdfShaderPath         = assetPrefix + "/sdf.wgsl"
	sdfVertexEntryPoint   = "canvas_sdf_vertex"
	sdfFragmentEntryPoint = "canvas_sdf_fragment"
	// textShaderPath is the path in platform's FS of TextMaterial's shader,
	// textVertexEntryPoint its vertex stage, placing small text on whole
	// pixels, and textFragmentEntryPoint its fragment stage, drawing a
	// glyph's fill over its profile's band.
	textShaderPath         = assetPrefix + "/text.wgsl"
	textVertexEntryPoint   = "canvas_text_vertex"
	textFragmentEntryPoint = "canvas_text_fragment"
	// viewportShaderPath is the path in platform's FS of the shader of the
	// quads canvas draws over a viewport: a camera's clear, what a
	// multisampled camera resolved, and an image filling its atlas cell.
	viewportShaderPath = assetPrefix + "/viewport.wgsl"
	// canvasNamePrefix begins every name canvas's include and its
	// materials' shaders declare, their vertex stages among them, so a
	// material's shader names none of its own with it.
	canvasNamePrefix = "canvas_"
	// spriteVertexEntryPoint is the include's vertex stage, for an entry
	// drawn as a quad.
	spriteVertexEntryPoint = "canvas_sprite_vertex"
	// meshVertexEntryPoint is the include's vertex stage for a mesh of
	// canvas.Vertex.
	meshVertexEntryPoint = "canvas_mesh_vertex"
	// viewportVertexEntryPoint is the include's vertex stage for a quad
	// covering the camera's viewport, which a camera's clear drawn over its
	// viewport draws through.
	viewportVertexEntryPoint = "canvas_viewport_vertex"
	// viewportFragmentEntryPoint is the viewport shader's fragment stage
	// for a camera's quads, its clear and what it resolved, and
	// packFragmentEntryPoint its fragment stage filling an atlas's cell
	// with an image premultiplied.
	viewportFragmentEntryPoint = "canvas_viewport_fragment"
	packFragmentEntryPoint     = "canvas_pack_fragment"
	// instancesBindingName is the include's storage binding of the instance
	// data, each element canvas's fixed part then, where the material takes
	// one, its argument.
	instancesBindingName = "canvas_instances"
	// argumentDefine is the define canvas preprocesses the shader of a
	// material taking an argument with, which the include's #if reads to
	// add the argument to the instance data and declare canvas_argument.
	argumentDefine = "CANVAS_ARGUMENT"
)

// embeddedAssets are canvas's built-in assets: its include, its materials'
// shaders and its viewport quads', in its assets folder, each shader's binding
// declaration beside it, which render's cmd/declarebindings generates, the
// SDF and text materials' of their define sets alone, as they compile only
// with CANVAS_ARGUMENT.
//
//go:generate go run github.com/dvoyni/cog/plugins/render/cmd/declarebindings assets/canvas.wgsl assets/default.wgsl assets/viewport.wgsl
//go:generate go run github.com/dvoyni/cog/plugins/render/cmd/declarebindings -variantsonly -variant CANVAS_ARGUMENT assets/sdf.wgsl assets/text.wgsl
//go:embed assets
var embeddedAssets embed.FS

// makeAssetFS answers canvas's built-in assets as an FS of their own, each
// named for its path in the assets folder, to mount under assetPrefix.
func makeAssetFS() fs.FS {
	assets, err := fs.Sub(embeddedAssets, "assets")
	if err != nil {
		panic(err) // "assets" is a valid path, which fs.Sub never refuses.
	}
	return assets
}
