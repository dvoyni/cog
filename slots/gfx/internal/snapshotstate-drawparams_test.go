package internal

import (
	"testing"
	"time"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx/internal/descriptors"
	"github.com/dvoyni/cog/slots/gfx/internal/shader"
	"github.com/dvoyni/cog/slots/gfx/internal/types"
)

// A draw through a set is no longer an opaque handle: the snapshot resolves the
// set it names, and the version it captured, into what the draw draws with.
// The properties worth pinning are the ones that would make that answer a lie -
// a frame's value reported as the set's, two versions folded into one, a
// default reported as a value, or a set drawn only in a dropped pass reported
// anyway.

// setRig is a capture rig whose backend reflects setLayout, with an uploaded
// shader and a durable buffer for the storage binding.
type setRig struct {
	*captureRig
	shader  types.ShaderID
	records descriptors.BufferDescr
}

func newSetRig(t *testing.T) *setRig {
	t.Helper()
	rig := &setRig{captureRig: newCaptureRig(t)}
	layout := setLayout()
	rig.backend.reflection = &layout
	program := compileShader(rig.k, nil, shader.ShaderWithText(programSource)).Program
	withShaders(rig.k, func(k kernel.Kernel, q *ResourceQueue) {
		rig.shader = q.NewShader()
		q.UploadProgram(k, rig.shader, program)
		rig.records = q.UploadBuffer(q.NewBuffer(), make([]byte, 64), true)
	})
	return rig
}

func (r *setRig) newSet(state types.MaterialState, params ...descriptors.ParameterDescr) descriptors.DrawParams {
	var set descriptors.DrawParams
	withShaders(r.k, func(k kernel.Kernel, q *ResourceQueue) {
		set = q.NewDrawParams(k, r.shader, state, params...)
	})
	return set
}

// runSnapshotWith is runSnapshot recording through the dispatch's Kernel, which
// SetDrawParams reports through.
func (r *setRig) runSnapshotWith(request frameSnapshotRequest, record func(kernel.Kernel, *OpQueue)) frameSnapshotResponse {
	r.t.Helper()
	type answer struct {
		response frameSnapshotResponse
		err      error
	}
	done := make(chan answer, 1)
	go func() {
		response, err := callFrame(r.k, request)
		done <- answer{response, err}
	}()
	expiry := time.After(10 * time.Second)
	for {
		select {
		case got := <-done:
			if got.err != nil {
				r.t.Fatalf("snapshot: %v", got.err)
			}
			return got.response
		case <-expiry:
			r.t.Fatal("the snapshot never answered")
		default:
		}
		r.k.ExecuteCommand[recordCmd](recordRequest{withKernel: record})
		r.tick()
		time.Sleep(time.Millisecond)
	}
}

func bindingNamed(t *testing.T, view DrawParamsView, name string) DrawParamsBindingView {
	t.Helper()
	for _, binding := range view.Bindings {
		if binding.Name == name {
			return binding
		}
	}
	t.Fatalf("set %d version %d has no binding %q: %+v", view.Set, view.Version, name, view.Bindings)
	return DrawParamsBindingView{}
}

func TestAFrameSnapshotResolvesEachSetAndVersionItsDrawsName(t *testing.T) {
	rig := newSetRig(t)
	state := types.MaterialState{Blend: types.BlendOpaque, DepthCompare: types.CompareLess, DepthWrite: true, Cull: types.CullBack, FrontFace: types.FrontCW}
	set := rig.newSet(state,
		descriptors.BufferRangeParam("instances", rig.records, 16, 32),
		descriptors.VecParam("tint", m.Vec4{X: 1}),
		descriptors.TextureParam("albedo", descriptors.TextureWithResource("sprites/hero.png")))

	response := rig.runSnapshotWith(frameSnapshotRequest{}, func(k kernel.Kernel, q *OpQueue) {
		world := screenPass(q, 10, "world")
		overlay := screenPass(q, 20, "overlay")
		q.DrawSet(overlay, triangle(), set, 2, 0)
		q.SetDrawParams(k, set, descriptors.MatParam("frame", translation(1)),
			descriptors.SamplerParam("albedoSampler", types.SamplerDesc{Mag: types.FilterNearest}))
		q.DrawSet(world, triangle(), set, 3, 0)
		q.DrawSet(overlay, triangle(), set, 1, 0)
		// A draw through the old material path names no set and is counted only.
		drawInto(q, world)
	})

	if response.DrawCount != 4 || response.InstanceCount != 7 {
		t.Errorf("frame = %d draws / %d instances, want 4 and 7: the totals are unchanged",
			response.DrawCount, response.InstanceCount)
	}
	if len(response.DrawParams) != 2 {
		t.Fatalf("drawParams = %+v, want the set's own values and its one version", response.DrawParams)
	}
	// World runs first, so the version its draw captured is the first entry,
	// though overlay's draw of the set's own values was recorded before it.
	version, own := response.DrawParams[0], response.DrawParams[1]
	if version.Version == 0 || own.Version != 0 {
		t.Fatalf("versions = %d then %d, want the frame's version then the set's own", version.Version, own.Version)
	}
	for _, view := range response.DrawParams {
		if view.Set != descriptors.DrawParamsIndex(set) || view.State != "live" || view.Shader != rig.shader || view.Label != "gfx.shader" {
			t.Errorf("entry = set %d %s shader %d %q, want the live set on gfx.shader", view.Set, view.State, view.Shader, view.Label)
		}
		want := DrawStateView{Blend: "opaque", DepthCompare: "less", DepthWrite: true, Cull: "back", FrontFace: "cw"}
		if view.DrawState == nil || *view.DrawState != want {
			t.Errorf("draw state = %+v, want %+v", view.DrawState, want)
		}
		if len(view.Bindings) != len(setLayout().Resources) {
			t.Errorf("bindings = %d, want every binding the shader declares", len(view.Bindings))
		}
	}
	// World (declaration 0) runs before overlay (declaration 1).
	if len(version.Passes) != 2 || version.Passes[0] != 0 || version.Passes[1] != 1 || version.Draws != 2 || version.Instances != 4 {
		t.Errorf("version drawn in passes %v, %d draws / %d instances; want [0 1], 2 and 4", version.Passes, version.Draws, version.Instances)
	}
	if len(own.Passes) != 1 || own.Passes[0] != 1 || own.Draws != 1 || own.Instances != 2 {
		t.Errorf("own values drawn in passes %v, %d draws / %d instances; want [1], 1 and 2", own.Passes, own.Draws, own.Instances)
	}

	if got := bindingNamed(t, own, "frame"); got.Source != "default" || got.Kind != "uniform" || got.Size != 64 || got.Group != 0 || got.Binding != 0 {
		t.Errorf("own frame = %+v, want a 64-byte uniform at 0/0 left at its default", got)
	}
	if got := bindingNamed(t, version, "frame"); got.Source != "frame" || got.Size != 64 {
		t.Errorf("version frame = %+v, want the frame's 64 bytes", got)
	}
	for _, view := range response.DrawParams {
		if got := bindingNamed(t, view, "tint"); got.Source != "set" || got.Size != 16 || got.Group != 0 || got.Binding != 1 {
			t.Errorf("tint = %+v, want the set's 16 bytes at 0/1 in both", got)
		}
		if got := bindingNamed(t, view, "albedo"); got.Source != "set" || got.Kind != "texture" ||
			got.Path != "sprites/hero.png" || got.Dimension != "texture_2d" || got.Group != 1 || got.Binding != 1 {
			t.Errorf("albedo = %+v, want the set's path at 1/1", got)
		}
		if got := bindingNamed(t, view, "instances"); got.Source != "set" || got.Kind != "storage" ||
			got.Buffer != rig.records.ID() || got.Offset != 16 || got.Range != 32 {
			t.Errorf("instances = %+v, want the set's buffer %d over 16+32", got, rig.records.ID())
		}
	}
	if got := bindingNamed(t, own, "albedoSampler"); got.Source != "default" || got.Kind != "sampler" || got.Sampler != nil {
		t.Errorf("own sampler = %+v, want the default and no sampler reported", got)
	}
	if got := bindingNamed(t, version, "albedoSampler"); got.Source != "frame" || got.Sampler == nil || got.Sampler.Mag != "nearest" {
		t.Errorf("version sampler = %+v, want the frame's nearest sampler", got)
	}
	if bytes := marshalToMap(t, response); bytes["drawParams"] == nil {
		t.Error("drawParams is missing from the flat document")
	}
}

func TestAFilteredSnapshotReportsOnlyTheSetsItsPassesDraw(t *testing.T) {
	rig := newSetRig(t)
	kept := rig.newSet(types.MaterialState{})
	dropped := rig.newSet(types.MaterialState{})

	response := rig.runSnapshotWith(frameSnapshotRequest{Pass: "world"}, func(_ kernel.Kernel, q *OpQueue) {
		q.DrawSet(screenPass(q, 20, "overlay"), triangle(), dropped, 1, 0)
		q.DrawSet(screenPass(q, 10, "world"), triangle(), kept, 1, 0)
	})

	if len(response.DrawParams) != 1 || response.DrawParams[0].Set != descriptors.DrawParamsIndex(kept) {
		t.Fatalf("drawParams = %+v, want only the set the kept pass draws", response.DrawParams)
	}
	if response.DrawParams[0].Passes[0] != 1 {
		t.Errorf("passes = %v, want world's declaration index 1", response.DrawParams[0].Passes)
	}
	if response.DrawCount != 2 {
		t.Errorf("drawCount = %d, want the whole frame's 2", response.DrawCount)
	}
}

func TestAFrameSnapshotSaysWhyASetsDrawsAreDropped(t *testing.T) {
	rig := newSetRig(t)
	released := rig.newSet(types.MaterialState{})
	withShaders(rig.k, func(k kernel.Kernel, q *ResourceQueue) { q.ReleaseDrawParams(k, released) })
	orphaned := rig.newSet(types.MaterialState{})
	withShaders(rig.k, func(k kernel.Kernel, q *ResourceQueue) { q.ReleaseShader(k, rig.shader) })

	response := rig.runSnapshotWith(frameSnapshotRequest{}, func(_ kernel.Kernel, q *OpQueue) {
		pass := screenPass(q, 0, "main")
		q.DrawSet(pass, triangle(), released, 1, 0)
		q.DrawSet(pass, triangle(), descriptors.DrawParamsOf(999), 1, 0)
		q.DrawSet(pass, triangle(), orphaned, 1, 0)
	})

	if len(response.DrawParams) != 3 {
		t.Fatalf("drawParams = %+v, want both sets", response.DrawParams)
	}
	if got := response.DrawParams[0]; got.State != "released" || got.Bindings != nil || got.DrawState != nil {
		t.Errorf("released = %+v, want its state and nothing it no longer has", got)
	}
	if got := response.DrawParams[1]; got.State != "unknown" || got.Set != 999 {
		t.Errorf("unknown = %+v, want set 999 named unknown", got)
	}
	if got := response.DrawParams[2]; got.State != "shaderReleased" || len(got.Bindings) != len(setLayout().Resources) {
		t.Errorf("orphaned = %+v, want its shader named released beside the values it still holds", got)
	}
}

func TestAFrameSnapshotNamesABindingsKindWithItsFlag(t *testing.T) {
	for kind, want := range map[shader.ResourceKind]string{
		shader.ResourceUniformBuffer:                           "uniform",
		shader.ResourceStorageBuffer:                           "storage",
		shader.ResourceStorageBuffer | shader.ResourceWritable: "readWriteStorage",
		shader.ResourceTexture:                                 "texture",
		shader.ResourceTexture | shader.ResourceDepth:          "depthTexture",
		shader.ResourceSampler:                                 "sampler",
		shader.ResourceSampler | shader.ResourceComparison:     "comparisonSampler",
	} {
		if got := bindingKindViewName(kind); got != want {
			t.Errorf("kind %d = %q, want %q", kind, got, want)
		}
	}
}
