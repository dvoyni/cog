package gfx

import "github.com/dvoyni/cog/m"

// gpuOpKind tags the variant of a gpuOp.
type gpuOpKind uint8

const (
	gpuSetPipeline gpuOpKind = iota
	gpuSetParams
	gpuSetBakedTexture
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
// local values and appended once.
type GpuQueue struct {
	bakes      []gpuOp
	render     []gpuOp
	releases   []gpuOp
	clearColor m.Color
	clearDepth float32
	clearMask  clearMask
}

type clearMask uint8

const (
	clearColorBit clearMask = 1 << iota
	clearDepthBit
)

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
	SetTexture(TextureID, SamplerID, int, int, int)
	SetVertexBuffer(BufferID, int)
	SetIndexBuffer(BufferID, int)
	SetBuffer(int, int, BufferID, int, int)
	Draw(first, count, instances int, indexed bool)
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
	q.clearMask = 0
}

// Clear sets the frame's color-attachment clear value. The last call wins; no
// call leaves the color attachment in load mode.
func (q *GpuQueue) Clear(color m.Color) {
	q.clearColor = color
	q.clearMask |= clearColorBit
}

// ClearDepth sets the frame's depth-attachment clear value. The last call wins;
// no call leaves the depth attachment in load mode.
func (q *GpuQueue) ClearDepth(depth float32) {
	q.clearDepth = depth
	q.clearMask |= clearDepthBit
}

func (q *GpuQueue) SetPipeline(pipeline PipelineID) {
	o := gpuOp{kind: gpuSetPipeline, res0: ResourceID(pipeline)}
	q.render = append(q.render, o)
}

func (q *GpuQueue) SetParams(params []byte) {
	o := gpuOp{kind: gpuSetParams, params: params}
	q.render = append(q.render, o)
}

func (q *GpuQueue) SetTexture(texture TextureID, sampler SamplerID, group, textureBinding, samplerBinding int) {
	o := gpuOp{
		kind: gpuSetBakedTexture, res0: ResourceID(texture), res1: ResourceID(sampler),
		arg0: int32(group), arg1: int32(textureBinding), arg2: int32(samplerBinding),
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

func (q *GpuQueue) Draw(first, count, instances int, indexed bool) {
	if instances < 1 {
		instances = 1
	}
	o := gpuOp{kind: gpuDraw, arg0: int32(first), arg1: int32(count), arg3: int32(instances)}
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
	q.bakes = append(q.bakes, gpuOp{
		kind: gpuAllocateTexture, res0: ResourceID(id),
		arg0: int32(desc.Width), arg1: int32(desc.Height), arg2: int32(desc.Layers), arg3: int32(desc.Format),
	})
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

// ClearColor returns the frame's color-attachment clear value.
func (q *GpuQueue) ClearColor() m.Color {
	return q.clearColor
}

// ClearDepthValue returns the frame's depth-attachment clear value.
func (q *GpuQueue) ClearDepthValue() float32 {
	return q.clearDepth
}

// Clears reports which attachments have explicit clear values this frame.
func (q *GpuQueue) Clears() (color, depth bool) {
	return q.clearMask&clearColorBit != 0, q.clearMask&clearDepthBit != 0
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
			})
		case gpuUpdateTexture:
			sink.UpdateTexture(TextureID(o.res0), int(o.arg0), Region{
				X: int(o.arg1), Y: int(o.arg2), Width: int(o.arg3), Height: int(o.arg4),
			}, o.params)
		}
	}
}

// ReplayRenderPass sends render commands to sink in recording order.
func (q *GpuQueue) ReplayRenderPass(sink RenderPass) {
	for i := range q.render {
		o := &q.render[i]
		switch o.kind {
		case gpuSetPipeline:
			sink.SetPipeline(PipelineID(o.res0))
		case gpuSetParams:
			sink.SetParams(o.params)
		case gpuSetBakedTexture:
			sink.SetTexture(TextureID(o.res0), SamplerID(o.res1), int(o.arg0), int(o.arg1), int(o.arg2))
		case gpuSetVertexBuffer:
			sink.SetVertexBuffer(BufferID(o.res0), int(o.arg0))
		case gpuSetIndexBuffer:
			sink.SetIndexBuffer(BufferID(o.res0), int(o.arg0))
		case gpuSetBakedBuffer:
			sink.SetBuffer(int(o.arg2), int(o.arg3), BufferID(o.res0), int(o.arg0), int(o.arg1))
		case gpuDraw:
			sink.Draw(int(o.arg0), int(o.arg1), int(o.arg3), o.arg2 != 0)
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
