package internal

import (
	"fmt"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// The two binding names canvas fills on every batch itself, beside the texture
// and sampler slots: the uniform block and the sprite instance buffer. They are
// the WGSL global names uniforms.wgsl and spritebindings.wgsl declare, which is
// what a param is addressed by.
const (
	uniformsSlot  = "u"
	instancesSlot = "instances"
)

// canvasUniforms mirrors struct CanvasUniforms in uniforms.wgsl, and is set
// whole, once a batch. The layout is WGSL's as it stands - a vec4 at 0, a mat4
// at 16, a vec4 at 80 - so gfx.RawParameterRef's check passes it unpadded.
type canvasUniforms struct {
	// Viewport is width, height, clipEnabled and an unused component.
	Viewport m.Vec4
	Layer    m.Mat4
	// Clip is minX, minY, maxX, maxY.
	Clip m.Vec4
}

// setDrawer is where both batchers turn a material into a gfx draw: it keeps
// the shader and set every material has been given, and builds each batch's
// frame values into one list handed to SetDrawParams.
//
// A draw's parameters resolve first-wins by name - canvas's own ahead of the
// draw's, the draw's ahead of the scope's - and SetDrawParams is last-wins per
// binding, so the list is built from the lowest precedence up: the batch's own
// list reversed, then canvas's. The material's own parameters are the set's,
// beneath all of them, which is what a version over a set already is.
//
// A parameter naming a binding the shader never declared is dropped here rather
// than handed to gfx, which would report it. One scope's parameter list serves
// all three families, and a fade amount the texture shader has no binding for
// is not a mistake.
//
// A set is one per material and binding shape - the names a batch sets, in
// order - rather than one per material. A frame's version of a set carries
// forward to its next draw whatever it does not change, so two batches of one
// material that set different bindings would leak into each other:
//
//	DrawTriangles(layer, verts, nil, TextureParam(TextureSlot, grass)) // version 1: canvasTexture = grass
//	DrawTriangles(layer, verts, nil)                                   // version 2 inherits grass, not white
//
// Under one shape per set every batch of a set sets the same bindings, so each
// version overwrites everything the frame's earlier versions changed. The
// shapes a game draws are few and do not vary with values, so the sets stay
// bounded.
type setDrawer struct {
	// frame is the plugin's frame, which is live only inside the flush.
	frame *frame
	// shaders is one compiled shader per descriptor, and materials the shader
	// each material fingerprint draws with, so a batch hashes no descriptor. A
	// failure is cached like a success, so a shader that does not compile is
	// compiled and reported once, not once a frame.
	shaders   map[gfx.ShaderDescr]canvasShader
	materials map[uint64]canvasShader
	sets      map[setKey]gfx.DrawParams

	// material and shader are what the batch being built draws with, params its
	// frame values, and uniforms the block they borrow.
	material    *Material
	fingerprint uint64
	shader      canvasShader
	params      []gfx.ParameterDescr
	uniforms    canvasUniforms
	// materialParams is scratch for filtering a material's own parameters on
	// the one frame a set is created.
	materialParams []gfx.ParameterDescr
}

// canvasShader is one shader descriptor compiled and uploaded, or the failure
// to compile it.
type canvasShader struct {
	id      gfx.ShaderID
	program gfx.ShaderProgram
	ok      bool
}

// setKey names one set: the material's fingerprint and the binding shape its
// batches set.
type setKey struct{ material, shape uint64 }

// begin opens a batch's draw through material, whose fingerprint is its key,
// and reports whether there is anything to draw it with.
func (d *setDrawer) begin(material *Material, fingerprint uint64) bool {
	d.material, d.fingerprint = material, fingerprint
	d.shader = d.materialShader(material, fingerprint)
	d.params = d.params[:0]
	return d.shader.ok
}

// add appends one frame value, dropped when the shader declares no binding of
// its name.
func (d *setDrawer) add(param gfx.ParameterDescr) {
	if _, ok := d.shader.program.Binding(param.Name()); ok {
		d.params = append(d.params, param)
	}
}

// addReversed appends a first-wins list so that SetDrawParams, which is
// last-wins, lands the same values.
func (d *setDrawer) addReversed(params []gfx.ParameterDescr) {
	for i := len(params) - 1; i >= 0; i-- {
		d.add(params[i])
	}
}

// draw fills the uniform block, versions the set with everything added since
// begin, and records the draw.
func (d *setDrawer) draw(gfxWrite *gfx.OpQueue, pass gfx.PassRef, mesh gfx.MeshDescr, instances int, viewport m.Vec2, layer m.Mat4, clip m.Rect, hasClip bool) {
	clipEnabled := float32(0)
	if hasClip {
		clipEnabled = 1
	}
	d.uniforms = canvasUniforms{
		Viewport: m.Vec4{X: viewport.X, Y: viewport.Y, Z: clipEnabled},
		Layer:    layer,
		Clip:     m.Vec4{X: clip.X, Y: clip.Y, Z: clip.X + clip.Width, W: clip.Y + clip.Height},
	}
	// Borrowed rather than copied: the block is 96 bytes, past what a param
	// carries inline, and SetDrawParams copies it before it returns.
	d.add(gfx.RawParameterRef(uniformsSlot, &d.uniforms))
	set := d.set()
	gfxWrite.SetDrawParams(d.frame.k, set, d.params...)
	gfxWrite.DrawSet(pass, mesh, set, instances, 0)
}

// set returns the set the batch being built draws through, creating it the
// first time its material draws in its shape. That is one of the two steps of
// the flush that are not cheap, and each happens once for the life of the
// plugin.
//
// A set is keyed on the whole fingerprint, the material's own values included,
// so its own parameters are baked into it once. A material rebuilt every frame
// at a changing value is a new set every frame; a value that changes belongs on
// the draw or the scope, which reach the set as the frame's version.
func (d *setDrawer) set() gfx.DrawParams {
	key := setKey{material: d.fingerprint, shape: bindingShape(d.params)}
	if set, ok := d.sets[key]; ok {
		return set
	}
	fr := d.frame
	d.materialParams = d.materialParams[:0]
	for _, param := range d.material.params {
		if _, declared := d.shader.program.Binding(param.Name()); declared {
			d.materialParams = append(d.materialParams, param)
		}
	}
	set := fr.resources.NewDrawParams(fr.k, d.shader.id, d.material.state, d.materialParams...)
	if d.sets == nil {
		d.sets = map[setKey]gfx.DrawParams{}
	}
	d.sets[key] = set
	return set
}

// bindingShape hashes the names a batch sets, in order: FNV-1a over each name
// and a terminator.
func bindingShape(params []gfx.ParameterDescr) uint64 {
	const prime, offset = 1099511628211, 14695981039346656037
	hash := uint64(offset)
	for i := range params {
		name := params[i].Name()
		for j := range len(name) {
			hash = (hash ^ uint64(name[j])) * prime
		}
		hash *= prime
	}
	return hash
}

// materialShader returns the shader a material draws with, compiling it the
// first time any material names its descriptor.
func (d *setDrawer) materialShader(material *Material, fingerprint uint64) canvasShader {
	if shader, ok := d.materials[fingerprint]; ok {
		return shader
	}
	shader := d.compiled(material.shader)
	if d.materials == nil {
		d.materials = map[uint64]canvasShader{}
	}
	d.materials[fingerprint] = shader
	return shader
}

// compiled returns descr compiled and uploaded, compiling it on first use.
// CompileShaderCmd holds no lock, so the flush declaring it costs no System
// anything; it reads the shader through the storage filesystem the flush
// already holds.
func (d *setDrawer) compiled(descr gfx.ShaderDescr) canvasShader {
	if shader, ok := d.shaders[descr]; ok {
		return shader
	}
	fr := d.frame
	var shader canvasShader
	response := fr.compile(fr.k, gfx.CompileShaderRequest{FS: fr.fsys, Descr: descr})
	if response.Err != nil {
		ReportShaderFailed(fr.k, descr, response.Err)
	} else {
		shader.id = fr.resources.NewShader()
		fr.resources.UploadProgram(fr.k, shader.id, response.Program)
		shader.program, shader.ok = response.Program, true
	}
	if d.shaders == nil {
		d.shaders = map[gfx.ShaderDescr]canvasShader{}
	}
	d.shaders[descr] = shader
	return shader
}

// shaderFailed is the report-once key a material's shader failing to compile
// is spoken under: canvas's own type, keyed by the descriptor, so one broken
// shader is one report however many materials name it.
type shaderFailed struct{ shader gfx.ShaderDescr }

// ReportShaderFailed says once that a material's shader did not compile, which
// leaves every draw through it drawing nothing. The error is the compile's, so
// it names the file and line the source map resolved.
func ReportShaderFailed(k kernel.Kernel, descr gfx.ShaderDescr, err error) {
	k.ReportErrorOnce(shaderFailed{shader: descr}, fmt.Errorf("canvas: material shader %s: %w", shaderText(descr), err))
}

// shaderText names a shader the way a report wants it: its path, or that it
// was inline text.
func shaderText(descr gfx.ShaderDescr) string {
	if path := descr.Path(); path != "" {
		return fmt.Sprintf("%q", path)
	}
	return "(inline)"
}
