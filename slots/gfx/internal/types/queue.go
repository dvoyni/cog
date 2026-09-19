package types

// opKind tags the variant of an op.
type opKind uint8

const (
	opSetPipeline opKind = iota
	opSetParams
	opSetTexture
	opSetSampler
	opSetVertexBuffer
	opSetIndexBuffer
	opSetBuffer
	opDraw
	opBakeBuffer
	opReleaseBuffer
	opBakeTexture
	opReleaseTexture
	opAllocateTexture
	opUpdateTexture
)

// op is one entry in a translated, backend-agnostic op stream produced by gfx
// and consumed by Backend.Execute. Its storage is a compact per-kind union - one
// resource slot and five int32 args are reinterpreted per op kind by the
// appenders and the replay above - so a frame's many ops stay small.
type op struct {
	kind   opKind
	res0   ResourceID // pipeline | vertex/index/uniform buffer | texture | sampler
	arg0   int32      // offset | first | group | width | layer
	arg1   int32      // size | count | binding | height | region x
	arg2   int32      // group | indexed | layers | format | region y
	arg3   int32      // binding | instances | format | mipmaps | region width
	arg4   int32      // first instance | renderable | region height
	params []byte     // params payload, buffer bytes or texture pixels
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
	bakes    []op
	render   []op
	releases []op
	passes   []pass

	// transitions is one flat arena for the whole frame; each pass holds a
	// half-open range into it, the same way it holds one into render.
	// transitionsUsed marks how much of it earlier passes already claimed.
	transitions     []TextureTransition
	transitionsUsed int
}

// Reset drops all commands but keeps queue capacity for reuse.
func (q *Queue) Reset() {
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
	o := op{kind: opSetPipeline, res0: ResourceID(pipeline)}
	q.render = append(q.render, o)
}

func (q *Queue) SetParams(params []byte) {
	o := op{kind: opSetParams, params: params}
	q.render = append(q.render, o)
}

func (q *Queue) SetTexture(texture TextureID, group, binding int) {
	o := op{
		kind: opSetTexture, res0: ResourceID(texture),
		arg0: int32(group), arg1: int32(binding),
	}
	q.render = append(q.render, o)
}

// SetSampler binds one sampler by its own group and binding. Samplers bind
// independently of textures because a material's textures may legitimately want
// different ones - a tiling ground beside a clamped decal.
func (q *Queue) SetSampler(sampler SamplerID, group, binding int) {
	o := op{
		kind: opSetSampler, res0: ResourceID(sampler),
		arg0: int32(group), arg1: int32(binding),
	}
	q.render = append(q.render, o)
}

func (q *Queue) SetVertexBuffer(buffer BufferID, offset int) {
	o := op{kind: opSetVertexBuffer, res0: ResourceID(buffer), arg0: int32(offset)}
	q.render = append(q.render, o)
}

func (q *Queue) SetIndexBuffer(buffer BufferID, offset int, width IndexWidth) {
	o := op{
		kind: opSetIndexBuffer, res0: ResourceID(buffer),
		arg0: int32(offset), arg1: int32(width),
	}
	q.render = append(q.render, o)
}

func (q *Queue) SetBuffer(group, binding int, buffer BufferID, offset, size int) {
	o := op{
		kind: opSetBuffer, res0: ResourceID(buffer),
		arg0: int32(offset), arg1: int32(size), arg2: int32(group), arg3: int32(binding),
	}
	q.render = append(q.render, o)
}

func (q *Queue) Draw(first, count, instances, firstInstance int, indexed bool) {
	if instances < 1 {
		instances = 1
	}
	o := op{
		kind: opDraw, arg0: int32(first), arg1: int32(count),
		arg3: int32(instances), arg4: int32(firstInstance),
	}
	if indexed {
		o.arg2 = 1
	}
	q.render = append(q.render, o)
}

func (q *Queue) BakeBuffer(id BufferID, kind BufferKind, size int, data []byte) {
	o := op{
		kind: opBakeBuffer, res0: ResourceID(id), arg0: int32(kind), arg1: int32(size), params: data,
	}
	q.bakes = append(q.bakes, o)
}

func (q *Queue) ReleaseBuffer(id BufferID) {
	o := op{kind: opReleaseBuffer, res0: ResourceID(id)}
	q.releases = append(q.releases, o)
}

func (q *Queue) BakeTexture(id TextureID, width, height int, format TextureFormat, pixels []byte, mipmaps bool) {
	o := op{
		kind: opBakeTexture, res0: ResourceID(id), arg0: int32(width), arg1: int32(height),
		arg2: int32(format), params: pixels,
	}
	if mipmaps {
		o.arg3 = 1
	}
	q.bakes = append(q.bakes, o)
}

func (q *Queue) AllocateTexture(id TextureID, desc TextureDesc) {
	o := op{
		kind: opAllocateTexture, res0: ResourceID(id),
		arg0: int32(desc.Width), arg1: int32(desc.Height), arg2: int32(desc.Layers), arg3: int32(desc.Format),
	}
	if desc.Renderable {
		o.arg4 = 1
	}
	q.bakes = append(q.bakes, o)
}

func (q *Queue) UpdateTexture(id TextureID, layer int, region Region, pixels []byte) {
	q.bakes = append(q.bakes, op{
		kind: opUpdateTexture, res0: ResourceID(id),
		arg0: int32(layer), arg1: int32(region.X), arg2: int32(region.Y),
		arg3: int32(region.Width), arg4: int32(region.Height), params: pixels,
	})
}

func (q *Queue) ReleaseTexture(id TextureID) {
	o := op{kind: opReleaseTexture, res0: ResourceID(id)}
	q.releases = append(q.releases, o)
}

// ReplayBakes sends every resource upload to sink.
func (q *Queue) ReplayBakes(sink BakeSink) {
	for i := range q.bakes {
		o := &q.bakes[i]
		switch o.kind {
		case opBakeBuffer:
			sink.BakeBuffer(BufferID(o.res0), BufferKind(o.arg0), int(o.arg1), o.params)
		case opBakeTexture:
			sink.BakeTexture(TextureID(o.res0), int(o.arg0), int(o.arg1), TextureFormat(o.arg2), o.params, o.arg3 != 0)
		case opAllocateTexture:
			sink.AllocateTexture(TextureID(o.res0), TextureDesc{
				Width: int(o.arg0), Height: int(o.arg1), Layers: int(o.arg2), Format: TextureFormat(o.arg3),
				Renderable: o.arg4 != 0,
			})
		case opUpdateTexture:
			sink.UpdateTexture(TextureID(o.res0), int(o.arg0), Region{
				X: int(o.arg1), Y: int(o.arg2), Width: int(o.arg3), Height: int(o.arg4),
			}, o.params)
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
		case opSetPipeline:
			sink.SetPipeline(PipelineID(o.res0))
		case opSetParams:
			sink.SetParams(o.params)
		case opSetTexture:
			sink.SetTexture(TextureID(o.res0), int(o.arg0), int(o.arg1))
		case opSetSampler:
			sink.SetSampler(SamplerID(o.res0), int(o.arg0), int(o.arg1))
		case opSetVertexBuffer:
			sink.SetVertexBuffer(BufferID(o.res0), int(o.arg0))
		case opSetIndexBuffer:
			sink.SetIndexBuffer(BufferID(o.res0), int(o.arg0), IndexWidth(o.arg1))
		case opSetBuffer:
			sink.SetBuffer(int(o.arg2), int(o.arg3), BufferID(o.res0), int(o.arg0), int(o.arg1))
		case opDraw:
			sink.Draw(int(o.arg0), int(o.arg1), int(o.arg3), int(o.arg4), o.arg2 != 0)
		}
	}
}

// ReplayReleases sends every resource release to sink.
func (q *Queue) ReplayReleases(sink ReleaseSink) {
	for i := range q.releases {
		o := &q.releases[i]
		switch o.kind {
		case opReleaseBuffer:
			sink.ReleaseBuffer(BufferID(o.res0))
		case opReleaseTexture:
			sink.ReleaseTexture(TextureID(o.res0))
		}
	}
}
