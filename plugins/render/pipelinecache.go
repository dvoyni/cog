package render

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/dvoyni/cog/plugins/gfx"
)

// cachedPipelineKey is what a pipeline cache shares pipelines of type P
// by: the shader it is freed with, how it makes its pipeline, and the name
// of that pipeline in reports.
type cachedPipelineKey[P comparable] interface {
	comparable
	fmt.Stringer
	// findShader answers the shader the key's pipeline is made from.
	findShader() Shader
	// makePipeline makes the key's pipeline from shader, the slot of its
	// shader, and table's vertex layouts; an error saying why it cannot be
	// made.
	makePipeline(table *assetTable, device gfx.Device, shader shaderSlot) (P, error)
	// makeInspectedPipeline answers pipeline, the key's, as an inspection
	// writes it down.
	makeInspectedPipeline(pipeline P) InspectedPipeline
}

// pipelineCache holds the pipelines of type P render made, one for each key
// of type K, made by the first draw or dispatch of its key, in that job, and
// kept, or its failure kept, until its shader is freed. Its mutex guards
// its map, taken inside the asset table's; a draw of a pipeline made
// already takes no lock of the pipeline's own, and only the draws of a
// pipeline being made wait for it.
type pipelineCache[K cachedPipelineKey[P], P comparable] struct {
	mutex     sync.Mutex
	pipelines map[K]*cachedPipeline[P]
}

// find answers key's pipeline, made the first time from shader, the slot of
// key's shader; false when it failed then, which make reported through
// table, once, unless the device was lost. Draws of a key being made wait
// for it.
func (c *pipelineCache[K, P]) find(table *assetTable, device gfx.Device, key K, shader shaderSlot) (P, bool) {
	c.mutex.Lock()
	cached, ok := c.pipelines[key]
	if !ok {
		if c.pipelines == nil {
			c.pipelines = map[K]*cachedPipeline[P]{}
		}
		cached = &cachedPipeline[P]{}
		c.pipelines[key] = cached
	}
	c.mutex.Unlock()
	if !cached.made.Load() {
		cached.make(table, device, key, shader)
	}
	var none P
	return cached.pipeline, cached.pipeline != none
}

// removeShader removes every pipeline of shader, which is being freed,
// handing each made to release.
func (c *pipelineCache[K, P]) removeShader(shader Shader, release func(P)) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	var none P
	for key, cached := range c.pipelines {
		if key.findShader() != shader {
			continue
		}
		cached.mutex.Lock()
		if cached.pipeline != none {
			release(cached.pipeline)
		}
		cached.mutex.Unlock()
		delete(c.pipelines, key)
	}
}

// cachedPipeline is one pipeline a cache holds: the pipeline, none when it
// failed, and whether it was made, set once pipeline holds it. Its mutex
// guards the making, so one draw makes it and the others of its key wait.
type cachedPipeline[P comparable] struct {
	mutex    sync.Mutex
	made     atomic.Bool
	pipeline P
}

// make makes c's pipeline of key from shader, the slot of key's shader,
// unless another draw made it while this one waited, and reports a failure
// through table, unless the device was lost; one made goes into the
// inspection.
func (c *cachedPipeline[P]) make[K cachedPipelineKey[P]](table *assetTable, device gfx.Device, key K, shader shaderSlot) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.made.Load() {
		return
	}
	var err error
	c.pipeline, err = key.makePipeline(table, device, shader)
	switch {
	case err == nil:
		table.inspector.add(func(inspection *Inspection) {
			inspection.MadePipelines = append(inspection.MadePipelines, key.makeInspectedPipeline(c.pipeline))
		})
	case !errors.Is(err, gfx.ErrDeviceLost):
		table.report(ReportKindPipelineFailed, fmt.Errorf("render: %v: %w", key, err))
	}
	c.made.Store(true)
}
