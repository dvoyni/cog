package gfx

import "github.com/dvoyni/cog/m"

// gpuOpKind tags the variant of a gpuOp.
type gpuOpKind uint8

const (
	gpuSetPipeline gpuOpKind = iota
	gpuSetParams
	gpuSetBakedTexture
	gpuSetSampler
	gpuSetVertexBuffer
	gpuSetIndexBuffer
	gpuSetBakedBuffer
	gpuDraw
	gpuBakeBuffer
	gpuReleaseBuffer
	gpuBakeTexture
	gpuReleaseTexture
	gpuAllocateTexture
	gpuUpdateTexture
)

// GpuQueue owns a translated command sequence. Commands are constructed as
// local values and appended once. Bakes are hoisted ahead of every pass; render
// commands belong to the pass that was open when they were recorded.
type GpuQueue struct {
	bakes    []gpuOp
	render   []gpuOp
	releases []gpuOp
	passes   []gpuPass

	// transitions is one flat arena for the whole frame; each pass holds a
	// half-open range into it, the same way it holds one into render.
	// transitionsUsed marks how much of it earlier passes already claimed.
	transitions     []TextureTransition
	transitionsUsed int
}

// TextureUsage names the role a texture is in as far as the GPU's memory
// pipeline is concerned. It is deliberately just the two roles gfx can put a
// texture in, not a mirror of the backend's usage flags.
type TextureUsage uint8

const (
	// TextureUsageRenderAttachment is a texture being written as a pass's
	// colour or depth attachment.
	TextureUsageRenderAttachment TextureUsage = iota
	// TextureUsageTextureBinding is a texture being read by a shader.
	TextureUsageTextureBinding
)

// TextureTransition orders one texture's writes against its reads, or the other
// way round. The backend derives none of these itself: it tracks resources for
// lifetime and submit-time validation only, so a texture written as an
// attachment and then sampled is not ordered against those writes - not within
// one command encoder, and not across a submit boundary either. On Vulkan the
// sample then reads the image mid-write, which shows as a flicker that looks
// random, is not a CPU/GPU race, and is invisible to a capture taken while the
// app is redrawing.
//
// gfx is the layer that can see the hazard, because by translation time the
// frame's passes are sorted and merged and the write-then-read pairs are
// computable. From is the usage the texture is actually in, not a guess: a
// layout transition that names the wrong old layout is undefined behaviour.
type TextureTransition struct {
	Texture  TextureID
	From, To TextureUsage
}

// GpuPassDesc is one render pass for the backend to encode. Screen selects the
// frame buffer, which only the backend can resolve because it is sized from the
// surface; Target names any other colour attachment, and zero means none.
type GpuPassDesc struct {
	Screen bool
	// NoColor is set by a pass that declares no colour attachment at all, which
	// a zero Target cannot say on its own: a texture target whose view does not
	// exist yet resolves to zero too, and the two want opposite handling. A
	// backend must not read "depth-only pass" off a zero Target.
	NoColor    bool
	Target     TextureViewID
	Depth      TextureViewID
	DepthAuto  bool
	Load       LoadOp
	Clear      m.Color
	Store      StoreOp
	DepthLoad  LoadOp
	DepthClear float32
	DepthStore StoreOp
	Label      string
}

// gpuPass is a pass descriptor and the half-open range of render commands in it.
// present marks the frame's implicit present pass instead, which carries no
// descriptor and no commands: everything about it is the backend's.
type gpuPass struct {
	desc       GpuPassDesc
	present    bool
	start, end int
	// transStart, transEnd is the range of transitions that must be placed
	// before this pass is encoded. They sit outside the pass because a barrier
	// cannot be recorded inside a render pass.
	transStart, transEnd int
}

// GpuPassSink receives the frame's passes. BeginPass returns the RenderPass its
// commands go to, so the backend owns encoder and pass lifetime entirely.
type GpuPassSink interface {
	BeginPass(GpuPassDesc) RenderPass
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
}

// GpuBakeSink receives resource uploads before render-pass encoding.
type GpuBakeSink interface {
	BakeBuffer(BufferID, BufferKind, int, []byte)
	BakeTexture(TextureID, int, int, TextureFormat, []byte, bool)
	AllocateTexture(TextureID, TextureDesc)
	UpdateTexture(TextureID, int, Region, []byte)
}

// RenderPass receives render commands in recording order.
type RenderPass interface {
	SetPipeline(PipelineID)
	SetParams([]byte)
	SetTexture(TextureID, int, int)
	SetSampler(SamplerID, int, int)
	SetVertexBuffer(BufferID, int)
	SetIndexBuffer(BufferID, int)
	SetBuffer(int, int, BufferID, int, int)
	Draw(first, count, instances, firstInstance int, indexed bool)
}

// GpuReleaseSink receives resource releases after submission.
type GpuReleaseSink interface {
	ReleaseBuffer(BufferID)
	ReleaseTexture(TextureID)
}

// Reset drops all commands but keeps queue capacity for reuse.
func (q *GpuQueue) Reset() {
	clear(q.bakes)
	clear(q.render)
	clear(q.releases)
	q.bakes = q.bakes[:0]
	q.render = q.render[:0]
	q.releases = q.releases[:0]
	clear(q.passes)
	q.passes = q.passes[:0]
	clear(q.transitions)
	q.transitions = q.transitions[:0]
	q.transitionsUsed = 0
}

// TransitionTexture records a barrier to place before the next pass opens.
// Calls accumulate until BeginPass claims them, so the translator can announce
// a pass's hazards before it knows the pass descriptor is even worth emitting.
func (q *GpuQueue) TransitionTexture(transition TextureTransition) {
	q.transitions = append(q.transitions, transition)
}

// BeginPass opens a pass; every render command until EndPass belongs to it, and
// every transition recorded since the last pass is placed before it.
func (q *GpuQueue) BeginPass(desc GpuPassDesc) {
	q.passes = append(q.passes, gpuPass{
		desc: desc, start: len(q.render), end: len(q.render),
		transStart: q.transitionsUsed, transEnd: len(q.transitions),
	})
	q.transitionsUsed = len(q.transitions)
}

// Present appends the frame's implicit present pass, which runs after every
// declared pass because it reads what they wrote. Its own barrier is the
// backend's: the frame buffer is the one attachment gfx never names.
func (q *GpuQueue) Present() {
	q.passes = append(q.passes, gpuPass{present: true, start: len(q.render), end: len(q.render)})
}

// EndPass closes the pass BeginPass opened.
func (q *GpuQueue) EndPass() {
	if len(q.passes) > 0 {
		q.passes[len(q.passes)-1].end = len(q.render)
	}
}

func (q *GpuQueue) SetPipeline(pipeline PipelineID) {
	o := gpuOp{kind: gpuSetPipeline, res0: ResourceID(pipeline)}
	q.render = append(q.render, o)
}

func (q *GpuQueue) SetParams(params []byte) {
	o := gpuOp{kind: gpuSetParams, params: params}
	q.render = append(q.render, o)
}

func (q *GpuQueue) SetTexture(texture TextureID, group, binding int) {
	o := gpuOp{
		kind: gpuSetBakedTexture, res0: ResourceID(texture),
		arg0: int32(group), arg1: int32(binding),
	}
	q.render = append(q.render, o)
}

// SetSampler binds one sampler by its own group and binding. Samplers bind
// independently of textures because a material's textures may legitimately want
// different ones - a tiling ground beside a clamped decal.
func (q *GpuQueue) SetSampler(sampler SamplerID, group, binding int) {
	o := gpuOp{
		kind: gpuSetSampler, res0: ResourceID(sampler),
		arg0: int32(group), arg1: int32(binding),
	}
	q.render = append(q.render, o)
}

func (q *GpuQueue) SetVertexBuffer(buffer BufferID, offset int) {
	o := gpuOp{kind: gpuSetVertexBuffer, res0: ResourceID(buffer), arg0: int32(offset)}
	q.render = append(q.render, o)
}

func (q *GpuQueue) SetIndexBuffer(buffer BufferID, offset int) {
	o := gpuOp{kind: gpuSetIndexBuffer, res0: ResourceID(buffer), arg0: int32(offset)}
	q.render = append(q.render, o)
}

func (q *GpuQueue) SetBuffer(group, binding int, buffer BufferID, offset, size int) {
	o := gpuOp{
		kind: gpuSetBakedBuffer, res0: ResourceID(buffer),
		arg0: int32(offset), arg1: int32(size), arg2: int32(group), arg3: int32(binding),
	}
	q.render = append(q.render, o)
}

func (q *GpuQueue) Draw(first, count, instances, firstInstance int, indexed bool) {
	if instances < 1 {
		instances = 1
	}
	o := gpuOp{
		kind: gpuDraw, arg0: int32(first), arg1: int32(count),
		arg3: int32(instances), arg4: int32(firstInstance),
	}
	if indexed {
		o.arg2 = 1
	}
	q.render = append(q.render, o)
}

func (q *GpuQueue) BakeBuffer(id BufferID, kind BufferKind, size int, data []byte) {
	o := gpuOp{
		kind: gpuBakeBuffer, res0: ResourceID(id), arg0: int32(kind), arg1: int32(size), params: data,
	}
	q.bakes = append(q.bakes, o)
}

func (q *GpuQueue) ReleaseBuffer(id BufferID) {
	o := gpuOp{kind: gpuReleaseBuffer, res0: ResourceID(id)}
	q.releases = append(q.releases, o)
}

func (q *GpuQueue) BakeTexture(id TextureID, width, height int, format TextureFormat, pixels []byte, mipmaps bool) {
	o := gpuOp{
		kind: gpuBakeTexture, res0: ResourceID(id), arg0: int32(width), arg1: int32(height),
		arg2: int32(format), params: pixels,
	}
	if mipmaps {
		o.arg3 = 1
	}
	q.bakes = append(q.bakes, o)
}

func (q *GpuQueue) AllocateTexture(id TextureID, desc TextureDesc) {
	o := gpuOp{
		kind: gpuAllocateTexture, res0: ResourceID(id),
		arg0: int32(desc.Width), arg1: int32(desc.Height), arg2: int32(desc.Layers), arg3: int32(desc.Format),
	}
	if desc.Renderable {
		o.arg4 = 1
	}
	q.bakes = append(q.bakes, o)
}

func (q *GpuQueue) UpdateTexture(id TextureID, layer int, region Region, pixels []byte) {
	q.bakes = append(q.bakes, gpuOp{
		kind: gpuUpdateTexture, res0: ResourceID(id),
		arg0: int32(layer), arg1: int32(region.X), arg2: int32(region.Y),
		arg3: int32(region.Width), arg4: int32(region.Height), params: pixels,
	})
}

func (q *GpuQueue) ReleaseTexture(id TextureID) {
	o := gpuOp{kind: gpuReleaseTexture, res0: ResourceID(id)}
	q.releases = append(q.releases, o)
}

// ReplayBakes sends every resource upload to sink.
func (q *GpuQueue) ReplayBakes(sink GpuBakeSink) {
	for i := range q.bakes {
		o := &q.bakes[i]
		switch o.kind {
		case gpuBakeBuffer:
			sink.BakeBuffer(BufferID(o.res0), BufferKind(o.arg0), int(o.arg1), o.params)
		case gpuBakeTexture:
			sink.BakeTexture(TextureID(o.res0), int(o.arg0), int(o.arg1), TextureFormat(o.arg2), o.params, o.arg3 != 0)
		case gpuAllocateTexture:
			sink.AllocateTexture(TextureID(o.res0), TextureDesc{
				Width: int(o.arg0), Height: int(o.arg1), Layers: int(o.arg2), Format: TextureFormat(o.arg3),
				Renderable: o.arg4 != 0,
			})
		case gpuUpdateTexture:
			sink.UpdateTexture(TextureID(o.res0), int(o.arg0), Region{
				X: int(o.arg1), Y: int(o.arg2), Width: int(o.arg3), Height: int(o.arg4),
			}, o.params)
		}
	}
}

// ReplayPasses sends every pass to sink in order, each bracketed by BeginPass
// and EndPass, with its own render commands in recording order.
func (q *GpuQueue) ReplayPasses(sink GpuPassSink) {
	for i := range q.passes {
		pass := &q.passes[i]
		if pass.present {
			sink.Present()
			continue
		}
		if pass.transEnd > pass.transStart {
			sink.TransitionTextures(q.transitions[pass.transStart:pass.transEnd])
		}
		rp := sink.BeginPass(pass.desc)
		if rp != nil {
			q.replayRange(rp, pass.start, pass.end)
		}
		sink.EndPass(rp)
	}
}

// replayRange sends one pass's slice of render commands to its RenderPass.
func (q *GpuQueue) replayRange(sink RenderPass, start, end int) {
	for i := start; i < end && i < len(q.render); i++ {
		o := &q.render[i]
		switch o.kind {
		case gpuSetPipeline:
			sink.SetPipeline(PipelineID(o.res0))
		case gpuSetParams:
			sink.SetParams(o.params)
		case gpuSetBakedTexture:
			sink.SetTexture(TextureID(o.res0), int(o.arg0), int(o.arg1))
		case gpuSetSampler:
			sink.SetSampler(SamplerID(o.res0), int(o.arg0), int(o.arg1))
		case gpuSetVertexBuffer:
			sink.SetVertexBuffer(BufferID(o.res0), int(o.arg0))
		case gpuSetIndexBuffer:
			sink.SetIndexBuffer(BufferID(o.res0), int(o.arg0))
		case gpuSetBakedBuffer:
			sink.SetBuffer(int(o.arg2), int(o.arg3), BufferID(o.res0), int(o.arg0), int(o.arg1))
		case gpuDraw:
			sink.Draw(int(o.arg0), int(o.arg1), int(o.arg3), int(o.arg4), o.arg2 != 0)
		}
	}
}

// ReplayReleases sends every resource release to sink.
func (q *GpuQueue) ReplayReleases(sink GpuReleaseSink) {
	for i := range q.releases {
		o := &q.releases[i]
		switch o.kind {
		case gpuReleaseBuffer:
			sink.ReleaseBuffer(BufferID(o.res0))
		case gpuReleaseTexture:
			sink.ReleaseTexture(TextureID(o.res0))
		}
	}
}

// gpuOp is one entry in a translated, backend-agnostic op stream produced by the
// gfx plugin and consumed by Backend.Execute. Its storage is a compact per-kind
// union — two resource slots and four int32 args are reinterpreted per op kind by
// the setters/accessors below — so a frame's many ops stay small. Callers only
// touch it through those methods.
type gpuOp struct {
	kind   gpuOpKind
	res0   ResourceID // pipeline | vertex/index/uniform buffer | texture
	res1   ResourceID // sampler for gpuSetBakedTexture
	arg0   int32      // offset | first | clear R bits
	arg1   int32      // size | count | clear G bits
	arg2   int32      // group | indexed | clear B bits
	arg3   int32      // binding | clear A bits
	arg4   int32      // texture update height
	params []byte     // gpuSetParams payload
}
