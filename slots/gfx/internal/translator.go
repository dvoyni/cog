package internal

import (
	"cmp"
	"io/fs"
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

// indexLengthKey is one malformed index buffer's shape: the byte length that
// did not divide, and the width it was declared at.
type indexLengthKey struct {
	length int
	width  gfx.IndexWidth
}

// unsuppliedBufferKey is one storage binding a shader never got filled. The
// parameter name is in it because one shader may declare several.
type unsuppliedBufferKey struct {
	shader    gfx.ShaderID
	parameter string
}

// textureViewKey is one texture binding already reported as filled by a
// texture of the wrong view dimension. It is shaped like unsuppliedBufferKey
// and for the same reason: one shader may declare several texture bindings, and
// each is its own fault.
type textureViewKey struct {
	shader    gfx.ShaderID
	parameter string
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
	shaders   *assets.Cache[types.ShaderDescrParams, shaderUserData, *shader]
	pipelines map[pipelineKey]gfx.PipelineID
	samplers  map[gfx.SamplerDesc]gfx.SamplerID
	uarena    []byte
	layouts   map[gfx.ShaderID]gfx.ShaderLayout
	// textures is the path-texture cache. It is a translator field like every
	// other cache here, reached only on the render thread, so what protects it
	// is the confinement rather than a lock of its own.
	textures       *assets.Cache[types.TextureDescrParams, textureUserData, texture]
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
	// textureViewMismatches is the set of texture bindings already reported as
	// filled at the wrong view dimension, on unsuppliedBuffers' terms: the
	// mistake lives in the material until someone edits it, and the frame
	// carries only its first error.
	textureViewMismatches map[textureViewKey]struct{}
}

func newTranslator() *translator {
	return &translator{
		shaders:           assets.New[types.ShaderDescrParams, shaderUserData, *shader](shaderLoader{}),
		pipelines:         map[pipelineKey]gfx.PipelineID{},
		samplers:          map[gfx.SamplerDesc]gfx.SamplerID{},
		layouts:           map[gfx.ShaderID]gfx.ShaderLayout{},
		textures:          assets.New[types.TextureDescrParams, textureUserData, texture](textureLoader{}),
		parameterPlans:    map[parameterPlanBucketKey][]cachedParameterPlan{},
		textureUsage:      map[gfx.TextureID]gfx.TextureUsage{},
		badIndexLengths:   map[indexLengthKey]struct{}{},
		unsuppliedBuffers: map[unsuppliedBufferKey]struct{}{},

		textureViewMismatches: map[textureViewKey]struct{}{},
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
