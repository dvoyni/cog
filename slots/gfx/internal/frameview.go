package internal

import (
	"cmp"
	"slices"
	"strconv"

	"github.com/dvoyni/cog/slots/gfx/internal/shader"

	"github.com/dvoyni/cog/slots/gfx/internal/types"
)

// FrameViewOf renders one tick's queues, keeping only the passes labelled
// filter when it is set. The pass order it walks is the
// translator's own - Order first, declaration sequence breaking ties - so what
// an agent reads as run order is the order the GPU sees.
func FrameViewOf(queue *OpQueue, resources *ResourceQueue, filter string) FrameView {
	view := FrameView{Filter: filter, PassCount: len(queue.passes)}

	draws := make([]int, len(queue.passes))
	instances := make([]int, len(queue.passes))
	for pass := range queue.passes {
		record := &queue.passes[pass]
		draws[pass] = len(record.Draws)
		for i := range record.Draws {
			instances[pass] += record.Draws[i].Instances
		}
		view.DrawCount += draws[pass]
		view.InstanceCount += instances[pass]
	}
	// A stray draw is counted and dropped at record time, so it adds to the
	// frame's draws but carries no instances.
	view.StrayDraws = queue.strayDraws
	view.DrawCount += view.StrayDraws

	order := make([]int, len(queue.passes))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int {
		return cmp.Compare(queue.passes[a].Desc.Order, queue.passes[b].Desc.Order)
	})
	seen := map[drawParamsKey]int{}
	for run, index := range order {
		desc := queue.passes[index].Desc
		if filter != "" && desc.Label != filter {
			view.OmittedPasses++
			continue
		}
		view.Passes = append(view.Passes, passViewOf(index, run, desc, draws[index], instances[index]))
		view.DrawParams = appendDrawParamsViews(view.DrawParams, seen, queue, resources, index)
	}

	view.ResourceOps = appendResourceOpViews(view.ResourceOps, "durable", resources.ops)
	view.ResourceOps = appendResourceOpViews(view.ResourceOps, "frame", queue.resources)
	return view
}

// passViewOf renders one declared pass, at its declaration index and its
// position in run order.
func passViewOf(index, run int, desc types.PassDescr, draws, instances int) PassView {
	view := PassView{
		Index: index, Run: run, Label: desc.Label, Order: int(desc.Order),
		Target:     desc.Target.Kind.String(),
		Depth:      desc.Depth.Kind.String(),
		Load:       desc.Load.String(),
		Store:      desc.Store.String(),
		DepthLoad:  desc.DepthLoad.String(),
		DepthClear: desc.DepthClear,
		DepthStore: desc.DepthStore.String(),
		Draws:      draws, Instances: instances,
		Runs: desc.IsObservable(draws),
	}
	if desc.Target.Kind == types.TargetTexture {
		view.TargetTexture = desc.Target.Texture
		view.TargetWidth, view.TargetHeight = desc.Target.Width, desc.Target.Height
		view.TargetMip, view.TargetLayer = desc.Target.Mip, desc.Target.Layer
	}
	if desc.Depth.Kind == types.DepthKindTexture {
		view.DepthTexture = desc.Depth.Texture
	}
	if desc.Load == types.LoadClear {
		view.Clear = []float32{desc.Clear.R, desc.Clear.G, desc.Clear.B, desc.Clear.A}
	}
	return view
}

// appendResourceOpViews renders one queue's resource operations. The index
// carried is the position among that queue's resource ops, which is what an op
// is addressed by.
func appendResourceOpViews(dst []ResourceOpView, queue string, ops []ResourceOp) []ResourceOpView {
	for i := range ops {
		dst = append(dst, resourceOpViewOf(queue, i, &ops[i]))
	}
	return dst
}

// resourceOpViewOf renders one resource operation, carrying only the fields
// its kind gives meaning to. The op struct is one flat union shared by every
// kind, so emitting all of it would put eight irrelevant zeroes beside each
// answer.
func resourceOpViewOf(queue string, index int, o *ResourceOp) ResourceOpView {
	view := ResourceOpView{Queue: queue, Index: index, Kind: opKindName(o.Kind)}
	switch o.Kind {
	case OpBakeBuffer:
		view.Buffer, view.BufferKind = o.BufferID, o.BufferKind.String()
		view.Size, view.Bytes = o.BufferSize, len(o.Bytes)
	case OpReleaseBuffer:
		view.Buffer = o.BufferID
	case OpReleaseTexture:
		view.Texture = o.TextureID
	case OpAllocateTexture:
		view.Texture, view.Width, view.Height = o.TextureID, o.TexW, o.TexH
		view.Layers, view.Format, view.Renderable = o.TexLayers, o.Format.String(), o.Renderable
		view.Mipmaps = o.Mipmaps
	case OpUpdateTexture:
		region := o.Region
		view.Texture, view.Layer, view.Region, view.Bytes = o.TextureID, o.TexLayer, &region, len(o.Bytes)
	case OpReleaseCachedResource:
		view.Path = o.Path
	case OpUploadProgram:
		view.Shader, view.Label = o.ShaderID, o.Program.Label()
		view.Bytes = len(shader.ProgramDesc(o.Program).Code)
	case OpReleaseShader:
		view.Shader = o.ShaderID
	}
	return view
}

func opKindName(kind OpKind) string {
	switch kind {
	case OpBakeBuffer:
		return "bakeBuffer"
	case OpReleaseBuffer:
		return "releaseBuffer"
	case OpReleaseTexture:
		return "releaseTexture"
	case OpReleaseCachedResource:
		return "releaseCachedResource"
	case OpFreeCachedResources:
		return "freeCachedResources"
	case OpAllocateTexture:
		return "allocateTexture"
	case OpUpdateTexture:
		return "updateTexture"
	case OpUploadProgram:
		return "uploadProgram"
	case OpReleaseShader:
		return "releaseShader"
	}
	return "unknown(" + strconv.Itoa(int(kind)) + ")"
}
