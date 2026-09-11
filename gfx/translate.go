package gfx

import (
	"cmp"
	"encoding/binary"
	"math"
	"slices"
	"strings"

	"github.com/dvoyni/cog/m"

	"github.com/dvoyni/cog/storage"
)

// uniformMax caps a per-draw shader-parameter block.
const uniformMax = 256

// pipelineKey identifies a cached pipeline by shader identity, render state and
// the formats of the attachments it renders into. MaterialState is embedded
// whole so that adding a state field cannot silently return a pipeline built
// for the old one.
type pipelineKey struct {
	shader      ShaderID
	topology    PrimitiveTopology
	state       MaterialState
	colorFormat TextureFormat
	depthFormat TextureFormat
	// noColor separates the pipeline a shader needs in a depth-only pass from
	// the one it needs in a colour pass. Without it in the key, a shader drawn
	// in both gets whichever pass reached it first, which is a validation
	// failure in the other one.
	noColor bool
	layout  vertexLayoutKey
}

// translator turns an OpQueue into a GpuQueue, lazily creating and caching
// backend shaders/pipelines/samplers and resolves every texture and buffer to a
// baked resource ID. It is owned by the plugin and used only on the driver's
// render thread inside ConsumeCmd.
type translator struct {
	shaders        map[ShaderDescr]*cachedShader
	pipelines      map[pipelineKey]PipelineID
	samplers       map[SamplerDesc]SamplerID
	uarena         []byte
	layouts        map[ShaderID]ShaderLayout
	textures       map[string]TextureDescr
	parameterPlans map[parameterPlanBucketKey][]cachedParameterPlan
	ops            GpuQueue
	// Pass bookkeeping, reused each frame: the run order of the frame's passes
	// and its draws bucketed behind the pass that recorded them.
	passOrder  []int
	passStart  []int
	passDraws  []int
	passCursor []int
	// strayDraws counts the frame's draws recorded outside any pass.
	strayDraws int
	// textureUsage is what role each attachment texture is currently in, for
	// the frame so far. A transition has to name the usage the texture is
	// actually in, so this is tracked rather than assumed; a texture absent
	// from the map has never been an attachment and needs no barrier.
	textureUsage map[TextureID]TextureUsage
	// runSampled is the scratch set of textures one merged run samples, reused
	// across runs so a frame allocates nothing per pass.
	runSampled []TextureID
	// diagnostic holds a report that does not stop the frame - a shader over the
	// web floor still renders here - until translate surfaces it.
	diagnostic error
}

func newTranslator() *translator {
	return &translator{
		shaders:        map[ShaderDescr]*cachedShader{},
		pipelines:      map[pipelineKey]PipelineID{},
		samplers:       map[SamplerDesc]SamplerID{},
		layouts:        map[ShaderID]ShaderLayout{},
		textures:       map[string]TextureDescr{},
		parameterPlans: map[parameterPlanBucketKey][]cachedParameterPlan{},
		textureUsage:   map[TextureID]TextureUsage{},
	}
}

// translate builds the op stream for the durable resource operations followed
// by the latest frame. The returned ops (and their payloads) are valid until
// the next translate call. It returns the first error encountered; valid draws
// are still translated.
func (t *translator) translate(
	queue *OpQueue, persistent []op, backend Backend, filesystem storage.FileSystem,
	capture GpuCaptureDesc, capturing bool,
) (*GpuQueue, error) {
	t.ops.Reset()

	need := len(queue.ops) * uniformMax
	if cap(t.uarena) < need {
		t.uarena = make([]byte, need)
	}
	t.uarena = t.uarena[:need]

	var firstErr error
	uoff := 0
	// Resource ops belong to no pass: every bake is hoisted ahead of all of
	// them, so a pass can read anything the frame uploaded.
	translateResources := func(list []op) {
		for i := range list {
			op := &list[i]
			if op.kind == opBakeBuffer {
				if len(op.bytes) > 0 {
					t.ops.BakeBuffer(op.bufferID, op.bufferKind, op.bufferSize, op.bytes)
				}
				continue
			}
			if op.kind == opReleaseBuffer {
				t.ops.ReleaseBuffer(op.bufferID)
				continue
			}
			if op.kind == opBakeTexture {
				if len(op.bytes) > 0 {
					t.ops.BakeTexture(op.textureID, op.texW, op.texH, op.format, op.bytes, op.mipmaps)
				}
				continue
			}
			if op.kind == opReleaseTexture {
				t.ops.ReleaseTexture(op.textureID)
				continue
			}
			if op.kind == opReleaseCachedResource {
				t.releaseCachedResource(backend, op.path)
				continue
			}
			if op.kind == opFreeCachedResources {
				t.freeCachedResources(backend)
				continue
			}
			if op.kind == opAllocateTexture {
				t.ops.AllocateTexture(op.textureID, TextureDesc{
					Width: op.texW, Height: op.texH, Layers: op.texLayers, Format: op.format,
					Renderable: op.renderable,
				})
				continue
			}
			if op.kind == opUpdateTexture {
				t.ops.UpdateTexture(op.textureID, op.texLayer, op.region, op.bytes)
				continue
			}
		}
	}
	translateResources(persistent)
	translateResources(queue.ops)

	t.translatePasses(queue, backend, filesystem, &uoff, &firstErr, capture, capturing)

	// A report that did not stop anything is still worth surfacing, but only
	// behind an error that did.
	if firstErr == nil {
		firstErr = t.diagnostic
	}
	t.diagnostic = nil

	return &t.ops, firstErr
}

// translatePasses runs the frame's passes in Order, merging the runs that are
// indistinguishable from one longer pass, and emits each one's draws.
func (t *translator) translatePasses(
	queue *OpQueue, backend Backend, filesystem storage.FileSystem, uoff *int, firstErr *error,
	capture GpuCaptureDesc, capturing bool,
) {
	t.planPasses(queue)
	if t.strayDraws > 0 && *firstErr == nil {
		*firstErr = ErrDrawWithoutPass{Count: t.strayDraws}
	}
	// Attachment roles are per frame: the pool hands the same texture id to a
	// different purpose next frame, and every frame's barriers are encoded from
	// scratch anyway.
	clear(t.textureUsage)
	presents := false
	for i := 0; i < len(t.passOrder); {
		head := queue.passes[t.passOrder[i]].desc
		tail, draws := head, t.passDrawCount(t.passOrder[i])
		last := i
		for j := i + 1; j < len(t.passOrder); j++ {
			next := queue.passes[t.passOrder[j]].desc
			if !mergesInto(next, tail) {
				break
			}
			tail, last = next, j
			draws += t.passDrawCount(t.passOrder[j])
		}
		if !head.hasEffect(draws) {
			i = last + 1
			continue
		}
		presents = presents || head.Target.IsScreen()
		t.transitionRun(queue, head, i, last)
		t.ops.BeginPass(t.gpuPassDesc(backend, head, tail))
		for j := i; j <= last; j++ {
			pass := &queue.passes[t.passOrder[j]]
			for _, index := range t.passDrawOps(t.passOrder[j]) {
				t.translateDraw(&queue.ops[index], pass.desc, backend, filesystem, uoff, firstErr)
			}
		}
		t.ops.EndPass()
		i = last + 1
	}
	// The present pass exists only to show the frame buffer, so it is emitted
	// exactly when the frame buffer was used - which is also when the backend
	// allocated it. A frame that rendered only into its own textures leaves the
	// screen alone rather than blitting a buffer nothing wrote to.
	if presents {
		t.ops.Present()
	}
	// The capture is the last thing in the frame. The present pass moves the
	// frame buffer out of RenderAttachment and samples it, so a copy encoded
	// ahead of it would name a layout that is no longer true; encoded here it
	// costs the frame nothing but the copy's own bandwidth and keeps Execute's
	// one-submit contract literally true.
	if capturing {
		// A texture capture is a third role for a texture gfx has been
		// tracking, so it declares the barrier like any other. A screen
		// capture declares none: the frame buffer is the one attachment gfx
		// never names, and the backend places that transition itself.
		if !capture.Screen {
			t.transitionTo(capture.Texture, TextureUsageCopySrc)
		}
		t.ops.Capture(capture)
	}
}

// transitionRun places the barriers one merged run needs and records the roles
// it leaves its textures in. It runs before the pass is opened because a
// barrier cannot be recorded inside a render pass.
//
// The rule is not "every render target gets a barrier" - it is that a
// write-then-read pair, in either direction, has to be ordered. Both directions
// occur: a camera renders into a temporary target that a later pass composites
// (write then read), and a post-processing chain ping-pongs two targets (read
// then write). A texture nothing has used as an attachment this frame is not
// gfx's to order.
func (t *translator) transitionRun(queue *OpQueue, head PassDescr, first, last int) {
	// Reads first: a texture this run samples has to have finished being written.
	t.runSampled = t.runSampled[:0]
	for j := first; j <= last; j++ {
		for _, index := range t.passDrawOps(t.passOrder[j]) {
			op := &queue.ops[index]
			t.collectSampled(op.material.params)
			t.collectSampled(op.params)
		}
	}
	for _, texture := range t.runSampled {
		t.transitionTo(texture, TextureUsageTextureBinding)
	}
	// Then writes: this run's own attachments. A texture that was sampled
	// earlier in the frame is transitioned back before it is written again.
	if head.Target.kind == targetTexture {
		t.transitionTo(head.Target.texture, TextureUsageRenderAttachment)
	}
	if head.Depth.kind == depthKindTexture {
		t.transitionTo(head.Depth.texture, TextureUsageRenderAttachment)
	}
}

// collectSampled adds every baked texture the parameters name to the run's
// sampled set, skipping the ones already in it: two draws sampling one render
// target is a single hazard, and a duplicate barrier is a real pipeline stall.
//
// It reads the raw parameters rather than a resolved parameter plan, so it can
// run before the pass opens. That over-approximates by the textures a shader
// does not actually declare, which costs a barrier nothing reads and is the
// safe direction to be wrong in.
func (t *translator) collectSampled(params []ParameterDescr) {
	for i := range params {
		p := &params[i]
		if p.kind != paramTexture || p.texture.source != TextureSourceBaked || p.texture.id == 0 {
			continue
		}
		if !slices.Contains(t.runSampled, p.texture.id) {
			t.runSampled = append(t.runSampled, p.texture.id)
		}
	}
}

// transitionTo moves one texture into a usage, emitting a barrier only when
// that is a change from a role the frame has already put it in. A texture that
// has never been an attachment this frame has no writes to order against, and
// one already in the usage is a no-op the backend should not pay for.
func (t *translator) transitionTo(texture TextureID, to TextureUsage) {
	if texture == 0 {
		return
	}
	from, seen := t.textureUsage[texture]
	if !seen {
		// First use this frame. Record the role without a barrier: the only
		// hazard a barrier fixes is against this frame's own earlier passes,
		// and there are none for this texture yet.
		t.textureUsage[texture] = to
		return
	}
	if from == to {
		return
	}
	t.ops.TransitionTexture(TextureTransition{Texture: texture, From: from, To: to})
	t.textureUsage[texture] = to
}

// gpuPassDesc resolves a merged run's attachments: it loads like the pass that
// opened the run and stores like the one that closed it.
func (t *translator) gpuPassDesc(backend Backend, head, tail PassDescr) GpuPassDesc {
	desc := GpuPassDesc{
		Load: head.Load, Clear: head.Clear, Store: tail.Store,
		DepthLoad: head.DepthLoad, DepthClear: head.DepthClear, DepthStore: tail.DepthStore,
		Label: head.Label,
	}
	switch head.Target.kind {
	case targetScreen:
		desc.Screen = true
	case targetNone:
		desc.NoColor = true
	case targetTexture:
		// This can resolve to zero: a temporary target is allocated by the same
		// frame's bakes, which the backend replays after these descriptors were
		// built, so a target used for the first time has no view yet. NoColor
		// stays false, which is what keeps that case distinguishable from a
		// pass that declares no colour attachment at all.
		desc.Target = backend.TextureView(head.Target.texture, head.Target.mip, head.Target.layer)
	}
	switch head.Depth.kind {
	case depthKindAuto:
		desc.DepthAuto = true
	case depthKindTexture:
		desc.Depth = backend.TextureView(head.Depth.texture, 0, 0)
	}
	return desc
}

// translateDraw emits one draw into the currently open pass.
func (t *translator) translateDraw(op *op, pass PassDescr, backend Backend, filesystem storage.FileSystem, uoff *int, firstErr *error) {
	m := &op.mesh
	stride := m.stride()
	if m.vertices.id == 0 || m.vertexCount <= 0 || stride <= 0 {
		return
	}
	// Every shader failure is fatal to its draw, whether or not this frame is the
	// one that reports it: the error surfaces once, the draw is dropped every
	// time. ErrShaderExceedsWebLimits stays the only non-fatal report in gfx.
	shaderID, err := t.ensureShader(backend, filesystem, op.material.shader)
	if err != nil && *firstErr == nil {
		*firstErr = err
	}
	if shaderID == 0 {
		return
	}
	pipeline := t.ensurePipeline(backend, shaderID, m, op.material.state, pass)
	if pipeline == 0 {
		return
	}

	layout := t.shaderLayout(backend, shaderID)
	plan := t.prepareParameterPlan(shaderID, shaderLabel(op.material.shader), layout, op.material.params, op.params)
	if plan.mismatch != nil {
		if *firstErr == nil {
			*firstErr = plan.mismatch
		}
		return
	}
	if name, ok := sampledAttachment(plan, op.params, op.material.params, pass); ok {
		if *firstErr == nil {
			*firstErr = ErrDrawSamplesAttachment{Pass: pass.Label, Parameter: name}
		}
		return
	}
	t.ops.SetPipeline(pipeline)
	// A shader that declares no uniform block gets no uniform binding and no
	// pooled buffer. Emitting one anyway puts an entry in a group the pipeline
	// layout does not have, and CreateBindGroup fails the entry-count rule with
	// the whole frame's command buffer as the casualty.
	if plan.uniformSize > 0 {
		u := t.packParams(t.uarena[*uoff:*uoff+uniformMax], op.params, op.material.params, plan)
		*uoff += uniformMax
		t.ops.SetParams(u)
	}
	t.emitResources(backend, filesystem, op.params, op.material.params, plan)
	t.ops.SetVertexBuffer(m.vertices.id, 0)

	instances := op.instances
	if instances < 1 {
		instances = 1
	}
	if m.indexed && m.indices.id != 0 && m.indexCount > 0 {
		t.ops.SetIndexBuffer(m.indices.id, 0)
		t.ops.Draw(0, m.indexCount, instances, op.firstInstance, true)
	} else {
		t.ops.Draw(0, m.vertexCount, instances, op.firstInstance, false)
	}
}

// sampledAttachment names the first texture parameter a draw samples that its
// own pass renders into. Only a baked texture can be an attachment, so this
// resolves nothing and costs a comparison per binding.
func sampledAttachment(plan *parameterPlan, drawParams, materialParams []ParameterDescr, pass PassDescr) (string, bool) {
	attachment := func(id TextureID) bool {
		if id == 0 {
			return false
		}
		return (pass.Target.kind == targetTexture && pass.Target.texture == id) ||
			(pass.Depth.kind == depthKindTexture && pass.Depth.texture == id)
	}
	for i := range plan.resources {
		resource := &plan.resources[i]
		if resource.kind != plannedTexture {
			continue
		}
		p := resource.param.value(materialParams, drawParams)
		if p != nil && p.kind == paramTexture && p.texture.source == TextureSourceBaked && attachment(p.texture.id) {
			return p.name, true
		}
	}
	return "", false
}

// planPasses puts the frame's passes in run order - Order first, declaration
// sequence breaking ties - and buckets each pass's draws behind it.
func (t *translator) planPasses(queue *OpQueue) {
	count := len(queue.passes)
	t.passOrder = t.passOrder[:0]
	for i := range count {
		t.passOrder = append(t.passOrder, i)
	}
	slices.SortStableFunc(t.passOrder, func(a, b int) int {
		return cmp.Compare(queue.passes[a].desc.Order, queue.passes[b].desc.Order)
	})

	t.passStart = slices.Grow(t.passStart[:0], count+1)[:count+1]
	clear(t.passStart)
	t.strayDraws = 0
	for i := range queue.ops {
		if queue.ops[i].kind != opDraw {
			continue
		}
		if pass := int(queue.ops[i].pass); pass >= 0 && pass < count {
			t.passStart[pass+1]++
		} else {
			t.strayDraws++
		}
	}
	for i := 1; i <= count; i++ {
		t.passStart[i] += t.passStart[i-1]
	}
	t.passDraws = slices.Grow(t.passDraws[:0], t.passStart[count])[:t.passStart[count]]
	cursor := append(t.passCursor[:0], t.passStart[:count]...)
	for i := range queue.ops {
		if pass := int(queue.ops[i].pass); queue.ops[i].kind == opDraw && pass >= 0 && pass < count {
			t.passDraws[cursor[pass]] = i
			cursor[pass]++
		}
	}
	t.passCursor = cursor
}

func (t *translator) passDrawOps(pass int) []int {
	return t.passDraws[t.passStart[pass]:t.passStart[pass+1]]
}

func (t *translator) passDrawCount(pass int) int {
	return t.passStart[pass+1] - t.passStart[pass]
}

// emitResources binds each reflected texture/sampler resource, matching its name
// to a material parameter (defaulting to the white texture / a clamp+linear
// sampler when unset), so every binding the shader declares is provided.
func (t *translator) emitResources(backend Backend, filesystem storage.FileSystem, drawParams, materialParams []ParameterDescr, plan *parameterPlan) {
	// Each reflected sampler is filled by the parameter of its own name, and
	// falls back to the zero descriptor - clamp and linear - when unset.
	for i := range plan.samplers {
		sampler := &plan.samplers[i]
		var desc SamplerDesc
		if p := sampler.param.value(materialParams, drawParams); p != nil && p.kind == paramSampler {
			desc = p.sampler
		}
		t.ops.SetSampler(t.ensureSampler(backend, desc), sampler.group, sampler.binding)
	}
	for i := range plan.resources {
		resource := &plan.resources[i]
		p := resource.param.value(materialParams, drawParams)
		if resource.kind == plannedBuffer {
			if p != nil && p.kind == paramBuffer {
				if p.buffer.id != 0 {
					t.ops.SetBuffer(resource.group, resource.binding, p.buffer.id, p.bufferOffset, p.bufferSize)
				}
			}
			continue
		}
		textureID := TextureID(0)
		if p != nil && p.kind == paramTexture {
			textureID = t.ensureTexture(backend, filesystem, p.texture)
		}
		t.ops.SetTexture(textureID, resource.group, resource.binding)
	}
}

func (t *translator) ensureTexture(backend Backend, filesystem storage.FileSystem, descr TextureDescr) TextureID {
	if descr.source == TextureSourceBaked {
		return descr.id
	}
	if descr.source != TextureSourceResource {
		return 0
	}
	if baked, ok := t.textures[descr.path]; ok {
		return baked.id
	}
	width, height, pixels, ok := loadTextureResource(filesystem, descr.path)
	if !ok {
		return 0
	}
	id := backend.NewTexture()
	t.ops.BakeTexture(id, width, height, descr.format, pixels, false)
	t.textures[descr.path] = TextureDescr{source: TextureSourceBaked, id: id}
	return id
}

// cachedShader is one module in the translator's shader cache: the backend id
// when it compiled, the error when it did not, and the sources it was built
// from, which is what eviction scans.
//
// A failed shader is cached as failed, on the same (root, supply) key. Without
// that, the next frame re-reads every source, re-flattens, re-fails and
// re-reports - at the frame rate.
type cachedShader struct {
	id       ShaderID
	err      error
	sources  []string
	reported bool
}

// report returns the entry's error the first time it is asked and nothing
// afterwards, following the reportedMissingBackend precedent: a condition true
// every frame is worth saying once. The caller drops the draw on a zero id
// rather than on the error, so silence never lets a bad draw through.
func (c *cachedShader) report() error {
	if c.err == nil || c.reported {
		return nil
	}
	c.reported = true
	return c.err
}

func (t *translator) ensureShader(backend Backend, filesystem storage.FileSystem, descr ShaderDescr) (ShaderID, error) {
	if cached, ok := t.shaders[descr]; ok {
		return cached.id, cached.report()
	}
	label := shaderLabel(descr)
	// Flatten happens here, on the render thread, on a cache miss only - the
	// first draw of a given (root, supply). The cost changes from one file read
	// to N, which is the same shape as today's hitch rather than a new class of
	// problem: if it ever bites, it bites the first frame a material appears,
	// which is already true.
	flattened, err := flattenShader(filesystem, descr)
	// The include set is recorded on failure as well as on success, so that a
	// failed entry evicts like any other and the developer loop stays: fix the
	// file, hot-reload evicts, the next frame retries and reports afresh.
	cached := &cachedShader{sources: flattened.sources}
	t.shaders[descr] = cached
	if err != nil {
		cached.err = err
		return 0, cached.report()
	}
	id, err := backend.NewShader(ShaderDesc{Code: []byte(flattened.text), Label: label})
	if err != nil {
		// Nothing the backend said is rewritten and no line number is parsed out
		// of its message: gfx appends the rendered segment table and lets the
		// reader subtract.
		cached.err = ErrShaderSource{
			Shader: label, Message: "failed to compile", Err: err, flattened: flattened.sourceMap.render(),
		}
		return 0, cached.report()
	}
	cached.id = id
	// Every shader gfx reflects is measured, not only an engine's bundled ones:
	// a caller-supplied material is what actually gets bound at draw time. The
	// shader is cached, so this reports once rather than once a frame.
	if diagnostic := checkWebLimits(label, t.shaderLayout(backend, id), backend.Limits()); diagnostic != nil && t.diagnostic == nil {
		t.diagnostic = diagnostic
	}
	return id, nil
}

func (t *translator) releaseCachedResource(backend Backend, path string) {
	if texture, ok := t.textures[path]; ok {
		t.ops.ReleaseTexture(texture.id)
		delete(t.textures, path)
	}
	// Eviction scans the forward index rather than probing one descriptor,
	// because three things break that probe under the preprocessor: a path may
	// root several variants, a path may be an included source of modules rooted
	// elsewhere, and a ShaderWithText shader can include resources, so a text
	// shader is now evictable by a path. A reverse path-to-modules index would be
	// O(1) instead of O(n), but it is a second structure to keep in sync on every
	// release for a lookup nobody waits on - eviction is a developer-loop command
	// and t.shaders holds single digits - and the forward direction is what
	// flatten already produces.
	for descr, cached := range t.shaders {
		if slices.Contains(cached.sources, path) {
			t.releaseShader(backend, descr, cached)
		}
	}
}

func (t *translator) releaseShader(backend Backend, descr ShaderDescr, cached *cachedShader) {
	delete(t.shaders, descr)
	if cached.id == 0 {
		return
	}
	for key, pipeline := range t.pipelines {
		if key.shader == cached.id {
			backend.FreePipeline(pipeline)
			delete(t.pipelines, key)
		}
	}
	for key := range t.parameterPlans {
		if key.shader == cached.id {
			delete(t.parameterPlans, key)
		}
	}
	delete(t.layouts, cached.id)
	backend.FreeShader(cached.id)
}

func (t *translator) freeCachedResources(backend Backend) {
	for _, texture := range t.textures {
		t.ops.ReleaseTexture(texture.id)
	}
	for _, pipeline := range t.pipelines {
		backend.FreePipeline(pipeline)
	}
	for _, cached := range t.shaders {
		if cached.id != 0 {
			backend.FreeShader(cached.id)
		}
	}
	for _, sampler := range t.samplers {
		backend.FreeSampler(sampler)
	}
	clear(t.textures)
	clear(t.pipelines)
	clear(t.shaders)
	clear(t.samplers)
	clear(t.layouts)
	clear(t.parameterPlans)
}

func shaderLabel(descr ShaderDescr) string {
	root := "gfx.shader"
	if descr.source == ShaderSourceResource {
		root = descr.textOrPath
	}
	if descr.supply == "" {
		return root
	}
	return root + " [" + strings.ReplaceAll(descr.supply, "\n", " ") + "]"
}

// shaderLayout returns the backend's reflected layout for a shader, cached by id.
func (t *translator) shaderLayout(backend Backend, id ShaderID) ShaderLayout {
	if l, ok := t.layouts[id]; ok {
		return l
	}
	l := backend.ShaderLayout(id)
	t.layouts[id] = l
	return l
}

// packParams writes reflected shader constants into dst. Per-draw parameters
// override same-named material parameters; unmatched members remain zero.
func (t *translator) packParams(dst []byte, drawParams, materialParams []ParameterDescr, plan *parameterPlan) []byte {
	size := plan.uniformSize
	if size <= 0 {
		return dst[:0]
	}
	if size > len(dst) {
		size = len(dst)
	}
	buf := dst[:size]
	clear(buf)
	for i := range plan.uniforms {
		uniform := &plan.uniforms[i]
		off := uniform.offset
		if off < 0 || off >= size {
			continue
		}
		if p := uniform.param.value(materialParams, drawParams); p != nil {
			writeParamAt(buf, off, p)
		}
	}
	return buf
}

// writeParamAt writes a scalar/vec/color param at byte offset off (bounds-checked).
func writeParamAt(buf []byte, off int, p *ParameterDescr) {
	switch p.kind {
	case paramColor:
		if off+16 <= len(buf) {
			writeColor(buf[off:off+16], p.color)
		}
	case paramVec4:
		if off+16 <= len(buf) {
			writeVec4(buf[off:off+16], p.vec)
		}
	case paramMat4:
		if off+64 <= len(buf) {
			writeMat4(buf[off:off+64], p.mat)
		}
	case paramFloat:
		if off+4 <= len(buf) {
			binary.LittleEndian.PutUint32(buf[off:], math.Float32bits(p.num))
		}
	case paramRaw:
		if off+len(p.raw) <= len(buf) {
			copy(buf[off:], p.raw)
		}
	}
}

func (t *translator) ensurePipeline(backend Backend, shader ShaderID, m *MeshDescr, state MaterialState, pass PassDescr) PipelineID {
	stride := m.stride()
	layout, ok := vertexLayoutKeyOf(m.layout)
	if !ok {
		return 0
	}
	// One colour format exists today, the frame buffer's, and every renderable
	// texture in the tree is allocated in it - so the sentinel is still right
	// for every pass that has a colour attachment at all. What is not
	// interchangeable is having one: a depth-only pass has no colour
	// attachment, and a pipeline that declares a target it will never be given
	// is rejected at setPipeline.
	const colorFormat, depthFormat = FormatScreen, FormatDepth32F
	noColor := pass.Target.IsNone()
	k := pipelineKey{
		shader: shader, topology: m.topology, state: state,
		colorFormat: colorFormat, depthFormat: depthFormat, noColor: noColor, layout: layout,
	}
	if id, ok := t.pipelines[k]; ok {
		return id
	}
	attrs := make([]VertexAttribute, len(m.layout))
	for i := range m.layout {
		attrs[i] = VertexAttribute{Offset: m.layout[i].offset, Type: m.layout[i].typ, Location: i}
	}
	id, err := backend.NewPipeline(PipelineDesc{
		Shader:        shader,
		Topology:      m.topology,
		State:         state,
		ColorFormat:   colorFormat,
		DepthFormat:   depthFormat,
		NoColorTarget: noColor,
		Stride:        stride,
		Attributes:    attrs,
		Label:         "gfx.pipeline",
	})
	if err != nil {
		return 0
	}
	t.pipelines[k] = id
	return id
}

func (t *translator) ensureSampler(backend Backend, desc SamplerDesc) SamplerID {
	if id, ok := t.samplers[desc]; ok {
		return id
	}
	id, err := backend.NewSampler(desc)
	if err != nil {
		return 0
	}
	t.samplers[desc] = id
	return id
}

func writeMat4(dst []byte, m m.Mat4) {
	for i := 0; i < 16; i++ {
		binary.LittleEndian.PutUint32(dst[i*4:], math.Float32bits(m[i]))
	}
}

func writeColor(dst []byte, c m.Color) {
	binary.LittleEndian.PutUint32(dst[0:], math.Float32bits(c.R))
	binary.LittleEndian.PutUint32(dst[4:], math.Float32bits(c.G))
	binary.LittleEndian.PutUint32(dst[8:], math.Float32bits(c.B))
	binary.LittleEndian.PutUint32(dst[12:], math.Float32bits(c.A))
}

func writeVec4(dst []byte, v m.Vec4) {
	binary.LittleEndian.PutUint32(dst[0:], math.Float32bits(v.X))
	binary.LittleEndian.PutUint32(dst[4:], math.Float32bits(v.Y))
	binary.LittleEndian.PutUint32(dst[8:], math.Float32bits(v.Z))
	binary.LittleEndian.PutUint32(dst[12:], math.Float32bits(v.W))
}
