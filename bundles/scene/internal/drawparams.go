package internal

import (
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// Draw params: every Batch draws through a gfx set, and what differs per Batch
// - the pass's frame block, its instances, the frame's animation and mesh
// arenas, the model's pose buffers and the Entity's Params - is the frame's
// version of that set.
//
// A model primitive drawn as its file says - no Material, the bundled default
// shader and no Params binding - draws the set model created at load. Every
// other Batch draws a set of scene's own, which the load System creates and
// releases, because it is the one scene System holding the resource queue and
// the shader compile. They are cached by setKey, so a steady frame creates,
// compiles and resolves nothing.

// setKey names one of scene's own sets: the material half of the Batch key the
// load System took, the shader variant the geometry needs, and the Params
// binding shape of the Batches drawing it.
//
// The shape is in the key because a frame's version of a set carries forward
// to its next draw whatever it does not name. Two Batches of one material, one
// tinted through Params and one not, would otherwise leak the tint into the
// untinted one; under one shape per set, every Batch drawing a set names the
// same bindings and overwrites what the one before it set.
type setKey struct {
	material uint64
	variant  model.ShaderVariant
	shape    uint64
}

// sceneBindings is which of the bindings scene fills per Batch a set's shader
// declares, found once when the set is created: gfx reports a param naming a
// binding the shader does not declare, and a caller's shader declares only
// what it reads.
type sceneBindings uint8

const (
	bindFrame sceneBindings = 1 << iota
	bindInstances
	bindAnim
	bindMeshes
	bindPoses
	bindSkinJoints
	bindMorphDeltas
	bindPbrMaterial
)

// sceneBindingNames are the names behind each sceneBindings bit, in bit order.
var sceneBindingNames = [...]string{
	model.BindingSceneFrame, model.BindingSceneInstances, model.BindingSceneAnim, model.BindingSceneMeshes,
	model.BindingScenePoses, model.BindingSceneSkinJoints, model.BindingSceneMorphDeltas,
	model.BindingScenePbrMaterial,
}

// bindingsOf reads which of scene's per-Batch bindings a program declares.
func bindingsOf(program gfx.ShaderProgram) sceneBindings {
	var bindings sceneBindings
	for i, name := range sceneBindingNames {
		if _, ok := program.Binding(name); ok {
			bindings |= 1 << i
		}
	}
	return bindings
}

// sceneShader is one shader descriptor compiled and uploaded, or the failure
// to compile it.
type sceneShader struct {
	id       gfx.ShaderID
	program  gfx.ShaderProgram
	bindings sceneBindings
	ok       bool
}

// cachedSets is one setKey's material: a set per tag it serves, and how many
// drawables hold it. It is released when the last one lets go.
type cachedSets struct {
	material material
	refs     int
}

// setCache is scene's own sets and the shaders they are built on. It lives in
// keyScratch: the load System writes it, and the recording System reads it.
type setCache struct {
	// shaders is one compiled shader per descriptor, variant defines
	// included. A failure is cached like a success, so a shader that does not
	// compile is compiled and reported once. Shaders are never released: a
	// descriptor's module serves every set built over it, for the plugin's
	// life, as model's bundled four do.
	shaders map[gfx.ShaderDescr]sceneShader
	sets    map[setKey]*cachedSets
	// bundled is the bundled shader's four variants as scene compiled them,
	// which is how a model's own set is read: its bindings, for the frame's
	// version, and its program, for the Params it may take.
	bundled [model.VariantCount]sceneShader
	// defaultShader is the default scene shader every set was built under,
	// and defaultBundled whether it is the bundled PBR with no params, which
	// is when a model's own set is drawn.
	defaultShader  model.SceneShaderDescr
	defaultBundled bool

	// params and values are the scratch a set is resolved in.
	params []gfx.ParameterDescr
	values model.PbrValues
}

// newSetCache starts under the bundled PBR, which is what the Lookup reports
// until a game sets a default scene shader.
func newSetCache() setCache {
	return setCache{
		shaders:        map[gfx.ShaderDescr]sceneShader{},
		sets:           map[setKey]*cachedSets{},
		defaultBundled: true,
	}
}

// ownSet reports whether a drawable draws a model's own set rather than one
// of scene's: a file material with nothing laid over it, under the bundled
// shader, with no Params binding.
func (c *setCache) ownSet(hasOverride bool, shape uint64) bool {
	return !hasOverride && c.defaultBundled && shape == 0
}

// defaultChanged reports whether the default scene shader differs from the one
// the sets were built under. The Lookup copies the params on every set, so
// the same slice is the same params.
func (c *setCache) defaultChanged(current model.SceneShaderDescr) bool {
	was := c.defaultShader
	if current.Source != was.Source || len(current.Params) != len(was.Params) {
		return true
	}
	return len(current.Params) > 0 && &current.Params[0] != &was.Params[0]
}

// paramsShape hashes the bindings a Params Component sets, in order: each
// name that is a binding of its own, and one token for however many members
// of the material block it names, since those reach the draw as the block.
// No binding at all is zero.
func paramsShape(params []gfx.ParameterDescr) uint64 {
	const prime, offset = 1099511628211, 14695981039346656037
	if len(params) == 0 {
		return 0
	}
	hash, members := uint64(offset), false
	for i := range params {
		name := params[i].Name()
		if model.IsPbrValue(name) {
			members = true
			continue
		}
		for j := range len(name) {
			hash = (hash ^ uint64(name[j])) * prime
		}
		hash *= prime
	}
	if members {
		hash = (hash ^ 1) * prime
	}
	return nonZero(hash)
}

// acquire takes one hold on the set key names, creating it the first time. A
// file's material is laid under it and an override, when there is one, over
// it.
func (r *keyer) acquire(s *keyScratch, key setKey, file *model.MaterialIngredients, override *Material) {
	c := &s.sets
	if cached, ok := c.sets[key]; ok {
		cached.refs++
		return
	}
	cached := &cachedSets{refs: 1}
	if override == nil {
		cached.material = material{r.resolveTag(c, TagForward, gfx.ShaderDescr{}, gfx.MaterialState{}, nil, file, key.variant)}
	} else {
		cached.material = make(material, 0, override.Tags.Len())
		for _, tag := range override.Tags.All() {
			cached.material = append(cached.material,
				r.resolveTag(c, tag.Tag, tag.Shader, tag.State, &tag.Params, file, key.variant))
		}
	}
	c.sets[key] = cached
}

// release lets go of one hold on the set key names, releasing its sets with
// the last.
func (r *keyer) release(s *keyScratch, key setKey) {
	c := &s.sets
	cached, ok := c.sets[key]
	if !ok {
		return
	}
	cached.refs--
	if cached.refs > 0 {
		return
	}
	for i := range cached.material {
		if set := cached.material[i].set; set != (gfx.DrawParams{}) {
			r.resources.ReleaseDrawParams(r.k, set)
		}
	}
	delete(c.sets, key)
}

// resolveTag resolves one tag of a material into a set, over one file
// material's ingredients:
//
//   - the shader is the tag's, or the default scene shader where it names
//     none, under the variant's defines whichever it is;
//   - the bindings are the file's textures and samplers, overlaid by name
//     with the default scene shader's and then the tag's, each kept only
//     where the shader declares it;
//   - the material block is the file's values, with every member the default
//     scene shader's params and then the tag's name laid over them;
//   - the state is the tag's, or the file's where it names none.
//
// Nothing here knows what any binding means. A shader that does not compile
// leaves the tag's set zero, which draws nothing.
func (r *keyer) resolveTag(
	c *setCache, tag PassTag, shader gfx.ShaderDescr, state gfx.MaterialState,
	own *m.List[gfx.ParameterDescr], file *model.MaterialIngredients, variant model.ShaderVariant,
) materialTag {
	if shader == (gfx.ShaderDescr{}) {
		shader = c.defaultShader.Source
	}
	if state == (gfx.MaterialState{}) {
		state = file.State
	}
	compiled := r.compiled(c, model.VariantShader(shader, variant))
	resolved := materialTag{
		tag: tagOf(tag), state: state, program: compiled.program, bindings: compiled.bindings,
		values: file.Values,
	}
	if !compiled.ok {
		return resolved
	}
	params := c.params[:0]
	for _, param := range file.Params {
		if !model.IsPbrValue(param.Name()) {
			params = append(params, param)
		}
	}
	params = layParams(params, 0, &resolved.values, c.defaultShader.Params...)
	if own != nil {
		for _, param := range own.All() {
			params = layParams(params, 0, &resolved.values, param)
		}
	}
	kept := params[:0]
	for _, param := range params {
		if _, declared := compiled.program.Binding(param.Name()); declared {
			kept = append(kept, param)
		}
	}
	if compiled.bindings&bindPbrMaterial != 0 {
		c.values = resolved.values
		// Borrowed rather than copied: the block is 160 bytes, past what a
		// param carries inline, and NewDrawParams copies it before it returns.
		kept = append(kept, gfx.RawParameterRef(model.BindingScenePbrMaterial, &c.values))
	}
	resolved.set = r.resources.NewDrawParams(r.k, compiled.id, state, kept...)
	// The scratch is kept for the next set, and cleared so it holds no
	// texture past the load.
	clear(params)
	c.params = params[:0]
	return resolved
}

// layParams lays params over what a tag resolves so far, in order: a member of
// the material block into its values, and any other binding over the window
// arena[start:] by name, through overlayParam.
func layParams(arena []gfx.ParameterDescr, start int, values *model.PbrValues, params ...gfx.ParameterDescr) []gfx.ParameterDescr {
	for _, param := range params {
		if !values.Overlay(param) {
			arena = overlayParam(arena, start, param)
		}
	}
	return arena
}

// compiled returns descr compiled and uploaded, compiling it on first use.
// CompileShaderCmd holds no lock, so the load System declaring it costs no
// System anything.
func (r *keyer) compiled(c *setCache, descr gfx.ShaderDescr) sceneShader {
	if shader, ok := c.shaders[descr]; ok {
		return shader
	}
	var shader sceneShader
	response := r.compile(r.k, gfx.CompileShaderRequest{FS: r.fsys, Descr: descr})
	if response.Err != nil {
		r.k.ReportErrorOnce(shaderFailed{shader: descr}, ErrMaterialShaderUnavailable{Shader: shaderText(descr), Err: response.Err})
	} else {
		shader.id = r.resources.NewShader()
		r.resources.UploadProgram(r.k, shader.id, response.Program)
		shader.program, shader.bindings, shader.ok = response.Program, bindingsOf(response.Program), true
	}
	c.shaders[descr] = shader
	return shader
}

// shaderFailed is the report-once key a material's shader failing to compile
// is spoken under: scene's own type, keyed by the descriptor, so one broken
// shader is one report however many materials name it.
type shaderFailed struct{ shader gfx.ShaderDescr }

// ensureBundled compiles the bundled shader's variant the first time a model's
// own set of it is keyed, which is what the recording System reads that set's
// bindings from.
func (r *keyer) ensureBundled(c *setCache, variant model.ShaderVariant) {
	if !c.bundled[variant].ok {
		c.bundled[variant] = r.compiled(c, model.VariantShader(gfx.ShaderDescr{}, variant))
	}
}
