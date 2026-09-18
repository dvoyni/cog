package internal

import (
	"cmp"
	"encoding/binary"
	"io/fs"
	"math"
	"slices"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/assets"
	"github.com/dvoyni/cog/slots/gfx"
	"github.com/dvoyni/cog/slots/gfx/internal/types"
)

// frame is the three values one dispatch of translate carries all the way down
// to a cache: the kernel a load reports its failures through, the filesystem it
// reads from, and the backend it mints handles on.
//
// They travel as one struct threaded by pointer because they would otherwise
// grow six signatures by two parameters each, and they travel per dispatch
// rather than as translator fields because none of the three may be retained
// past the handler that granted it.
type frame struct {
	k       kernel.Kernel
	fsys    fs.FS
	backend gfx.Backend
}

// uniformMax caps a per-draw shader-parameter block.
const uniformMax = 256

// pipelineKey identifies a cached pipeline by shader identity, render state and
// the formats of the attachments it renders into. MaterialState is embedded
// whole so that adding a state field cannot silently return a pipeline built
// for the old one.
type pipelineKey struct {
	shader      gfx.ShaderID
	topology    gfx.PrimitiveTopology
	state       gfx.MaterialState
	colorFormat gfx.TextureFormat
	depthFormat gfx.TextureFormat
	// noColor separates the pipeline a shader needs in a depth-only pass from
	// the one it needs in a colour pass. Without it in the key, a shader drawn
	// in both gets whichever pass reached it first, which is a validation
	// failure in the other one.
	noColor bool
	layout  types.VertexLayoutKey
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

func stripIndexKeyOf(topology gfx.PrimitiveTopology, width gfx.IndexWidth) stripIndexKey {
	if topology != gfx.TopologyTriangleStrip {
		return stripIndexNone
	}
	if width == gfx.IndexUint16 {
		return stripIndexUint16
	}
	return stripIndexUint32
}

// translator turns an OpQueue into a gfx.Queue, lazily creating and caching
// backend shaders/pipelines/samplers and resolves every texture and buffer to a
// baked resource ID. It is owned by the plugin and used only on the driver's
// render thread inside ConsumeCmd.
type translator struct {
	// shaders is the module cache, keyed on the whole descriptor - a root plus
	// one supply is one variant - and holding a compiled id or the failure that
	// stood in for one. It is a translator field like every other cache here,
	// reached only on the render thread, so what protects it is the confinement
	// rather than a lock of its own.
	shaders   *assets.Cache[types.ShaderDescrParams, shaderUser, *shader]
	pipelines map[pipelineKey]gfx.PipelineID
	samplers  map[gfx.SamplerDesc]gfx.SamplerID
	uarena    []byte
	layouts   map[gfx.ShaderID]gfx.ShaderLayout
	// textures is the path-texture cache. It is a translator field like every
	// other cache here, reached only on the render thread, so what protects it
	// is the confinement rather than a lock of its own.
	textures       *assets.Cache[types.TextureDescrParams, textureUser, texture]
	parameterPlans map[parameterPlanBucketKey][]cachedParameterPlan
	ops            gfx.Queue
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
	textureUsage map[gfx.TextureID]gfx.TextureUsage
	// runSampled is the scratch set of textures one merged run samples, reused
	// across runs so a frame allocates nothing per pass.
	runSampled []gfx.TextureID
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
	// unsuppliedBuffers is the set of storage bindings already reported as
	// unfilled, so a material that misses one is named once rather than at
	// frame rate. It is keyed by shader and parameter name rather than by
	// plan, because a plan is keyed by parameter shape and two materials of
	// one shape carrying different buffers are two faults.
	unsuppliedBuffers map[unsuppliedBufferKey]struct{}
}

func newTranslator() *translator {
	return &translator{
		shaders:           assets.New[types.ShaderDescrParams, shaderUser, *shader](shaderLoader{}),
		pipelines:         map[pipelineKey]gfx.PipelineID{},
		samplers:          map[gfx.SamplerDesc]gfx.SamplerID{},
		layouts:           map[gfx.ShaderID]gfx.ShaderLayout{},
		textures:          assets.New[types.TextureDescrParams, textureUser, texture](textureLoader{}),
		parameterPlans:    map[parameterPlanBucketKey][]cachedParameterPlan{},
		textureUsage:      map[gfx.TextureID]gfx.TextureUsage{},
		badIndexLengths:   map[indexLengthKey]struct{}{},
		unsuppliedBuffers: map[unsuppliedBufferKey]struct{}{},
	}
}

// translate builds the op stream for the durable resource operations followed
// by the latest frame. The returned ops (and their payloads) are valid until
// the next translate call. It returns the first error encountered; valid draws
// are still translated.
func (t *translator) translate(
	k kernel.Kernel, queue *gfx.OpQueue, persistent []types.Op, backend gfx.Backend, files func() fs.FS,
	capture gfx.CaptureDesc, capturing bool,
) (*gfx.Queue, error) {
	t.ops.Reset()

	// files() is called once, here, rather than once per cache miss. Handing
	// storage.FileSystem out as an fs.FS boxes it - 32 bytes, measured - and a
	// cache wants it materialised before every Get rather than only behind the
	// probe both ensure* functions used to do themselves. Paid per frame that is
	// nothing; paid per hit it is once a texture parameter and once a draw.
	f := &frame{k: k, fsys: files(), backend: backend}

	need := len(types.OpQueueOps(queue)) * uniformMax
	if cap(t.uarena) < need {
		t.uarena = make([]byte, need)
	}
	t.uarena = t.uarena[:need]

	var firstErr error
	uoff := 0
	// Resource ops belong to no pass: every bake is hoisted ahead of all of
	// them, so a pass can read anything the frame uploaded.
	translateResources := func(list []types.Op) {
		for i := range list {
			op := &list[i]
			if op.Kind == types.OpBakeBuffer {
				if len(op.Bytes) > 0 {
					t.ops.BakeBuffer(op.BufferID, op.BufferKind, op.BufferSize, op.Bytes)
				}
				continue
			}
			if op.Kind == types.OpReleaseBuffer {
				t.ops.ReleaseBuffer(op.BufferID)
				continue
			}
			if op.Kind == types.OpBakeTexture {
				if len(op.Bytes) > 0 {
					t.ops.BakeTexture(op.TextureID, op.TexW, op.TexH, op.Format, op.Bytes, op.Mipmaps)
				}
				continue
			}
			if op.Kind == types.OpReleaseTexture {
				t.ops.ReleaseTexture(op.TextureID)
				continue
			}
			if op.Kind == types.OpReleaseCachedResource {
				t.releaseCachedResource(f, op.Path)
				continue
			}
			if op.Kind == types.OpFreeCachedResources {
				t.freeCachedResources(f)
				continue
			}
			if op.Kind == types.OpAllocateTexture {
				t.ops.AllocateTexture(op.TextureID, gfx.TextureDesc{
					Width: op.TexW, Height: op.TexH, Layers: op.TexLayers, Format: op.Format,
					Renderable: op.Renderable,
				})
				continue
			}
			if op.Kind == types.OpUpdateTexture {
				t.ops.UpdateTexture(op.TextureID, op.TexLayer, op.Region, op.Bytes)
				continue
			}
		}
	}
	translateResources(persistent)
	translateResources(types.OpQueueOps(queue))

	t.translatePasses(f, queue, &uoff, &firstErr, capture, capturing)

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
	f *frame, queue *gfx.OpQueue, uoff *int, firstErr *error,
	capture gfx.CaptureDesc, capturing bool,
) {
	passes, ops := types.OpQueuePasses(queue), types.OpQueueOps(queue)
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
			if !types.MergesInto(next, tail) {
				break
			}
			tail, last = next, j
			draws += t.passDrawCount(t.passOrder[j])
		}
		if !types.PassHasEffect(&head, draws) {
			i = last + 1
			continue
		}
		presents = presents || head.Target.IsScreen()
		t.transitionRun(queue, head, i, last)
		t.ops.BeginPass(t.gpuPassDesc(f.backend, head, tail))
		for j := i; j <= last; j++ {
			pass := &passes[t.passOrder[j]]
			for _, index := range t.passDrawOps(t.passOrder[j]) {
				t.translateDraw(f, &ops[index], pass.Desc, uoff, firstErr)
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
			t.transitionTo(capture.Texture, gfx.TextureUsageCopySrc)
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
	ops := types.OpQueueOps(queue)
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
		t.transitionTo(texture, gfx.TextureUsageTextureBinding)
	}
	// Then writes: this run's own attachments. A texture that was sampled
	// earlier in the frame is transitioned back before it is written again.
	if types.TargetKindOf(&head.Target) == types.TargetTexture {
		t.transitionTo(types.TargetTextureOf(&head.Target), gfx.TextureUsageRenderAttachment)
	}
	if types.DepthKindOf(&head.Depth) == types.DepthKindTexture {
		t.transitionTo(types.DepthTexture(&head.Depth), gfx.TextureUsageRenderAttachment)
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
		if types.ParameterKind(p) != types.ParamTexture {
			continue
		}
		// Only a baked texture carries an id, so the id is the whole test: a
		// path or an inline run has nothing an attachment could collide with.
		texture := types.ParameterTextureRef(p)
		if texture.ID() == 0 {
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
func (t *translator) transitionTo(texture gfx.TextureID, to gfx.TextureUsage) {
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
	t.ops.TransitionTexture(gfx.TextureTransition{Texture: texture, From: from, To: to})
	t.textureUsage[texture] = to
}

// gpuPassDesc resolves a merged run's attachments: it loads like the pass that
// opened the run and stores like the one that closed it.
func (t *translator) gpuPassDesc(backend gfx.Backend, head, tail gfx.PassDescr) gfx.PassDesc {
	desc := gfx.PassDesc{
		Load: head.Load, Clear: head.Clear, Store: tail.Store,
		DepthLoad: head.DepthLoad, DepthClear: head.DepthClear, DepthStore: tail.DepthStore,
		Label: head.Label,
	}
	switch types.TargetKindOf(&head.Target) {
	case types.TargetScreen:
		desc.Screen = true
	case types.TargetNone:
		desc.NoColor = true
	case types.TargetTexture:
		// This can resolve to zero: a temporary target is allocated by the same
		// frame's bakes, which the backend replays after these descriptors were
		// built, so a target used for the first time has no view yet. NoColor
		// stays false, which is what keeps that case distinguishable from a
		// pass that declares no colour attachment at all.
		desc.Target = backend.TextureView(types.TargetTextureOf(&head.Target), types.TargetMip(&head.Target), types.TargetLayer(&head.Target))
	}
	switch types.DepthKindOf(&head.Depth) {
	case types.DepthKindAuto:
		desc.DepthAuto = true
	case types.DepthKindTexture:
		desc.Depth = backend.TextureView(types.DepthTexture(&head.Depth), 0, 0)
	}
	return desc
}

// translateDraw emits one draw into the currently open pass.
func (t *translator) translateDraw(f *frame, op *types.Op, pass gfx.PassDescr, uoff *int, firstErr *error) {
	m := &op.Mesh
	stride := types.MeshStride(m)
	vertices, indices := types.MeshVertices(m), types.MeshIndices(m)
	if vertices.ID() == 0 || m.VertexCount() <= 0 || stride <= 0 {
		return
	}
	// The index buffer is checked before anything is emitted for this draw, so
	// a dropped one leaves no orphaned pipeline or binding behind it. It is the
	// one thing gfx can say about an index buffer without walking it, and
	// MeshIndexed is a pure value constructor with nowhere to say it.
	if m.Indexed() && indices.ID() != 0 && indices.Size()%m.IndexWidth().Bytes() != 0 {
		if err := t.reportIndexLength(m, types.ShaderLabel(op.Material.Shader())); err != nil && *firstErr == nil {
			*firstErr = err
		}
		return
	}
	// Every shader failure is fatal to its draw, whether or not this frame is the
	// one that reports it: the error surfaces once, the draw is dropped every
	// time. ErrShaderExceedsWebLimits stays the only report in gfx that drops
	// nothing at all.
	shaderID, err := t.ensureShader(f, op.Material.Shader())
	if err != nil && *firstErr == nil {
		*firstErr = err
	}
	if shaderID == 0 {
		return
	}
	label := types.ShaderLabel(op.Material.Shader())
	// A vertex layout that does not supply what the shader reads is fatal to
	// the draw on the same terms a shader failure is: it reports on the frame
	// that built the pipeline and drops the draw on every frame after it.
	pipeline, err := t.ensurePipeline(f.backend, shaderID, label, m, op.Material.State(), pass)
	if err != nil && *firstErr == nil {
		*firstErr = err
	}
	if pipeline == 0 {
		return
	}

	layout := t.shaderLayout(f.backend, shaderID)
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
	if resource, unbaked, ok := unsuppliedBuffer(plan, op.Params, op.Material.Params()); ok {
		if err := t.reportUnsuppliedBuffer(shaderID, label, resource, unbaked); err != nil && *firstErr == nil {
			*firstErr = err
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
	t.emitResources(f, op.Params, op.Material.Params(), plan)
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
	width  gfx.IndexWidth
}

// reportIndexLength returns the report for a malformed index buffer the first
// time that shape is seen, and nothing on the frames after it. The draw is
// dropped either way: the caller returns before emitting anything, so
// report-once-drop-always holds here the way it does for a failed pipeline.
func (t *translator) reportIndexLength(m *gfx.MeshDescr, label string) error {
	key := indexLengthKey{length: types.MeshIndices(m).Size(), width: m.IndexWidth()}
	if _, seen := t.badIndexLengths[key]; seen {
		return nil
	}
	t.badIndexLengths[key] = struct{}{}
	return gfx.ErrIndexBufferLength{Shader: label, Length: key.length, Width: key.width.Bytes()}
}

// unsuppliedBuffer returns the first declared storage binding the draw does not
// fill, and whether a parameter named it at all. It walks the same slice
// sampledAttachment does, for the same reason: the plan is cached per parameter
// shape, so it knows which names are declared but not which buffers this draw
// carries, and an unbaked buffer is a per-draw value.
func unsuppliedBuffer(plan *parameterPlan, drawParams, materialParams []gfx.ParameterDescr) (*plannedResource, bool, bool) {
	for i := range plan.resources {
		resource := &plan.resources[i]
		if resource.kind != plannedBuffer {
			continue
		}
		p := resource.param.value(materialParams, drawParams)
		if p == nil {
			return resource, false, true
		}
		// The kind is already settled in plan.mismatch, so a parameter that is
		// here at all is a buffer; only its id is still in question.
		if types.ParameterBuffer(p).ID() == 0 {
			return resource, true, true
		}
	}
	return nil, false, false
}

// unsuppliedBufferKey is one storage binding a shader never got filled. The
// parameter name is in it because one shader may declare several.
type unsuppliedBufferKey struct {
	shader    gfx.ShaderID
	parameter string
}

// reportUnsuppliedBuffer returns the report for an unfilled storage binding the
// first time that binding is seen, and nothing on the frames after it. The draw
// is dropped either way, on reportIndexLength's terms: a material that misses a
// binding misses it until someone fixes the material, and firstErr carries only
// the frame's first error, so re-reporting would mask every later error in
// every later frame.
func (t *translator) reportUnsuppliedBuffer(shader gfx.ShaderID, label string, resource *plannedResource, unbaked bool) error {
	key := unsuppliedBufferKey{shader: shader, parameter: resource.name}
	if _, seen := t.unsuppliedBuffers[key]; seen {
		return nil
	}
	t.unsuppliedBuffers[key] = struct{}{}
	return gfx.ErrStorageBufferUnsupplied{
		Shader: label, Parameter: resource.name,
		Group: resource.group, Binding: resource.binding, Unbaked: unbaked,
	}
}

// sampledAttachment names the first texture parameter a draw samples that its
// own pass renders into. Only a baked texture can be an attachment, so this
// resolves nothing and costs a comparison per binding.
func sampledAttachment(plan *parameterPlan, drawParams, materialParams []gfx.ParameterDescr, pass gfx.PassDescr) (string, bool) {
	attachment := func(id gfx.TextureID) bool {
		if id == 0 {
			return false
		}
		return (types.TargetKindOf(&pass.Target) == types.TargetTexture && types.TargetTextureOf(&pass.Target) == id) ||
			(types.DepthKindOf(&pass.Depth) == types.DepthKindTexture && types.DepthTexture(&pass.Depth) == id)
	}
	for i := range plan.resources {
		resource := &plan.resources[i]
		if resource.kind != plannedTexture {
			continue
		}
		p := resource.param.value(materialParams, drawParams)
		if p == nil || types.ParameterKind(p) != types.ParamTexture {
			continue
		}
		if texture := types.ParameterTextureRef(p); attachment(texture.ID()) {
			return p.Name(), true
		}
	}
	return "", false
}

// planPasses puts the frame's passes in run order - Order first, declaration
// sequence breaking ties - and buckets each pass's draws behind it.
func (t *translator) planPasses(queue *gfx.OpQueue) {
	passes, ops := types.OpQueuePasses(queue), types.OpQueueOps(queue)
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
		if ops[i].Kind != types.OpDraw {
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
		if pass := int(ops[i].Pass); ops[i].Kind == types.OpDraw && pass >= 0 && pass < count {
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
func (t *translator) emitResources(f *frame, drawParams, materialParams []gfx.ParameterDescr, plan *parameterPlan) {
	// Each reflected sampler is filled by the parameter of its own name, and
	// falls back to the zero descriptor - clamp and linear - when unset.
	for i := range plan.samplers {
		sampler := &plan.samplers[i]
		var desc gfx.SamplerDesc
		if p := sampler.param.value(materialParams, drawParams); p != nil && types.ParameterKind(p) == types.ParamSampler {
			desc = types.ParameterSampler(p)
		}
		t.ops.SetSampler(t.ensureSampler(f.backend, desc), sampler.group, sampler.binding)
	}
	for i := range plan.resources {
		resource := &plan.resources[i]
		p := resource.param.value(materialParams, drawParams)
		if resource.kind == plannedBuffer {
			if p != nil && types.ParameterKind(p) == types.ParamBuffer {
				if types.ParameterBuffer(p).ID() != 0 {
					t.ops.SetBuffer(resource.group, resource.binding, types.ParameterBuffer(p).ID(), types.ParameterBufferOffset(p), types.ParameterBufferSize(p))
				}
			}
			continue
		}
		textureID := gfx.TextureID(0)
		if p != nil && types.ParameterKind(p) == types.ParamTexture {
			textureID = t.ensureTexture(f, types.ParameterTexture(p))
		}
		t.ops.SetTexture(textureID, resource.group, resource.binding)
	}
}

// ensureTexture resolves one texture parameter to the id its binding is set
// from.
//
// The three descriptor cases split here, and only one of them is a cache's: a
// baked texture already carries its id and needs nothing, an inline run was
// baked into one when the frame was recorded, and a path is what the cache is
// for.
func (t *translator) ensureTexture(f *frame, descr gfx.TextureDescr) gfx.TextureID {
	if id := descr.ID(); id != 0 {
		return id
	}
	if descr.Path() == "" {
		return 0
	}
	return t.textures.Get(f.k, assets.Descr[types.TextureDescrParams](descr), f.fsys, t.textureUser(f)).id
}

// textureUser is what the texture loader is handed on every call. The op queue
// is the translator's own, so a bake or a release the loader emits lands in the
// frame being built exactly where the translator used to put it itself.
func (t *translator) textureUser(f *frame) textureUser {
	return textureUser{backend: f.backend, ops: &t.ops}
}

// ensureShader resolves one material's shader to the module id its draw is
// encoded against, and to the error that module has to say for itself.
func (t *translator) ensureShader(f *frame, descr gfx.ShaderDescr) (gfx.ShaderID, error) {
	cached := t.shaders.Get(
		f.k, assets.Descr[types.ShaderDescrParams](descr), f.fsys, t.shaderUser(f, descr.Path()),
	)
	return cached.id, cached.report()
}

// shaderUser is what the shader loader is handed on every call. root is the
// module's path on a load and empty on a free, which is the whole difference
// between the two: a free reads the value, and only a load needs to be told
// what the bytes it was given are called.
func (t *translator) shaderUser(f *frame, root string) shaderUser {
	return shaderUser{t: t, backend: f.backend, root: root}
}

func (t *translator) releaseCachedResource(f *frame, path string) {
	// A path names exactly one texture entry, because TextureWithResource is the
	// only way one is made and it takes no options - so the key a Free names is
	// the key a Get made, and the report that entry filed is forgotten with it.
	t.textures.Free(f.k, assets.Descr[types.TextureDescrParams](gfx.TextureWithResource(path)), t.textureUser(f))
	// A shader cannot be freed by key, because three things break the probe of
	// one descriptor: a path may root several variants, a path may be an
	// included source of modules rooted elsewhere, and a ShaderWithText shader
	// can include resources, so a text shader is evictable by a path it never
	// names. The decision is over the value, and the value already holds the
	// answer - which is what FreeWhere is, and why gfx builds no reverse
	// path-to-modules index to hold what the include set holds already.
	t.shaders.FreeWhere(f.k, t.shaderUser(f, ""), func(_ assets.Descr[types.ShaderDescrParams], value *shader) bool {
		return slices.Contains(value.sources, path)
	})
}

func (t *translator) freeCachedResources(f *frame) {
	// Pipelines and plans go first, and the order is the point. Freeing an entry
	// runs the loader's cascade, which sweeps both maps for the dead ShaderID;
	// left full, that is O(shaders x pipelines) across the FreeAll. Emptied
	// ahead of it, every sweep scans nothing and the pipelines are still handed
	// back exactly once - here, where the shader ids they were keyed on are
	// about to stop meaning anything.
	for _, pipeline := range t.pipelines {
		if pipeline != 0 {
			f.backend.FreePipeline(pipeline)
		}
	}
	clear(t.pipelines)
	clear(t.parameterPlans)

	t.textures.FreeAll(f.k, t.textureUser(f))
	t.shaders.FreeAll(f.k, t.shaderUser(f, ""))

	for _, sampler := range t.samplers {
		f.backend.FreeSampler(sampler)
	}
	clear(t.samplers)
	clear(t.layouts)
	clear(t.badIndexLengths)
	clear(t.unsuppliedBuffers)
}

// shaderLayout returns the backend's reflected layout for a shader, cached by id.
func (t *translator) shaderLayout(backend gfx.Backend, id gfx.ShaderID) gfx.ShaderLayout {
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
	switch types.ParameterKind(p) {
	case types.ParamColor:
		if off+16 <= len(buf) {
			types.WriteColor(buf[off:off+16], types.ParameterColor(p))
		}
	case types.ParamVec4:
		if off+16 <= len(buf) {
			types.WriteVec4(buf[off:off+16], types.ParameterVec(p))
		}
	case types.ParamMat4:
		if off+64 <= len(buf) {
			types.WriteMat4(buf[off:off+64], types.ParameterMat(p))
		}
	case types.ParamFloat:
		if off+4 <= len(buf) {
			binary.LittleEndian.PutUint32(buf[off:], math.Float32bits(types.ParameterNum(p)))
		}
	case types.ParamRaw:
		raw := types.ParameterRaw(p)
		if off+raw.Len() <= len(buf) {
			copy(buf[off:], raw.Data())
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
	backend gfx.Backend, shader gfx.ShaderID, label string, m *gfx.MeshDescr, state gfx.MaterialState, pass gfx.PassDescr,
) (gfx.PipelineID, error) {
	stride := types.MeshStride(m)
	layout, ok := types.VertexLayoutKeyOf(types.MeshLayout(m))
	if !ok {
		return 0, nil
	}
	// One colour format exists today, the frame buffer's, and every renderable
	// texture in the tree is allocated in it - so the sentinel is still right
	// for every pass that has a colour attachment at all. What is not
	// interchangeable is having one: a depth-only pass has no colour
	// attachment, and a pipeline that declares a target it will never be given
	// is rejected at setPipeline.
	const colorFormat, depthFormat = gfx.FormatScreen, gfx.FormatDepth32F
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
	if err := gfx.CheckVertexInterface(label, t.shaderLayout(backend, shader), types.MeshLayout(m)); err != nil {
		t.pipelines[k] = 0
		return 0, err
	}
	attrs := make([]gfx.VertexAttribute, len(types.MeshLayout(m)))
	for i := range types.MeshLayout(m) {
		attrs[i] = gfx.VertexAttribute{Offset: types.VertexAttrOffset(&(types.MeshLayout(m)[i])), Type: types.VertexAttrTyp(&(types.MeshLayout(m)[i])), Location: i}
	}
	id, err := backend.NewPipeline(gfx.PipelineDesc{
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

func (t *translator) ensureSampler(backend gfx.Backend, desc gfx.SamplerDesc) gfx.SamplerID {
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
