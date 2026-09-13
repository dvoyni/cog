package gfximpl

import (
	"cmp"
	"encoding/binary"
	"io/fs"
	"math"
	"slices"

	"github.com/dvoyni/cog/extensions/gfx"
	"github.com/dvoyni/cog/extensions/gfx/gpu"
	"github.com/dvoyni/cog/extensions/gfx/internal"
)

// uniformMax caps a per-draw shader-parameter block.
const uniformMax = 256

// pipelineKey identifies a cached pipeline by shader identity, render state and
// the formats of the attachments it renders into. MaterialState is embedded
// whole so that adding a state field cannot silently return a pipeline built
// for the old one.
type pipelineKey struct {
	shader      gpu.ShaderID
	topology    gpu.PrimitiveTopology
	state       gpu.MaterialState
	colorFormat gpu.TextureFormat
	depthFormat gpu.TextureFormat
	// noColor separates the pipeline a shader needs in a depth-only pass from
	// the one it needs in a colour pass. Without it in the key, a shader drawn
	// in both gets whichever pass reached it first, which is a validation
	// failure in the other one.
	noColor bool
	layout  internal.VertexLayoutKey
	// stripIndex is the index width a strip topology's pipeline has to declare,
	// and nothing at all for every other topology. It is the strip format
	// rather than the mesh's width because keying on the width unconditionally
	// would build two identical pipelines for two triangle lists differing only
	// in an encoding detail the pipeline never sees.
	stripIndex stripIndexKey
}

// stripIndexKey is a pipeline key's view of an index width: the format that
// cuts a strip, or the zero value for a topology that has no strip to cut.
type stripIndexKey uint8

const (
	stripIndexNone stripIndexKey = iota
	stripIndexUint16
	stripIndexUint32
)

func stripIndexKeyOf(topology gpu.PrimitiveTopology, width gpu.IndexWidth) stripIndexKey {
	if topology != gpu.TopologyTriangleStrip {
		return stripIndexNone
	}
	if width == gpu.IndexUint16 {
		return stripIndexUint16
	}
	return stripIndexUint32
}

// translator turns an OpQueue into a gpu.Queue, lazily creating and caching
// backend shaders/pipelines/samplers and resolves every texture and buffer to a
// baked resource ID. It is owned by the plugin and used only on the driver's
// render thread inside ConsumeCmd.
type translator struct {
	shaders        map[gfx.ShaderDescr]*cachedShader
	pipelines      map[pipelineKey]gpu.PipelineID
	samplers       map[gpu.SamplerDesc]gpu.SamplerID
	uarena         []byte
	layouts        map[gpu.ShaderID]gpu.ShaderLayout
	textures       map[string]gfx.TextureDescr
	parameterPlans map[parameterPlanBucketKey][]cachedParameterPlan
	ops            gpu.Queue
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
	textureUsage map[gpu.TextureID]gpu.TextureUsage
	// runSampled is the scratch set of textures one merged run samples, reused
	// across runs so a frame allocates nothing per pass.
	runSampled []gpu.TextureID
	// diagnostic holds a report that does not stop the frame - a shader over the
	// web floor still renders here - until translate surfaces it.
	diagnostic error
	// badIndexLengths is the set of malformed index-buffer shapes already
	// reported, so one whose length does not divide by its declared width is
	// reported once rather than at frame rate. It is the index twin of the
	// failed-pipeline cache entry.
	//
	// It is keyed by the shape rather than by the buffer id because a mesh
	// carrying inline bytes is re-baked into a fresh id every frame, which is
	// exactly the case the quiet exists for; the cost is that two meshes broken
	// the same way report once between them.
	badIndexLengths map[indexLengthKey]struct{}
}

func newTranslator() *translator {
	return &translator{
		shaders:         map[gfx.ShaderDescr]*cachedShader{},
		pipelines:       map[pipelineKey]gpu.PipelineID{},
		samplers:        map[gpu.SamplerDesc]gpu.SamplerID{},
		layouts:         map[gpu.ShaderID]gpu.ShaderLayout{},
		textures:        map[string]gfx.TextureDescr{},
		parameterPlans:  map[parameterPlanBucketKey][]cachedParameterPlan{},
		textureUsage:    map[gpu.TextureID]gpu.TextureUsage{},
		badIndexLengths: map[indexLengthKey]struct{}{},
	}
}

// translate builds the op stream for the durable resource operations followed
// by the latest frame. The returned ops (and their payloads) are valid until
// the next translate call. It returns the first error encountered; valid draws
// are still translated.
func (t *translator) translate(
	queue *gfx.OpQueue, persistent []internal.Op, backend gpu.Backend, files func() fs.FS,
	capture gpu.CaptureDesc, capturing bool,
) (*gpu.Queue, error) {
	t.ops.Reset()

	need := len(internal.OpQueueOps(queue)) * uniformMax
	if cap(t.uarena) < need {
		t.uarena = make([]byte, need)
	}
	t.uarena = t.uarena[:need]

	var firstErr error
	uoff := 0
	// Resource ops belong to no pass: every bake is hoisted ahead of all of
	// them, so a pass can read anything the frame uploaded.
	translateResources := func(list []internal.Op) {
		for i := range list {
			op := &list[i]
			if op.Kind == internal.OpBakeBuffer {
				if len(op.Bytes) > 0 {
					t.ops.BakeBuffer(op.BufferID, op.BufferKind, op.BufferSize, op.Bytes)
				}
				continue
			}
			if op.Kind == internal.OpReleaseBuffer {
				t.ops.ReleaseBuffer(op.BufferID)
				continue
			}
			if op.Kind == internal.OpBakeTexture {
				if len(op.Bytes) > 0 {
					t.ops.BakeTexture(op.TextureID, op.TexW, op.TexH, op.Format, op.Bytes, op.Mipmaps)
				}
				continue
			}
			if op.Kind == internal.OpReleaseTexture {
				t.ops.ReleaseTexture(op.TextureID)
				continue
			}
			if op.Kind == internal.OpReleaseCachedResource {
				t.releaseCachedResource(backend, op.Path)
				continue
			}
			if op.Kind == internal.OpFreeCachedResources {
				t.freeCachedResources(backend)
				continue
			}
			if op.Kind == internal.OpAllocateTexture {
				t.ops.AllocateTexture(op.TextureID, gpu.TextureDesc{
					Width: op.TexW, Height: op.TexH, Layers: op.TexLayers, Format: op.Format,
					Renderable: op.Renderable,
				})
				continue
			}
			if op.Kind == internal.OpUpdateTexture {
				t.ops.UpdateTexture(op.TextureID, op.TexLayer, op.Region, op.Bytes)
				continue
			}
		}
	}
	translateResources(persistent)
	translateResources(internal.OpQueueOps(queue))

	t.translatePasses(queue, backend, files, &uoff, &firstErr, capture, capturing)

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
	queue *gfx.OpQueue, backend gpu.Backend, files func() fs.FS, uoff *int, firstErr *error,
	capture gpu.CaptureDesc, capturing bool,
) {
	passes, ops := internal.OpQueuePasses(queue), internal.OpQueueOps(queue)
	t.planPasses(queue)
	if t.strayDraws > 0 && *firstErr == nil {
		*firstErr = gfx.ErrDrawWithoutPass{Count: t.strayDraws}
	}
	// Attachment roles are per frame: the pool hands the same texture id to a
	// different purpose next frame, and every frame's barriers are encoded from
	// scratch anyway.
	clear(t.textureUsage)
	presents := false
	for i := 0; i < len(t.passOrder); {
		head := passes[t.passOrder[i]].Desc
		tail, draws := head, t.passDrawCount(t.passOrder[i])
		last := i
		for j := i + 1; j < len(t.passOrder); j++ {
			next := passes[t.passOrder[j]].Desc
			if !internal.MergesInto(next, tail) {
				break
			}
			tail, last = next, j
			draws += t.passDrawCount(t.passOrder[j])
		}
		if !internal.PassHasEffect(&head, draws) {
			i = last + 1
			continue
		}
		presents = presents || head.Target.IsScreen()
		t.transitionRun(queue, head, i, last)
		t.ops.BeginPass(t.gpuPassDesc(backend, head, tail))
		for j := i; j <= last; j++ {
			pass := &passes[t.passOrder[j]]
			for _, index := range t.passDrawOps(t.passOrder[j]) {
				t.translateDraw(&ops[index], pass.Desc, backend, files, uoff, firstErr)
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
			t.transitionTo(capture.Texture, gpu.TextureUsageCopySrc)
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
func (t *translator) transitionRun(queue *gfx.OpQueue, head gfx.PassDescr, first, last int) {
	ops := internal.OpQueueOps(queue)
	// Reads first: a texture this run samples has to have finished being written.
	t.runSampled = t.runSampled[:0]
	for j := first; j <= last; j++ {
		for _, index := range t.passDrawOps(t.passOrder[j]) {
			op := &ops[index]
			t.collectSampled(op.Material.Params())
			t.collectSampled(op.Params)
		}
	}
	for _, texture := range t.runSampled {
		t.transitionTo(texture, gpu.TextureUsageTextureBinding)
	}
	// Then writes: this run's own attachments. A texture that was sampled
	// earlier in the frame is transitioned back before it is written again.
	if internal.TargetKindOf(&head.Target) == internal.TargetTexture {
		t.transitionTo(internal.TargetTextureOf(&head.Target), gpu.TextureUsageRenderAttachment)
	}
	if internal.DepthKindOf(&head.Depth) == internal.DepthKindTexture {
		t.transitionTo(internal.DepthTexture(&head.Depth), gpu.TextureUsageRenderAttachment)
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
func (t *translator) collectSampled(params []gfx.ParameterDescr) {
	for i := range params {
		p := &params[i]
		if internal.ParameterKind(p) != internal.ParamTexture {
			continue
		}
		texture := internal.ParameterTextureRef(p)
		if internal.TextureSource(texture) != gfx.TextureSourceBaked || texture.ID() == 0 {
			continue
		}
		if !slices.Contains(t.runSampled, texture.ID()) {
			t.runSampled = append(t.runSampled, texture.ID())
		}
	}
}

// transitionTo moves one texture into a usage, emitting a barrier only when
// that is a change from a role the frame has already put it in. A texture that
// has never been an attachment this frame has no writes to order against, and
// one already in the usage is a no-op the backend should not pay for.
func (t *translator) transitionTo(texture gpu.TextureID, to gpu.TextureUsage) {
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
	t.ops.TransitionTexture(gpu.TextureTransition{Texture: texture, From: from, To: to})
	t.textureUsage[texture] = to
}

// gpuPassDesc resolves a merged run's attachments: it loads like the pass that
// opened the run and stores like the one that closed it.
func (t *translator) gpuPassDesc(backend gpu.Backend, head, tail gfx.PassDescr) gpu.PassDesc {
	desc := gpu.PassDesc{
		Load: head.Load, Clear: head.Clear, Store: tail.Store,
		DepthLoad: head.DepthLoad, DepthClear: head.DepthClear, DepthStore: tail.DepthStore,
		Label: head.Label,
	}
	switch internal.TargetKindOf(&head.Target) {
	case internal.TargetScreen:
		desc.Screen = true
	case internal.TargetNone:
		desc.NoColor = true
	case internal.TargetTexture:
		// This can resolve to zero: a temporary target is allocated by the same
		// frame's bakes, which the backend replays after these descriptors were
		// built, so a target used for the first time has no view yet. NoColor
		// stays false, which is what keeps that case distinguishable from a
		// pass that declares no colour attachment at all.
		desc.Target = backend.TextureView(internal.TargetTextureOf(&head.Target), internal.TargetMip(&head.Target), internal.TargetLayer(&head.Target))
	}
	switch internal.DepthKindOf(&head.Depth) {
	case internal.DepthKindAuto:
		desc.DepthAuto = true
	case internal.DepthKindTexture:
		desc.Depth = backend.TextureView(internal.DepthTexture(&head.Depth), 0, 0)
	}
	return desc
}

// translateDraw emits one draw into the currently open pass.
func (t *translator) translateDraw(op *internal.Op, pass gfx.PassDescr, backend gpu.Backend, files func() fs.FS, uoff *int, firstErr *error) {
	m := &op.Mesh
	stride := internal.MeshStride(m)
	vertices, indices := internal.MeshVertices(m), internal.MeshIndices(m)
	if vertices.ID() == 0 || m.VertexCount() <= 0 || stride <= 0 {
		return
	}
	// The index buffer is checked before anything is emitted for this draw, so
	// a dropped one leaves no orphaned pipeline or binding behind it. It is the
	// one thing gfx can say about an index buffer without walking it, and
	// MeshIndexed is a pure value constructor with nowhere to say it.
	if m.Indexed() && indices.ID() != 0 && indices.Size()%m.IndexWidth().Bytes() != 0 {
		if err := t.reportIndexLength(m, internal.ShaderLabel(op.Material.Shader())); err != nil && *firstErr == nil {
			*firstErr = err
		}
		return
	}
	// Every shader failure is fatal to its draw, whether or not this frame is the
	// one that reports it: the error surfaces once, the draw is dropped every
	// time. ErrShaderExceedsWebLimits stays the only report in gfx that drops
	// nothing at all.
	shaderID, err := t.ensureShader(backend, files, op.Material.Shader())
	if err != nil && *firstErr == nil {
		*firstErr = err
	}
	if shaderID == 0 {
		return
	}
	label := internal.ShaderLabel(op.Material.Shader())
	// A vertex layout that does not supply what the shader reads is fatal to
	// the draw on the same terms a shader failure is: it reports on the frame
	// that built the pipeline and drops the draw on every frame after it.
	pipeline, err := t.ensurePipeline(backend, shaderID, label, m, op.Material.State(), pass)
	if err != nil && *firstErr == nil {
		*firstErr = err
	}
	if pipeline == 0 {
		return
	}

	layout := t.shaderLayout(backend, shaderID)
	plan := t.prepareParameterPlan(shaderID, label, layout, op.Material.Params(), op.Params)
	if plan.mismatch != nil {
		if *firstErr == nil {
			*firstErr = plan.mismatch
		}
		return
	}
	if name, ok := sampledAttachment(plan, op.Params, op.Material.Params(), pass); ok {
		if *firstErr == nil {
			*firstErr = gfx.ErrDrawSamplesAttachment{Pass: pass.Label, Parameter: name}
		}
		return
	}
	t.ops.SetPipeline(pipeline)
	// A shader that declares no uniform block gets no uniform binding and no
	// pooled buffer. Emitting one anyway puts an entry in a group the pipeline
	// layout does not have, and CreateBindGroup fails the entry-count rule with
	// the whole frame's command buffer as the casualty.
	if plan.uniformSize > 0 {
		u := t.packParams(t.uarena[*uoff:*uoff+uniformMax], op.Params, op.Material.Params(), plan)
		*uoff += uniformMax
		t.ops.SetParams(u)
	}
	t.emitResources(backend, files, op.Params, op.Material.Params(), plan)
	t.ops.SetVertexBuffer(vertices.ID(), 0)

	instances := op.Instances
	if instances < 1 {
		instances = 1
	}
	if m.Indexed() && indices.ID() != 0 && m.IndexCount() > 0 {
		t.ops.SetIndexBuffer(indices.ID(), 0, m.IndexWidth())
		t.ops.Draw(0, m.IndexCount(), instances, op.FirstInstance, true)
	} else {
		t.ops.Draw(0, m.VertexCount(), instances, op.FirstInstance, false)
	}
}

// indexLengthKey is one malformed index buffer's shape: the byte length that
// did not divide, and the width it was declared at.
type indexLengthKey struct {
	length int
	width  gpu.IndexWidth
}

// reportIndexLength returns the report for a malformed index buffer the first
// time that shape is seen, and nothing on the frames after it. The draw is
// dropped either way: the caller returns before emitting anything, so
// report-once-drop-always holds here the way it does for a failed pipeline.
func (t *translator) reportIndexLength(m *gfx.MeshDescr, label string) error {
	key := indexLengthKey{length: internal.MeshIndices(m).Size(), width: m.IndexWidth()}
	if _, seen := t.badIndexLengths[key]; seen {
		return nil
	}
	t.badIndexLengths[key] = struct{}{}
	return gfx.ErrIndexBufferLength{Shader: label, Length: key.length, Width: key.width.Bytes()}
}

// sampledAttachment names the first texture parameter a draw samples that its
// own pass renders into. Only a baked texture can be an attachment, so this
// resolves nothing and costs a comparison per binding.
func sampledAttachment(plan *parameterPlan, drawParams, materialParams []gfx.ParameterDescr, pass gfx.PassDescr) (string, bool) {
	attachment := func(id gpu.TextureID) bool {
		if id == 0 {
			return false
		}
		return (internal.TargetKindOf(&pass.Target) == internal.TargetTexture && internal.TargetTextureOf(&pass.Target) == id) ||
			(internal.DepthKindOf(&pass.Depth) == internal.DepthKindTexture && internal.DepthTexture(&pass.Depth) == id)
	}
	for i := range plan.resources {
		resource := &plan.resources[i]
		if resource.kind != plannedTexture {
			continue
		}
		p := resource.param.value(materialParams, drawParams)
		if p == nil || internal.ParameterKind(p) != internal.ParamTexture {
			continue
		}
		if texture := internal.ParameterTextureRef(p); internal.TextureSource(texture) == gfx.TextureSourceBaked && attachment(texture.ID()) {
			return p.Name(), true
		}
	}
	return "", false
}

// planPasses puts the frame's passes in run order - Order first, declaration
// sequence breaking ties - and buckets each pass's draws behind it.
func (t *translator) planPasses(queue *gfx.OpQueue) {
	passes, ops := internal.OpQueuePasses(queue), internal.OpQueueOps(queue)
	count := len(passes)
	t.passOrder = t.passOrder[:0]
	for i := range count {
		t.passOrder = append(t.passOrder, i)
	}
	slices.SortStableFunc(t.passOrder, func(a, b int) int {
		return cmp.Compare(passes[a].Desc.Order, passes[b].Desc.Order)
	})

	t.passStart = slices.Grow(t.passStart[:0], count+1)[:count+1]
	clear(t.passStart)
	t.strayDraws = 0
	for i := range ops {
		if ops[i].Kind != internal.OpDraw {
			continue
		}
		if pass := int(ops[i].Pass); pass >= 0 && pass < count {
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
	for i := range ops {
		if pass := int(ops[i].Pass); ops[i].Kind == internal.OpDraw && pass >= 0 && pass < count {
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
func (t *translator) emitResources(backend gpu.Backend, files func() fs.FS, drawParams, materialParams []gfx.ParameterDescr, plan *parameterPlan) {
	// Each reflected sampler is filled by the parameter of its own name, and
	// falls back to the zero descriptor - clamp and linear - when unset.
	for i := range plan.samplers {
		sampler := &plan.samplers[i]
		var desc gpu.SamplerDesc
		if p := sampler.param.value(materialParams, drawParams); p != nil && internal.ParameterKind(p) == internal.ParamSampler {
			desc = internal.ParameterSampler(p)
		}
		t.ops.SetSampler(t.ensureSampler(backend, desc), sampler.group, sampler.binding)
	}
	for i := range plan.resources {
		resource := &plan.resources[i]
		p := resource.param.value(materialParams, drawParams)
		if resource.kind == plannedBuffer {
			if p != nil && internal.ParameterKind(p) == internal.ParamBuffer {
				if internal.ParameterBuffer(p).ID() != 0 {
					t.ops.SetBuffer(resource.group, resource.binding, internal.ParameterBuffer(p).ID(), internal.ParameterBufferOffset(p), internal.ParameterBufferSize(p))
				}
			}
			continue
		}
		textureID := gpu.TextureID(0)
		if p != nil && internal.ParameterKind(p) == internal.ParamTexture {
			textureID = t.ensureTexture(backend, files, internal.ParameterTexture(p))
		}
		t.ops.SetTexture(textureID, resource.group, resource.binding)
	}
}

func (t *translator) ensureTexture(backend gpu.Backend, files func() fs.FS, descr gfx.TextureDescr) gpu.TextureID {
	if internal.TextureSource(&descr) == gfx.TextureSourceBaked {
		return descr.ID()
	}
	if internal.TextureSource(&descr) != gfx.TextureSourceResource {
		return 0
	}
	if baked, ok := t.textures[descr.Path()]; ok {
		return baked.ID()
	}
	width, height, pixels, ok := internal.LoadTextureResource(files(), descr.Path())
	if !ok {
		return 0
	}
	id := backend.NewTexture()
	t.ops.BakeTexture(id, width, height, descr.Format(), pixels, false)
	t.textures[descr.Path()] = internal.BakedTexture(id, 0, 0)
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
	id       gpu.ShaderID
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

func (t *translator) ensureShader(backend gpu.Backend, files func() fs.FS, descr gfx.ShaderDescr) (gpu.ShaderID, error) {
	if cached, ok := t.shaders[descr]; ok {
		return cached.id, cached.report()
	}
	label := internal.ShaderLabel(descr)
	// Flatten happens here, on the render thread, on a cache miss only - the
	// first draw of a given (root, supply). The cost changes from one file read
	// to N, which is the same shape as today's hitch rather than a new class of
	// problem: if it ever bites, it bites the first frame a material appears,
	// which is already true.
	flattened, err := internal.FlattenShader(files(), descr)
	// The include set is recorded on failure as well as on success, so that a
	// failed entry evicts like any other and the developer loop stays: fix the
	// file, hot-reload evicts, the next frame retries and reports afresh.
	cached := &cachedShader{sources: flattened.Sources}
	t.shaders[descr] = cached
	if err != nil {
		cached.err = err
		return 0, cached.report()
	}
	id, err := backend.NewShader(gpu.ShaderDesc{Code: []byte(flattened.Text), Label: label})
	if err != nil {
		// Nothing the backend said is rewritten and no line number is parsed out
		// of its message: gfx appends the rendered segment table and lets the
		// reader subtract.
		cached.err = internal.CompileError(label, err, flattened.SourceMap)
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

func (t *translator) releaseCachedResource(backend gpu.Backend, path string) {
	if texture, ok := t.textures[path]; ok {
		t.ops.ReleaseTexture(texture.ID())
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

func (t *translator) releaseShader(backend gpu.Backend, descr gfx.ShaderDescr, cached *cachedShader) {
	delete(t.shaders, descr)
	if cached.id == 0 {
		return
	}
	for key, pipeline := range t.pipelines {
		if key.shader != cached.id {
			continue
		}
		// A zero entry is the marker for a pipeline that failed to build, not
		// a resource: there is nothing to hand back.
		if pipeline != 0 {
			backend.FreePipeline(pipeline)
		}
		delete(t.pipelines, key)
	}
	for key := range t.parameterPlans {
		if key.shader == cached.id {
			delete(t.parameterPlans, key)
		}
	}
	delete(t.layouts, cached.id)
	backend.FreeShader(cached.id)
}

func (t *translator) freeCachedResources(backend gpu.Backend) {
	for _, texture := range t.textures {
		t.ops.ReleaseTexture(texture.ID())
	}
	for _, pipeline := range t.pipelines {
		if pipeline != 0 {
			backend.FreePipeline(pipeline)
		}
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
	clear(t.badIndexLengths)
}

// shaderLayout returns the backend's reflected layout for a shader, cached by id.
func (t *translator) shaderLayout(backend gpu.Backend, id gpu.ShaderID) gpu.ShaderLayout {
	if l, ok := t.layouts[id]; ok {
		return l
	}
	l := backend.ShaderLayout(id)
	t.layouts[id] = l
	return l
}

// packParams writes reflected shader constants into dst. Per-draw parameters
// override same-named material parameters; unmatched members remain zero.
func (t *translator) packParams(dst []byte, drawParams, materialParams []gfx.ParameterDescr, plan *parameterPlan) []byte {
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
func writeParamAt(buf []byte, off int, p *gfx.ParameterDescr) {
	switch internal.ParameterKind(p) {
	case internal.ParamColor:
		if off+16 <= len(buf) {
			internal.WriteColor(buf[off:off+16], internal.ParameterColor(p))
		}
	case internal.ParamVec4:
		if off+16 <= len(buf) {
			internal.WriteVec4(buf[off:off+16], internal.ParameterVec(p))
		}
	case internal.ParamMat4:
		if off+64 <= len(buf) {
			internal.WriteMat4(buf[off:off+64], internal.ParameterMat(p))
		}
	case internal.ParamFloat:
		if off+4 <= len(buf) {
			binary.LittleEndian.PutUint32(buf[off:], math.Float32bits(internal.ParameterNum(p)))
		}
	case internal.ParamRaw:
		if off+len(internal.ParameterRaw(p)) <= len(buf) {
			copy(buf[off:], internal.ParameterRaw(p))
		}
	}
}

// ensurePipeline returns the cached pipeline for one (shader, mesh layout,
// state, attachments) combination, building it on a miss.
//
// It returns an error only on the miss that produced it, which is what makes
// report-once-drop-always fall out of the cache that already exists: a failure
// is cached as the zero id, so every frame after the first finds `ok` true,
// returns zero and no error, and the caller drops the draw on the zero id
// exactly as it did before.
func (t *translator) ensurePipeline(
	backend gpu.Backend, shader gpu.ShaderID, label string, m *gfx.MeshDescr, state gpu.MaterialState, pass gfx.PassDescr,
) (gpu.PipelineID, error) {
	stride := internal.MeshStride(m)
	layout, ok := internal.VertexLayoutKeyOf(internal.MeshLayout(m))
	if !ok {
		return 0, nil
	}
	// One colour format exists today, the frame buffer's, and every renderable
	// texture in the tree is allocated in it - so the sentinel is still right
	// for every pass that has a colour attachment at all. What is not
	// interchangeable is having one: a depth-only pass has no colour
	// attachment, and a pipeline that declares a target it will never be given
	// is rejected at setPipeline.
	const colorFormat, depthFormat = gpu.FormatScreen, gpu.FormatDepth32F
	noColor := pass.Target.IsNone()
	k := pipelineKey{
		shader: shader, topology: m.Topology(), state: state,
		colorFormat: colorFormat, depthFormat: depthFormat, noColor: noColor, layout: layout,
		stripIndex: stripIndexKeyOf(m.Topology(), m.IndexWidth()),
	}
	if id, ok := t.pipelines[k]; ok {
		return id, nil
	}
	// The vertex interface is checked before the backend is asked for anything,
	// because no backend checks it: gogpu performs no vertex-interface
	// validation of any kind, the software rasterizer keeps an unsupplied
	// input's zero value, and WebGPU itself fills the components a format does
	// not supply with (0, 0, 0, 1).
	if err := gfx.CheckVertexInterface(label, t.shaderLayout(backend, shader), internal.MeshLayout(m)); err != nil {
		t.pipelines[k] = 0
		return 0, err
	}
	attrs := make([]gpu.VertexAttribute, len(internal.MeshLayout(m)))
	for i := range internal.MeshLayout(m) {
		attrs[i] = gpu.VertexAttribute{Offset: internal.VertexAttrOffset(&(internal.MeshLayout(m)[i])), Type: internal.VertexAttrTyp(&(internal.MeshLayout(m)[i])), Location: i}
	}
	id, err := backend.NewPipeline(gpu.PipelineDesc{
		Shader:        shader,
		Topology:      m.Topology(),
		State:         state,
		ColorFormat:   colorFormat,
		DepthFormat:   depthFormat,
		NoColorTarget: noColor,
		Stride:        stride,
		Attributes:    attrs,
		IndexWidth:    m.IndexWidth(),
		Label:         "gfx.pipeline",
	})
	if err != nil {
		t.pipelines[k] = 0
		return 0, gfx.ErrPipelineFailed{Shader: label, Err: err}
	}
	t.pipelines[k] = id
	return id, nil
}

func (t *translator) ensureSampler(backend gpu.Backend, desc gpu.SamplerDesc) gpu.SamplerID {
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
