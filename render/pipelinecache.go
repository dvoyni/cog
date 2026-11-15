package render

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/dvoyni/cog/gfx"
)

// pipelineKey is what a render pipeline is shared by: the variant and the
// vertex and fragment entry points its material names, an empty fragment
// entry point meaning no fragment stage; the material's render state, in
// the form makeKeyState gives every state that draws the same; the numbers
// of its mesh's vertex layouts and of its instance buffers' in the vertex
// layout interner, zero for none, as a draw with no mesh has; the formats
// and sample count of the pass drawn into; and the topology, with the
// format of its indices when it is a strip drawn indexed.
type pipelineKey struct {
	shader                               Shader
	vertexEntryPoint, fragmentEntryPoint string
	state                                State
	vertexLayout, instanceLayout         uint32
	formats                              passFormat
	topology                             PrimitiveTopology
	stripIndexFormat                     IndexFormat
}

// passFormat is what a pass's targets fix of the pipelines drawn in it: the
// format of each colour target, the first colorCount of colorFormats, that
// of its depth-stencil target, none for none, and their sample count.
type passFormat struct {
	colorFormats       [maxColorTargetCount]TextureFormat
	colorCount         int
	depthStencilFormat TextureFormat
	sampleCount        uint32
}

// pipelineCache holds the render pipelines render made for its draws, one for
// each pipelineKey, made by the first draw of its key, in that draw's job,
// and kept, or its failure kept, until its shader is freed. Its mutex guards
// its map, taken inside the asset table's; a draw of a pipeline made already
// takes no lock of the pipeline's own, and only the draws of a pipeline being
// made wait for it.
type pipelineCache struct {
	mutex     sync.Mutex
	pipelines map[pipelineKey]*cachedPipeline
}

// find answers key's pipeline, made the first time from shader, the slot of
// key's shader, and table's vertex layouts; false when it failed then,
// which acquire reported through table, once, unless the device was lost.
// Draws of a key being made wait for it.
func (c *pipelineCache) find(table *assetTable, device gfx.Device, key pipelineKey, shader shaderSlot) (gfx.RenderPipeline, bool) {
	c.mutex.Lock()
	cached, ok := c.pipelines[key]
	if !ok {
		if c.pipelines == nil {
			c.pipelines = map[pipelineKey]*cachedPipeline{}
		}
		cached = &cachedPipeline{}
		c.pipelines[key] = cached
	}
	c.mutex.Unlock()
	if !cached.made.Load() {
		cached.make(table, device, key, shader)
	}
	return cached.pipeline, cached.pipeline != (gfx.RenderPipeline{})
}

// removeShader removes and releases every pipeline of shader, which is being
// freed.
func (c *pipelineCache) removeShader(device gfx.Device, shader Shader) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	for key, cached := range c.pipelines {
		if key.shader != shader {
			continue
		}
		cached.mutex.Lock()
		if cached.pipeline != (gfx.RenderPipeline{}) {
			device.ReleaseRenderPipeline(cached.pipeline)
		}
		cached.mutex.Unlock()
		delete(c.pipelines, key)
	}
}

// cachedPipeline is one pipeline the cache holds: the pipeline, none when it
// failed, and whether it was made, set once pipeline holds it. Its mutex
// guards the making, so one draw makes it and the others of its key wait.
type cachedPipeline struct {
	mutex    sync.Mutex
	made     atomic.Bool
	pipeline gfx.RenderPipeline
}

// make makes c's pipeline of key from shader, the slot of key's shader,
// unless another draw made it while this one waited, and reports a failure
// through table, unless the device was lost.
func (c *cachedPipeline) make(table *assetTable, device gfx.Device, key pipelineKey, shader shaderSlot) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.made.Load() {
		return
	}
	var err error
	if c.pipeline, err = makeRenderPipeline(table, device, key, shader); err != nil && !errors.Is(err, gfx.ErrDeviceLost) {
		table.report(ReportKindPipelineFailed, fmt.Errorf("render: the pipeline of shader %v, vertex entry point %q and fragment entry point %q: %w", key.shader, key.vertexEntryPoint, key.fragmentEntryPoint, err))
	}
	c.made.Store(true)
}

// makeRenderPipeline makes key's pipeline from shader, the slot of key's
// shader, with the layout of the shader's bindings and key's vertex layouts
// from table, its mesh's before its instance buffers'. A shader with no
// fragment stage drawn into colour targets is refused; gfx's refusal, such
// as ErrVertexMismatch, answers its error.
func makeRenderPipeline(table *assetTable, device gfx.Device, key pipelineKey, shader shaderSlot) (gfx.RenderPipeline, error) {
	descriptor := gfx.RenderPipelineDescriptor{
		Label:  "render: a material's pipeline",
		Layout: shader.layout.layout,
		Vertex: gfx.VertexState{
			Module:     shader.module,
			EntryPoint: key.vertexEntryPoint,
			Buffers:    slices.Concat(table.vertexLayouts.findGfxLayouts(key.vertexLayout), table.vertexLayouts.findGfxLayouts(key.instanceLayout)),
		},
		Primitive: gfx.PrimitiveState{
			Topology:         gfx.PrimitiveTopology(key.topology),
			StripIndexFormat: gfx.IndexFormat(key.stripIndexFormat),
			FrontFace:        gfx.FrontFace(key.state.FrontFace),
			CullMode:         gfx.CullMode(key.state.CullMode),
		},
		DepthStencil: key.state.makeDepthStencilState(key.formats.depthStencilFormat),
		Multisample:  gfx.MultisampleState{Count: key.formats.sampleCount, Mask: ^uint64(0), AlphaToCoverageEnabled: key.state.AlphaToCoverage},
	}
	switch {
	case key.fragmentEntryPoint != "":
		descriptor.Fragment = gfx.FragmentState{Module: shader.module, EntryPoint: key.fragmentEntryPoint, Targets: key.state.makeColorTargetStates(key.formats.colorFormats[:key.formats.colorCount])}
	case key.formats.colorCount > 0:
		return gfx.RenderPipeline{}, fmt.Errorf("the shader has no fragment stage, and the pass %d colour targets, which only a pass with none may have", key.formats.colorCount)
	}
	return device.CreateRenderPipeline(descriptor)
}
