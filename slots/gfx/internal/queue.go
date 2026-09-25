package internal

// UniformBlockSize is one draw's uniform block, and so the stride of the
// frame's uniform arena. It is both the cap on a block a shader may declare
// and the largest minUniformBufferOffsetAlignment WebGPU permits, so slot x
// UniformBlockSize is a valid uniform binding offset on every device.
const UniformBlockSize = 256

// UniformBlock is one draw's packed uniform block: the shader's numeric
// parameters at their reflected offsets, zero past the block's declared size.
type UniformBlock [UniformBlockSize]byte

// renderKind tags the variant of a renderOp.
type renderKind uint8

const (
	renderSetPipeline renderKind = iota
	renderSetUniformBlock
	renderSetTexture
	renderSetSampler
	renderSetVertexBuffer
	renderSetIndexBuffer
	renderSetBuffer
	renderDraw
)

// renderOp is one command inside a pass. Its storage is a compact per-kind
// union - one resource slot and five int32 args are reinterpreted per kind by
// the appenders and replayRange - so a frame's many render commands stay small.
type renderOp struct {
	kind renderKind
	res0 ResourceID // pipeline | vertex/index/uniform buffer | texture | sampler
	arg0 int32      // uniform slot | offset | first | group
	arg1 int32      // size | count | binding
	arg2 int32      // group | indexed
	arg3 int32      // binding | instances
	arg4 int32      // first instance
}

// bufferBake is one buffer upload. Buffers have a single bake op, so it needs
// no kind.
type bufferBake struct {
	id   BufferID
	kind BufferKind
	size int
	data []byte
}

// textureBakeKind tags the variant of a textureBake.
type textureBakeKind uint8

const (
	textureBakePixels textureBakeKind = iota
	textureBakeAllocate
	textureBakeUpdate
)

// textureBake is one texture upload, allocation or region update. The three
// share one list because they must replay in recording order: an update of a
// texture allocated this frame has to follow its allocation.
type textureBake struct {
	kind       textureBakeKind
	id         TextureID
	width      int
	height     int
	layers     int
	format     TextureFormat
	mipmaps    bool
	renderable bool
	layer      int
	region     Region
	data       []byte
}

// pass is a pass descriptor and the half-open range of render commands in it.
// present marks the frame's implicit present pass instead, which carries no
// descriptor and no commands: everything about it is the backend's. capture
// marks the frame's readback the same way, and carries the one target it reads.
type pass struct {
	desc       PassDesc
	present    bool
	capture    bool
	captured   CaptureDesc
	start, end int
	// transStart, transEnd is the range of transitions that must be placed
	// before this pass is encoded. They sit outside the pass because a barrier
	// cannot be recorded inside a render pass.
	transStart, transEnd int
}

// PassSink receives the frame's passes. BeginPass returns the RenderPass its
// commands go to, so the backend owns encoder and pass lifetime entirely.
type PassSink interface {
	BeginPass(PassDesc) RenderPass
	EndPass(RenderPass)
	// TransitionTextures places the barriers a pass needs before it is encoded,
	// and is called outside any render pass because that is the only place a
	// barrier can be recorded. It is never called with an empty slice: a frame
	// with no render-then-sample pair pays nothing.
	TransitionTextures([]TextureTransition)
	// Present puts the frame buffer on the swapchain. It takes no arguments
	// because every piece of it - the buffer, the full-screen triangle, the
	// transfer function and the swapchain's own format - belongs to the
	// backend; gfx only decides that the frame has something to show.
	Present()
	// Capture copies one colour target into CPU-visible memory. Like Present it
	// is a whole-frame action rather than a pass, so it carries no commands;
	// unlike Present its result arrives later, through Backend.TakeCapture.
	//
	// It is the last thing in the frame, after the present: the present pass
	// transitions the frame buffer out of RenderAttachment and then samples it,
	// so a copy encoded before it would name an old layout that is no longer
	// true.
	Capture(CaptureDesc)
}

// BakeSink receives resource uploads before render-pass encoding.
type BakeSink interface {
	// BakeUniforms receives the frame's uniform arena, every block a draw
	// packed, in slot order. It comes first and comes every frame, empty when
	// no draw carries a uniform block, so a backend can size its uniform buffer
	// before any bind group names it. The blocks are the queue's and are valid
	// until it is reset.
	BakeUniforms([]UniformBlock)
	BakeBuffer(BufferID, BufferKind, int, []byte)
	BakeTexture(TextureID, int, int, TextureFormat, []byte, bool)
	AllocateTexture(TextureID, TextureDesc)
	UpdateTexture(TextureID, int, Region, []byte)
}

// RenderPass receives render commands in recording order.
type RenderPass interface {
	SetPipeline(PipelineID)
	// SetUniformBlock binds the draw's uniform block: the slot of the arena
	// BakeUniforms handed over, at offset slot x UniformBlockSize.
	SetUniformBlock(slot int)
	SetTexture(TextureID, int, int)
	SetSampler(SamplerID, int, int)
	SetVertexBuffer(BufferID, int)
	// SetIndexBuffer binds the index buffer at the width one of its elements
	// was written at. The width is the mesh's rather than the pass's: two
	// meshes in one pass may well be indexed differently, because the width
	// follows from how many vertices each of them has.
	SetIndexBuffer(BufferID, int, IndexWidth)
	SetBuffer(int, int, BufferID, int, int)
	Draw(first, count, instances, firstInstance int, indexed bool)
}

// ReleaseSink receives resource releases after submission.
type ReleaseSink interface {
	ReleaseBuffer(BufferID)
	ReleaseTexture(TextureID)
}

// Queue owns a translated command sequence. Commands are constructed as local
// values and appended once. Bakes are hoisted ahead of every pass; render
// commands belong to the pass that was open when they were recorded.
//
// gfx appends to it and a Backend reads it back only by replaying it into its
// sinks: ReplayBakes, ReplayPasses and ReplayReleases are the whole read side.
type Queue struct {
	// Bakes are split by resource because order matters only within one
	// resource, and a buffer and a texture never share one.
	bufferBakes      []bufferBake
	textureBakes     []textureBake
	render           []renderOp
	releasedBuffers  []BufferID
	releasedTextures []TextureID
	passes           []pass

	// transitions is one flat arena for the whole frame; each pass holds a
	// half-open range into it, the same way it holds one into render.
	// transitionsUsed marks how much of it earlier passes already claimed.
	transitions     []TextureTransition
	transitionsUsed int

	// uniforms is the frame's uniform arena. A draw's block is packed straight
	// into its slot, and the whole of it goes to the backend in one piece.
	uniforms []UniformBlock
}

// Reset drops all commands but keeps queue capacity for reuse.
func (q *Queue) Reset() {
	clear(q.bufferBakes)
	clear(q.textureBakes)
	q.bufferBakes = q.bufferBakes[:0]
	q.textureBakes = q.textureBakes[:0]
	q.render = q.render[:0]
	q.releasedBuffers = q.releasedBuffers[:0]
	q.releasedTextures = q.releasedTextures[:0]
	clear(q.passes)
	q.passes = q.passes[:0]
	clear(q.transitions)
	q.transitions = q.transitions[:0]
	q.transitionsUsed = 0
	q.uniforms = q.uniforms[:0]
}

// TransitionTexture records a barrier to place before the next pass opens.
// Calls accumulate until BeginPass claims them, so the translator can announce
// a pass's hazards before it knows the pass descriptor is even worth emitting.
func (q *Queue) TransitionTexture(transition TextureTransition) {
	q.transitions = append(q.transitions, transition)
}

// BeginPass opens a pass; every render command until EndPass belongs to it, and
// every transition recorded since the last pass is placed before it.
func (q *Queue) BeginPass(desc PassDesc) {
	q.passes = append(q.passes, pass{
		desc: desc, start: len(q.render), end: len(q.render),
		transStart: q.transitionsUsed, transEnd: len(q.transitions),
	})
	q.transitionsUsed = len(q.transitions)
}

// Present appends the frame's implicit present pass, which runs after every
// declared pass because it reads what they wrote. Its own barrier is the
// backend's: the frame buffer is the one attachment gfx never names.
func (q *Queue) Present() {
	q.passes = append(q.passes, pass{present: true, start: len(q.render), end: len(q.render)})
}

// Capture appends the frame's readback, which runs after everything else
// including the present. It claims the transitions recorded since the last
// pass the same way BeginPass does, because a texture capture has to be moved
// into TextureUsageCopySrc first; a screen capture declares none, since the
// frame buffer is the one attachment gfx never names.
func (q *Queue) Capture(desc CaptureDesc) {
	q.passes = append(q.passes, pass{
		capture: true, captured: desc, start: len(q.render), end: len(q.render),
		transStart: q.transitionsUsed, transEnd: len(q.transitions),
	})
	q.transitionsUsed = len(q.transitions)
}

// EndPass closes the pass BeginPass opened.
func (q *Queue) EndPass() {
	if len(q.passes) > 0 {
		q.passes[len(q.passes)-1].end = len(q.render)
	}
}

func (q *Queue) SetPipeline(pipeline PipelineID) {
	o := renderOp{kind: renderSetPipeline, res0: ResourceID(pipeline)}
	q.render = append(q.render, o)
}

// SetUniformBlock claims the frame's next uniform slot, binds it for the
// draw that follows, and returns the zeroed block for the caller to pack. The
// pointer is valid until the next claim, which may move the arena.
//
// A block claimed outside every pass belongs to a stray draw that no replay
// reaches. It is still handed to BakeUniforms, which costs a backend one unused
// slot and nothing else.
func (q *Queue) SetUniformBlock() *UniformBlock {
	slot := len(q.uniforms)
	q.uniforms = append(q.uniforms, UniformBlock{})
	q.render = append(q.render, renderOp{kind: renderSetUniformBlock, arg0: int32(slot)})
	return &q.uniforms[slot]
}

func (q *Queue) SetTexture(texture TextureID, group, binding int) {
	o := renderOp{
		kind: renderSetTexture, res0: ResourceID(texture),
		arg0: int32(group), arg1: int32(binding),
	}
	q.render = append(q.render, o)
}

// SetSampler binds one sampler by its own group and binding. Samplers bind
// independently of textures because a material's textures may legitimately want
// different ones - a tiling ground beside a clamped decal.
func (q *Queue) SetSampler(sampler SamplerID, group, binding int) {
	o := renderOp{
		kind: renderSetSampler, res0: ResourceID(sampler),
		arg0: int32(group), arg1: int32(binding),
	}
	q.render = append(q.render, o)
}

func (q *Queue) SetVertexBuffer(buffer BufferID, offset int) {
	o := renderOp{kind: renderSetVertexBuffer, res0: ResourceID(buffer), arg0: int32(offset)}
	q.render = append(q.render, o)
}

func (q *Queue) SetIndexBuffer(buffer BufferID, offset int, width IndexWidth) {
	o := renderOp{
		kind: renderSetIndexBuffer, res0: ResourceID(buffer),
		arg0: int32(offset), arg1: int32(width),
	}
	q.render = append(q.render, o)
}

func (q *Queue) SetBuffer(group, binding int, buffer BufferID, offset, size int) {
	o := renderOp{
		kind: renderSetBuffer, res0: ResourceID(buffer),
		arg0: int32(offset), arg1: int32(size), arg2: int32(group), arg3: int32(binding),
	}
	q.render = append(q.render, o)
}

func (q *Queue) Draw(first, count, instances, firstInstance int, indexed bool) {
	if instances < 1 {
		instances = 1
	}
	o := renderOp{
		kind: renderDraw, arg0: int32(first), arg1: int32(count),
		arg3: int32(instances), arg4: int32(firstInstance),
	}
	if indexed {
		o.arg2 = 1
	}
	q.render = append(q.render, o)
}

func (q *Queue) BakeBuffer(id BufferID, kind BufferKind, size int, data []byte) {
	q.bufferBakes = append(q.bufferBakes, bufferBake{id: id, kind: kind, size: size, data: data})
}

func (q *Queue) ReleaseBuffer(id BufferID) {
	q.releasedBuffers = append(q.releasedBuffers, id)
}

func (q *Queue) BakeTexture(id TextureID, width, height int, format TextureFormat, pixels []byte, mipmaps bool) {
	q.textureBakes = append(q.textureBakes, textureBake{
		kind: textureBakePixels, id: id, width: width, height: height, format: format,
		mipmaps: mipmaps, data: pixels,
	})
}

func (q *Queue) AllocateTexture(id TextureID, desc TextureDesc) {
	q.textureBakes = append(q.textureBakes, textureBake{
		kind: textureBakeAllocate, id: id, width: desc.Width, height: desc.Height, layers: desc.Layers,
		format: desc.Format, renderable: desc.Renderable,
	})
}

func (q *Queue) UpdateTexture(id TextureID, layer int, region Region, pixels []byte) {
	q.textureBakes = append(q.textureBakes, textureBake{
		kind: textureBakeUpdate, id: id, layer: layer, region: region, data: pixels,
	})
}

func (q *Queue) ReleaseTexture(id TextureID) {
	q.releasedTextures = append(q.releasedTextures, id)
}

// ReplayBakes sends every resource upload to sink: the uniform arena, then
// every buffer, then every texture, each in recording order.
func (q *Queue) ReplayBakes(sink BakeSink) {
	sink.BakeUniforms(q.uniforms)
	for i := range q.bufferBakes {
		b := &q.bufferBakes[i]
		sink.BakeBuffer(b.id, b.kind, b.size, b.data)
	}
	for i := range q.textureBakes {
		t := &q.textureBakes[i]
		switch t.kind {
		case textureBakePixels:
			sink.BakeTexture(t.id, t.width, t.height, t.format, t.data, t.mipmaps)
		case textureBakeAllocate:
			sink.AllocateTexture(t.id, TextureDesc{
				Width: t.width, Height: t.height, Layers: t.layers, Format: t.format, Renderable: t.renderable,
			})
		case textureBakeUpdate:
			sink.UpdateTexture(t.id, t.layer, t.region, t.data)
		}
	}
}

// ReplayPasses sends every pass to sink in order, each bracketed by BeginPass
// and EndPass, with its own render commands in recording order.
func (q *Queue) ReplayPasses(sink PassSink) {
	for i := range q.passes {
		p := &q.passes[i]
		if p.present {
			sink.Present()
			continue
		}
		if p.transEnd > p.transStart {
			sink.TransitionTextures(q.transitions[p.transStart:p.transEnd])
		}
		if p.capture {
			sink.Capture(p.captured)
			continue
		}
		rp := sink.BeginPass(p.desc)
		if rp != nil {
			q.replayRange(rp, p.start, p.end)
		}
		sink.EndPass(rp)
	}
}

// replayRange sends one pass's slice of render commands to its RenderPass.
func (q *Queue) replayRange(sink RenderPass, start, end int) {
	for i := start; i < end && i < len(q.render); i++ {
		o := &q.render[i]
		switch o.kind {
		case renderSetPipeline:
			sink.SetPipeline(PipelineID(o.res0))
		case renderSetUniformBlock:
			sink.SetUniformBlock(int(o.arg0))
		case renderSetTexture:
			sink.SetTexture(TextureID(o.res0), int(o.arg0), int(o.arg1))
		case renderSetSampler:
			sink.SetSampler(SamplerID(o.res0), int(o.arg0), int(o.arg1))
		case renderSetVertexBuffer:
			sink.SetVertexBuffer(BufferID(o.res0), int(o.arg0))
		case renderSetIndexBuffer:
			sink.SetIndexBuffer(BufferID(o.res0), int(o.arg0), IndexWidth(o.arg1))
		case renderSetBuffer:
			sink.SetBuffer(int(o.arg2), int(o.arg3), BufferID(o.res0), int(o.arg0), int(o.arg1))
		case renderDraw:
			sink.Draw(int(o.arg0), int(o.arg1), int(o.arg3), int(o.arg4), o.arg2 != 0)
		}
	}
}

// ReplayReleases sends every resource release to sink.
func (q *Queue) ReplayReleases(sink ReleaseSink) {
	for _, id := range q.releasedBuffers {
		sink.ReleaseBuffer(id)
	}
	for _, id := range q.releasedTextures {
		sink.ReleaseTexture(id)
	}
}
