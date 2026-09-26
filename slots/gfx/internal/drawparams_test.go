package internal

import (
	"encoding/binary"
	"errors"
	"math"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
	"github.com/dvoyni/cog/slots/gfx/internal/descriptors"
	"github.com/dvoyni/cog/slots/gfx/internal/shader"
	"github.com/dvoyni/cog/slots/gfx/internal/types"
)

// setLayout is what the fake reflection port answers for a set's shader: a
// per-draw uniform, a material uniform, a texture with its sampler, and a
// storage buffer - one binding of every kind, out of name order.
func setLayout() shader.ShaderLayout {
	return shader.ShaderLayout{
		Resources: []shader.ShaderResource{
			{Name: "frame", Kind: shader.ResourceUniformBuffer, Group: 0, Binding: 0, Size: 64},
			{Name: "tint", Kind: shader.ResourceUniformBuffer, Group: 0, Binding: 1, Size: 16},
			{Name: "albedoSampler", Kind: shader.ResourceSampler, Group: 1, Binding: 0},
			{Name: "albedo", Group: 1, Binding: 1},
			{Name: "instances", Kind: shader.ResourceStorageBuffer, Group: 2, Binding: 0},
		},
	}
}

// setWorld is one engine composed over a fake that reflects setLayout, with an
// uploaded shader to build sets on and a durable buffer to fill the storage
// binding with, so a set that names only what a test is about still draws.
type setWorld struct {
	t        *testing.T
	p        *plugin
	backend  *fakeBackend
	k        kernel.Executioner
	reported *[]error
	shader   types.ShaderID
	records  descriptors.BufferDescr
}

func newSetWorld(t *testing.T) *setWorld {
	t.Helper()
	p, backend, k, reported := shaderEngine(t, fstest.MapFS{})
	layout := setLayout()
	backend.reflection = &layout
	w := &setWorld{t: t, p: p, backend: backend, k: k, reported: reported}
	program := compileShader(k, nil, shader.ShaderWithText(programSource)).Program
	w.resources(func(k kernel.Kernel, q *ResourceQueue) {
		w.shader = q.NewShader()
		q.UploadProgram(k, w.shader, program)
		w.records = q.UploadBuffer(q.NewBuffer(), make([]byte, 64), true)
	})
	return w
}

func (w *setWorld) resources(use func(kernel.Kernel, *ResourceQueue)) { withShaders(w.k, use) }

// newSet creates a set on the world's shader that fills its storage binding.
func (w *setWorld) newSet(state types.DrawState, params ...descriptors.ParameterDescr) descriptors.DrawParams {
	var set descriptors.DrawParams
	w.resources(func(k kernel.Kernel, q *ResourceQueue) {
		params = append([]descriptors.ParameterDescr{descriptors.BufferParam("instances", w.records)}, params...)
		set = q.NewDrawParams(k, w.shader, state, params...)
	})
	return set
}

// frame records one frame through the dispatch's Kernel and renders it.
func (w *setWorld) frame(record func(kernel.Kernel, *OpQueue)) {
	w.k.ExecuteCommand[recordCmd](recordRequest{withKernel: record})
	w.k.ExecuteCommand[PresentCmd](PresentRequest{})
	w.k.PublishEvent(app.RenderEvent{}).Wait()
}

func screenPass(q *OpQueue, order int, label string) descriptors.PassRef {
	return q.NewPass(descriptors.PassDescr{
		Order: descriptors.Order(order), Target: descriptors.ScreenTarget(), Depth: descriptors.DepthAuto(), Label: label,
	})
}

// uniformsBound is every uniform block the last frame bound at group and
// binding, in replay order.
func (w *setWorld) uniformsBound(group, binding int) []backendOp {
	var bound []backendOp
	for _, op := range w.backend.lastOps {
		if op.kind == testOpSetUniformBlock && op.group == group && op.binding == binding {
			bound = append(bound, op)
		}
	}
	return bound
}

func floatsOf(data []byte) []float32 {
	values := make([]float32, len(data)/4)
	for i := range values {
		values[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4:]))
	}
	return values
}

func translation(x float32) m.Mat4 {
	matrix := m.NewMat4()
	matrix[12] = x
	return matrix
}

// A draw through a set binds the set's own values: a uniform's bytes, the
// texture, the sampler and the buffer, with the values resolved when the set
// was created rather than looked up by name at the draw.
func TestADrawBindsItsSetsOwnValues(t *testing.T) {
	w := newSetWorld(t)
	var albedo descriptors.TextureDescr
	w.resources(func(_ kernel.Kernel, q *ResourceQueue) {
		albedo = q.NewTexture(2, 2, 1, descriptors.FormatRGBA8, false)
	})
	set := w.newSet(types.DrawState{},
		descriptors.ColorParam("tint", m.Color{R: 1, G: 0.5, B: 0.25, A: 1}),
		descriptors.TextureParam("albedo", albedo),
		descriptors.SamplerParam("albedoSampler", types.SamplerDesc{AddressU: types.AddressRepeat}),
	)

	w.frame(func(_ kernel.Kernel, q *OpQueue) { q.Draw(screenPass(q, 0, "main"), triangle(), set, 1, 0) })

	if len(*w.reported) != 0 {
		t.Fatalf("reported %v", *w.reported)
	}
	if w.backend.passDraws[0] != 1 {
		t.Fatalf("draws = %v, want the set's draw", w.backend.passDraws)
	}
	tint := w.uniformsBound(0, 1)
	if len(tint) != 1 || !equalFloats(floatsOf(tint[0].data), []float32{1, 0.5, 0.25, 1}) {
		t.Errorf("tint bound %+v, want the set's color", tint)
	}
	if !w.backend.boundTexture(albedo.ID()) {
		t.Errorf("texture %d was never bound", albedo.ID())
	}
	var buffer, sampler bool
	for _, op := range w.backend.lastOps {
		buffer = buffer || (op.kind == testOpSetBuffer && op.buffer == w.records.ID() && op.group == 2)
		sampler = sampler || (op.kind == testOpSetSampler && op.group == 1 && op.binding == 0)
	}
	if !buffer || !sampler {
		t.Errorf("buffer bound %v, sampler bound %v, want both", buffer, sampler)
	}
}

// A draw sees the values its set had when the draw was recorded. Passes run in
// Order, not in recording order, so a set changed between draws into two
// passes must not let the later value leak into the earlier draw.
func TestADrawSeesTheVersionItWasRecordedWith(t *testing.T) {
	w := newSetWorld(t)
	set := w.newSet(types.DrawState{})

	w.frame(func(k kernel.Kernel, q *OpQueue) {
		passA := screenPass(q, 1, "a")
		passB := screenPass(q, 0, "b")
		q.SetDrawParams(k, set, descriptors.MatParam("frame", translation(1)))
		q.Draw(passA, triangle(), set, 1, 0)
		q.SetDrawParams(k, set, descriptors.MatParam("frame", translation(2)))
		q.Draw(passB, triangle(), set, 1, 0)
	})

	frames := w.uniformsBound(0, 0)
	if len(frames) != 2 {
		t.Fatalf("frame uniform bound %d times, want once a draw", len(frames))
	}
	// Pass b runs first, so the first binding is b's draw.
	if got := floatsOf(frames[0].data)[12]; got != 2 {
		t.Errorf("pass b's draw saw x = %v, want 2", got)
	}
	if got := floatsOf(frames[1].data)[12]; got != 1 {
		t.Errorf("pass a's draw saw x = %v, want 1: the later change leaked into it", got)
	}
	if len(*w.reported) != 0 {
		t.Errorf("reported %v", *w.reported)
	}
}

// A version no draw has captured is patched in place: several calls before
// the draws make one version, and bytes a call wrote are overwritten by the
// next rather than appended.
func TestAnUncapturedVersionIsPatchedInPlace(t *testing.T) {
	w := newSetWorld(t)
	set := w.newSet(types.DrawState{})
	var slots, bytes int

	w.frame(func(k kernel.Kernel, q *OpQueue) {
		q.SetDrawParams(k, set, descriptors.MatParam("frame", translation(1)))
		q.SetDrawParams(k, set, descriptors.ColorParam("tint", m.Color{G: 1, A: 1}))
		q.SetDrawParams(k, set, descriptors.MatParam("frame", translation(3)))
		q.Draw(screenPass(q, 0, "main"), triangle(), set, 1, 0)
		slots, bytes = len(q.versionValues), len(q.versionBytes)
	})

	if want := len(setLayout().Resources); slots != want {
		t.Errorf("version slots = %d, want one version of %d", slots, want)
	}
	if bytes != 64+16 {
		t.Errorf("version bytes = %d, want the frame and tint written once each", bytes)
	}
	if got := floatsOf(w.uniformsBound(0, 0)[0].data)[12]; got != 3 {
		t.Errorf("frame x = %v, want the last value set", got)
	}
	if got := floatsOf(w.uniformsBound(0, 1)[0].data); !equalFloats(got, []float32{0, 1, 0, 1}) {
		t.Errorf("tint = %v, want the version's", got)
	}
}

// A version is copied when a draw has captured it, and the copy shares the
// bytes of every binding it does not change.
func TestACapturedVersionIsCopiedAndSharesWhatItKeeps(t *testing.T) {
	w := newSetWorld(t)
	set := w.newSet(types.DrawState{})
	var bytes int

	w.frame(func(k kernel.Kernel, q *OpQueue) {
		pass := screenPass(q, 0, "main")
		q.SetDrawParams(k, set, descriptors.MatParam("frame", translation(1)), descriptors.ColorParam("tint", m.Color{B: 1, A: 1}))
		q.Draw(pass, triangle(), set, 1, 0)
		q.SetDrawParams(k, set, descriptors.MatParam("frame", translation(2)))
		q.Draw(pass, triangle(), set, 1, 0)
		bytes = len(q.versionBytes)
	})

	if bytes != 64+16+64 {
		t.Errorf("version bytes = %d, want the second version to write only its frame", bytes)
	}
	tints := w.uniformsBound(0, 1)
	if len(tints) != 2 || tints[0].offset != tints[1].offset {
		t.Errorf("tint bound at %+v, want both draws to bind the one shared upload", tints)
	}
	frames := w.uniformsBound(0, 0)
	if floatsOf(frames[0].data)[12] != 1 || floatsOf(frames[1].data)[12] != 2 {
		t.Errorf("frames bound %v and %v, want 1 then 2", floatsOf(frames[0].data)[12], floatsOf(frames[1].data)[12])
	}
}

// The next frame starts from the set's own values: a version is the frame's.
func TestTheNextFrameStartsFromTheSetsOwnValues(t *testing.T) {
	w := newSetWorld(t)
	set := w.newSet(types.DrawState{}, descriptors.ColorParam("tint", m.Color{R: 1, A: 1}))

	w.frame(func(k kernel.Kernel, q *OpQueue) {
		q.SetDrawParams(k, set, descriptors.ColorParam("tint", m.Color{G: 1, A: 1}))
		q.Draw(screenPass(q, 0, "main"), triangle(), set, 1, 0)
	})
	w.frame(func(_ kernel.Kernel, q *OpQueue) { q.Draw(screenPass(q, 0, "main"), triangle(), set, 1, 0) })

	if got := floatsOf(w.uniformsBound(0, 1)[0].data); !equalFloats(got, []float32{1, 0, 0, 1}) {
		t.Errorf("tint = %v, want the set's own red", got)
	}
}

// Each distinct uniform value is uploaded once a frame, and every draw sharing
// it binds the same offset: draws of one set with no change between them pack
// nothing per draw.
func TestEachDistinctUniformIsUploadedOnce(t *testing.T) {
	w := newSetWorld(t)
	set := w.newSet(types.DrawState{}, descriptors.ColorParam("tint", m.Color{R: 1, A: 1}))

	w.frame(func(k kernel.Kernel, q *OpQueue) {
		pass := screenPass(q, 0, "main")
		q.SetDrawParams(k, set, descriptors.MatParam("frame", translation(1)))
		for range 3 {
			q.Draw(pass, triangle(), set, 1, 0)
		}
	})

	// Two values - the version's frame and the set's tint - each claim one
	// aligned block of the arena.
	if got := len(w.backend.uniforms); got != 2*UniformAlignment {
		t.Errorf("uniform arena = %d bytes, want %d: one upload per distinct value", got, 2*UniformAlignment)
	}
	for _, binding := range []int{0, 1} {
		bound := w.uniformsBound(0, binding)
		if len(bound) != 3 || bound[0].offset != bound[1].offset || bound[1].offset != bound[2].offset {
			t.Errorf("binding %d bound at %+v, want three draws binding one offset", binding, bound)
		}
	}
}

// Unsupplied bindings take their defaults: a uniform is zero, a sampler the
// default and a texture white, which is the zero id the backend fills.
func TestUnsuppliedBindingsTakeTheirDefaults(t *testing.T) {
	w := newSetWorld(t)
	set := w.newSet(types.DrawState{})

	w.frame(func(_ kernel.Kernel, q *OpQueue) { q.Draw(screenPass(q, 0, "main"), triangle(), set, 1, 0) })

	if w.backend.passDraws[0] != 1 || len(*w.reported) != 0 {
		t.Fatalf("draws %v, reported %v, want the draw and no report", w.backend.passDraws, *w.reported)
	}
	for _, uniform := range []backendOp{w.uniformsBound(0, 0)[0], w.uniformsBound(0, 1)[0]} {
		for _, b := range uniform.data {
			if b != 0 {
				t.Fatalf("unsupplied uniform bound %v, want zeros", uniform.data)
			}
		}
	}
	var texture, sampler bool
	for _, op := range w.backend.lastOps {
		texture = texture || (op.kind == testOpSetTexture && op.texture == 0 && op.group == 1 && op.binding == 1)
		sampler = sampler || (op.kind == testOpSetSampler && op.group == 1 && op.binding == 0)
	}
	if !texture || !sampler {
		t.Errorf("white texture bound %v, default sampler bound %v, want both", texture, sampler)
	}
}

// A storage buffer has no default: a draw whose set and version leave one
// unsupplied is dropped every frame and reported once.
func TestAnUnsuppliedStorageBufferDropsTheDrawAndIsReportedOnce(t *testing.T) {
	w := newSetWorld(t)
	var set descriptors.DrawParams
	w.resources(func(k kernel.Kernel, q *ResourceQueue) { set = q.NewDrawParams(k, w.shader, types.DrawState{}) })

	for range 2 {
		w.frame(func(_ kernel.Kernel, q *OpQueue) { q.Draw(screenPass(q, 0, "main"), triangle(), set, 1, 0) })
		if w.backend.passDraws[0] != 0 {
			t.Fatalf("draws = %v, want the draw dropped", w.backend.passDraws)
		}
	}
	var unsupplied types.ErrStorageBufferUnsupplied
	if len(*w.reported) != 1 || !errors.As((*w.reported)[0], &unsupplied) || unsupplied.Parameter != "instances" || unsupplied.Unbaked {
		t.Fatalf("reported %v, want one ErrStorageBufferUnsupplied naming instances", *w.reported)
	}
}

// A version may bind what lives one frame: a temporary buffer fills the
// storage binding for that frame's draws.
func TestAVersionBindsATemporary(t *testing.T) {
	w := newSetWorld(t)
	var set descriptors.DrawParams
	w.resources(func(k kernel.Kernel, q *ResourceQueue) { set = q.NewDrawParams(k, w.shader, types.DrawState{}) })
	var arena descriptors.BufferDescr

	w.frame(func(k kernel.Kernel, q *OpQueue) {
		arena = q.NewTemporaryBuffer(make([]byte, 2*descriptors.StorageAlignment), true)
		q.SetDrawParams(k, set, descriptors.BufferRangeParam("instances", arena, descriptors.StorageAlignment, descriptors.StorageAlignment))
		q.Draw(screenPass(q, 0, "main"), triangle(), set, 4, 1)
	})

	if len(*w.reported) != 0 || w.backend.passDraws[0] != 1 {
		t.Fatalf("reported %v, draws %v, want one draw", *w.reported, w.backend.passDraws)
	}
	var bound bool
	for _, op := range w.backend.lastOps {
		bound = bound || (op.kind == testOpSetBuffer && op.buffer == arena.ID() &&
			op.offset == descriptors.StorageAlignment && op.size == descriptors.StorageAlignment)
	}
	if !bound {
		t.Error("the temporary's range was not bound")
	}
	if draw := w.backend.draws[len(w.backend.draws)-1]; draw.instances != 4 || draw.firstInstance != 1 {
		t.Errorf("draw %+v, want 4 instances from 1", draw)
	}
}

// The pipeline is keyed on the set's shader and Draw state: two sets sharing
// both share a pipeline, and a set under another state builds its own.
func TestSetsShareAPipelineOnlyUnderOneState(t *testing.T) {
	w := newSetWorld(t)
	opaque := types.DrawState{Blend: types.BlendOpaque, DepthCompare: types.CompareLess, DepthWrite: true}
	first := w.newSet(opaque, descriptors.ColorParam("tint", m.Color{R: 1, A: 1}))
	second := w.newSet(opaque, descriptors.ColorParam("tint", m.Color{G: 1, A: 1}))
	overlay := w.newSet(types.DrawState{Blend: types.BlendAdditive})

	w.frame(func(_ kernel.Kernel, q *OpQueue) {
		pass := screenPass(q, 0, "main")
		for _, set := range []descriptors.DrawParams{first, second, overlay} {
			q.Draw(pass, triangle(), set, 1, 0)
		}
	})

	if len(w.backend.lastPipelines) != 2 {
		t.Fatalf("built %d pipelines, want 2", len(w.backend.lastPipelines))
	}
	if w.backend.lastPipelines[0].State != opaque || w.backend.lastPipelines[1].State.Blend != types.BlendAdditive {
		t.Errorf("pipelines %+v, want the opaque one then the additive one", w.backend.lastPipelines)
	}
	if w.backend.lastPipelines[0].Shader != w.shader {
		t.Errorf("pipeline shader = %d, want the set's %d", w.backend.lastPipelines[0].Shader, w.shader)
	}
}

// UpdateDrawParams changes the set's own values for good.
func TestUpdateDrawParamsPersists(t *testing.T) {
	w := newSetWorld(t)
	set := w.newSet(types.DrawState{}, descriptors.ColorParam("tint", m.Color{R: 1, A: 1}))
	w.resources(func(k kernel.Kernel, q *ResourceQueue) {
		q.UpdateDrawParams(k, set, descriptors.ColorParam("tint", m.Color{B: 1, A: 1}))
	})

	for range 2 {
		w.frame(func(_ kernel.Kernel, q *OpQueue) { q.Draw(screenPass(q, 0, "main"), triangle(), set, 1, 0) })
		if got := floatsOf(w.uniformsBound(0, 1)[0].data); !equalFloats(got, []float32{0, 0, 1, 1}) {
			t.Errorf("tint = %v, want the updated blue", got)
		}
	}
}

// Inline bytes a durable set is given are baked into resources the set owns,
// and ReleaseDrawParams frees them. Replacing an owned value frees it too.
func TestInlineBytesAreBakedIntoResourcesTheSetOwns(t *testing.T) {
	w := newSetWorld(t)
	pixels := []byte{1, 2, 3, 4}
	set := w.newSet(types.DrawState{},
		descriptors.TextureParam("albedo", descriptors.TextureWithBytes(1, 1, descriptors.FormatRGBA8, pixels, true, false)))
	w.frame(func(_ kernel.Kernel, q *OpQueue) { q.Draw(screenPass(q, 0, "main"), triangle(), set, 1, 0) })
	// The bake is a durable one, replayed with the frame that followed it.
	var owned types.TextureID
	for _, op := range w.backend.lastOps {
		if op.kind == testOpAllocateTexture {
			owned = op.texture
		}
	}
	if owned == 0 || !w.backend.boundTexture(owned) {
		t.Fatalf("baked texture %d, bound %v, want the set's own texture drawn", owned, w.backend.boundTextures)
	}

	w.resources(func(k kernel.Kernel, q *ResourceQueue) { q.ReleaseDrawParams(k, set) })
	w.frame(func(_ kernel.Kernel, q *OpQueue) {})

	var released bool
	for _, op := range w.backend.lastOps {
		released = released || (op.kind == testOpReleaseTexture && op.texture == owned)
	}
	if !released {
		t.Errorf("the set's texture %d was not released with it", owned)
	}
	if len(*w.reported) != 0 {
		t.Errorf("reported %v", *w.reported)
	}
}

// A frame still naming a set released since it was recorded drops the draw,
// silently: it is the frame rendered after the set was let go.
func TestADrawNamingAReleasedSetIsDroppedSilently(t *testing.T) {
	w := newSetWorld(t)
	set := w.newSet(types.DrawState{})

	w.k.ExecuteCommand[recordCmd](recordRequest{withKernel: func(_ kernel.Kernel, q *OpQueue) {
		q.Draw(screenPass(q, 0, "main"), triangle(), set, 1, 0)
	}})
	w.k.ExecuteCommand[PresentCmd](PresentRequest{})
	w.resources(func(k kernel.Kernel, q *ResourceQueue) { q.ReleaseDrawParams(k, set) })
	w.k.PublishEvent(app.RenderEvent{}).Wait()

	if countOps(w.backend.lastOps, testOpDraw) != 0 {
		t.Error("a draw naming a released set reached the backend")
	}
	if len(*w.reported) != 0 {
		t.Errorf("reported %v, want the drop silent", *w.reported)
	}
}

// A draw may not sample the texture its own pass renders into, whichever of
// the set or the version names it.
func TestASetDrawSamplingItsOwnAttachmentIsDropped(t *testing.T) {
	w := newSetWorld(t)
	set := w.newSet(types.DrawState{})
	var target descriptors.TextureDescr
	w.resources(func(_ kernel.Kernel, q *ResourceQueue) { target = q.NewRenderTarget(4, 4, 1, descriptors.FormatRGBA8) })

	w.frame(func(k kernel.Kernel, q *OpQueue) {
		pass := q.NewPass(descriptors.PassDescr{Target: descriptors.TextureTarget(target, 0, 0), Label: "offscreen"})
		q.SetDrawParams(k, set, descriptors.TextureParam("albedo", target))
		q.Draw(pass, triangle(), set, 1, 0)
	})

	var sampled types.ErrDrawSamplesAttachment
	if len(*w.reported) != 1 || !errors.As((*w.reported)[0], &sampled) || sampled.Parameter != "albedo" {
		t.Fatalf("reported %v, want ErrDrawSamplesAttachment naming albedo", *w.reported)
	}
	if countOps(w.backend.lastOps, testOpDraw) != 0 {
		t.Error("the draw sampling its own attachment reached the backend")
	}
}

// Every programmer mistake a set call can make is reported through the
// calling System's Kernel, once however often it is repeated, and the bad
// param is ignored while the rest take.
func TestSetMistakesAreReportedOnceAndIgnored(t *testing.T) {
	w := newSetWorld(t)
	set := w.newSet(types.DrawState{})
	var temporary descriptors.BufferDescr
	w.k.ExecuteCommand[recordCmd](recordRequest{fn: func(q *OpQueue) {
		temporary = q.NewTemporaryBuffer(make([]byte, 16), true)
	}})
	bad := []descriptors.ParameterDescr{
		descriptors.FloatParam("missing", 1),
		descriptors.TextureParam("tint", descriptors.BakedTexture(1, 1, 1)),
		descriptors.FloatParam("tint", 1),
		descriptors.ColorParam("tint", m.Color{R: 1, A: 1}),
	}

	for range 2 {
		w.resources(func(k kernel.Kernel, q *ResourceQueue) {
			q.UpdateDrawParams(k, set, bad...)
			q.UpdateDrawParams(k, set, descriptors.BufferParam("instances", temporary))
		})
		w.frame(func(k kernel.Kernel, q *OpQueue) {
			q.SetDrawParams(k, set, bad...)
			q.Draw(screenPass(q, 0, "main"), triangle(), set, 1, 0)
		})
	}

	var unknown, update, set2 int
	var kind types.ErrParameterKindMismatch
	var size types.ErrUniformSizeMismatch
	var temp types.ErrDrawParamTemporary
	for _, err := range *w.reported {
		var named types.ErrDrawParamUnknown
		switch {
		case errors.As(err, &named):
			unknown++
			if named.Call == "UpdateDrawParams" {
				update++
			} else if named.Call == "SetDrawParams" {
				set2++
			}
		case errors.As(err, &kind), errors.As(err, &size), errors.As(err, &temp):
		default:
			t.Errorf("unexpected report %v", err)
		}
	}
	// Three bad params, each through two calls, and the temporary once.
	if len(*w.reported) != 7 || unknown != 2 || update != 1 || set2 != 1 {
		t.Fatalf("reported %d: %v, want each mistake once per call", len(*w.reported), *w.reported)
	}
	if kind.Declared != "a uniform" || kind.Supplied != "texture" || size.Declared != 16 || size.Supplied != 4 {
		t.Errorf("kind %+v, size %+v, want a texture refused for a uniform and 4 bytes for 16", kind, size)
	}
	if temp.Parameter != "instances" {
		t.Errorf("temporary %+v, want instances", temp)
	}
	// The good param beside the bad ones took, and the temporary did not
	// replace the durable buffer.
	if got := floatsOf(w.uniformsBound(0, 1)[0].data); !equalFloats(got, []float32{1, 0, 0, 1}) {
		t.Errorf("tint = %v, want the good param's red", got)
	}
	if w.backend.passDraws[0] != 1 {
		t.Errorf("draws = %v, want the draw kept", w.backend.passDraws)
	}
}

// A call naming a set that is not live - never created, or released - is
// reported once and ignored.
func TestACallNamingASetThatIsNotLiveIsReported(t *testing.T) {
	w := newSetWorld(t)
	released := w.newSet(types.DrawState{})
	w.resources(func(k kernel.Kernel, q *ResourceQueue) { q.ReleaseDrawParams(k, released) })
	unknown := descriptors.DrawParamsOf(9999)

	for range 2 {
		w.resources(func(k kernel.Kernel, q *ResourceQueue) {
			q.UpdateDrawParams(k, unknown)
			q.ReleaseDrawParams(k, released)
		})
		w.frame(func(k kernel.Kernel, q *OpQueue) {
			q.SetDrawParams(k, released, descriptors.ColorParam("tint", m.Color{}))
			q.SetDrawParams(k, unknown)
		})
	}

	want := []types.ErrDrawParamsNotLive{
		{Set: 9999, Call: "UpdateDrawParams"},
		{Set: descriptors.DrawParamsIndex(released), Call: "ReleaseDrawParams", Released: true},
		{Set: descriptors.DrawParamsIndex(released), Call: "SetDrawParams", Released: true},
		{Set: 9999, Call: "SetDrawParams"},
	}
	if len(*w.reported) != len(want) {
		t.Fatalf("reported %v, want %d ErrDrawParamsNotLive", *w.reported, len(want))
	}
	for i, err := range *w.reported {
		var got types.ErrDrawParamsNotLive
		if !errors.As(err, &got) || got != want[i] {
			t.Errorf("report %d = %v, want %+v", i, err, want[i])
		}
	}
}

// A set built on a shader with no program is created failed: the creation is
// reported, and the set exists, takes nothing, says nothing and draws nothing.
func TestASetOnAShaderWithNoProgramIsCreatedFailed(t *testing.T) {
	w := newSetWorld(t)
	var set descriptors.DrawParams
	var reserved types.ShaderID
	w.resources(func(k kernel.Kernel, q *ResourceQueue) {
		reserved = q.NewShader()
		set = q.NewDrawParams(k, reserved, types.DrawState{}, descriptors.ColorParam("tint", m.Color{}))
		q.UpdateDrawParams(k, set, descriptors.ColorParam("tint", m.Color{}))
	})
	w.frame(func(k kernel.Kernel, q *OpQueue) {
		q.SetDrawParams(k, set, descriptors.ColorParam("tint", m.Color{}))
		q.Draw(screenPass(q, 0, "main"), triangle(), set, 1, 0)
	})

	var none types.ErrShaderHasNoProgram
	if len(*w.reported) != 1 || !errors.As((*w.reported)[0], &none) || none.Shader != reserved {
		t.Fatalf("reported %v, want one ErrShaderHasNoProgram", *w.reported)
	}
	if set == (descriptors.DrawParams{}) || countOps(w.backend.lastOps, testOpDraw) != 0 {
		t.Errorf("set %v drew %d, want a set that draws nothing", set, countOps(w.backend.lastOps, testOpDraw))
	}
}

// A set whose shader was released draws nothing: the shader's release is the
// app reloading it, and the frame between is the one rendered after.
func TestASetWhoseShaderWasReleasedDrawsNothing(t *testing.T) {
	w := newSetWorld(t)
	set := w.newSet(types.DrawState{})
	w.resources(func(k kernel.Kernel, q *ResourceQueue) { q.ReleaseShader(k, w.shader) })

	w.frame(func(_ kernel.Kernel, q *OpQueue) { q.Draw(screenPass(q, 0, "main"), triangle(), set, 1, 0) })

	if countOps(w.backend.lastOps, testOpDraw) != 0 || len(*w.reported) != 0 {
		t.Errorf("drew %d, reported %v, want nothing", countOps(w.backend.lastOps, testOpDraw), *w.reported)
	}
}

// setBench is a set, its queues and a translator built outside a composition,
// which is what a steady-state measurement needs: nothing behind the calls but
// the calls.
type setBench struct {
	backend    *fakeBackend
	resources  *ResourceQueue
	queue      *OpQueue
	translator *translator
	set        descriptors.DrawParams
	mesh       descriptors.MeshDescr
}

func newSetBench(tb testing.TB) *setBench {
	tb.Helper()
	layout := setLayout()
	backend := &fakeBackend{reflection: &layout, layout: &layout}
	resources := NewResourceQueue(idsOf(backend))
	b := &setBench{
		backend: backend, resources: resources, translator: newTranslator(),
		queue: newOpQueue(idsOf(backend), resources.drawParams.registry),
		mesh: descriptors.Mesh(
			descriptors.BakedBuffer(1, 3*28),
			types.TopologyTriangleList,
			descriptors.Attr(0, descriptors.Float32x3), descriptors.Attr(12, descriptors.Float32x4),
		),
	}
	program, err := shader.CompileShader(nil, shader.ShaderWithText(programSource), backend.ReflectShader)
	if err != nil {
		tb.Fatal(err)
	}
	// The zero Kernel is legal here because nothing reaches it: every call is
	// correct, and a kernel is only ever touched by a mistake.
	id := resources.NewShader()
	resources.UploadProgram(kernel.Kernel{}, id, program)
	records := resources.UploadBuffer(resources.NewBuffer(), make([]byte, 64), true)
	albedo := resources.NewTexture(2, 2, 1, descriptors.FormatRGBA8, false)
	b.set = resources.NewDrawParams(kernel.Kernel{}, id, types.DrawState{},
		descriptors.ColorParam("tint", m.Color{R: 1, G: 1, B: 1, A: 1}),
		descriptors.TextureParam("albedo", albedo),
		descriptors.SamplerParam("albedoSampler", types.SamplerDesc{}),
		descriptors.BufferParam("instances", records),
	)
	// The durable ops are replayed once, as the render does, and dropped.
	b.translate(tb)
	ResourceQueueReset(resources)
	return b
}

func (b *setBench) translate(tb testing.TB) *Queue {
	out, err := b.translator.translate(kernel.Kernel{}, b.queue, b.resources, b.backend, noFiles, types.CaptureDesc{}, false)
	if err != nil {
		tb.Fatalf("translate: %v", err)
	}
	return out
}

// record records draws draws of the set, each with a version of its own.
func (b *setBench) record(draws int) {
	pass := b.queue.NewPass(descriptors.PassDescr{Target: descriptors.ScreenTarget(), Depth: descriptors.DepthAuto()})
	for i := range draws {
		b.queue.SetDrawParams(kernel.Kernel{}, b.set, descriptors.MatParam("frame", translation(float32(i))))
		b.queue.Draw(pass, b.mesh, b.set, 1, 0)
	}
}

// Recording a version and a draw, and translating a steady frame of them,
// allocate nothing.
func TestSetDrawsAllocateNothing(t *testing.T) {
	if raceEnabled {
		t.Skip("allocation counts are not meaningful under the race detector")
	}
	b := newSetBench(t)
	const draws = 100
	record := func() {
		b.queue.reset()
		b.record(draws)
	}
	record()
	record()
	if allocs := testing.AllocsPerRun(20, record); allocs != 0 {
		t.Errorf("recording %d versioned draws allocated %v times, want 0", draws, allocs)
	}

	b.translate(t)
	b.backend.Execute(b.translate(t))
	if got := countOps(b.backend.lastOps, testOpDraw); got != draws {
		t.Fatalf("draw ops = %d, want %d: the frame must reach the draws it measures", got, draws)
	}
	if allocs := testing.AllocsPerRun(20, func() { b.translate(t) }); allocs != 0 {
		t.Errorf("translating %d versioned draws allocated %v times, want 0", draws, allocs)
	}
}

// BenchmarkOpQueueDrawSteadyState records a version of one binding and a draw
// through a set, each frame.
func BenchmarkOpQueueDrawSteadyState(b *testing.B) {
	bench := newSetBench(b)
	bench.record(1)
	bench.queue.reset()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		bench.record(1)
		bench.queue.reset()
	}
}

// BenchmarkTranslateSteadyState translates a hundred draws each with its own
// per-draw uniform, through a set holding a material uniform, a texture, a
// sampler and a storage buffer.
func BenchmarkTranslateSteadyState(b *testing.B) {
	bench := newSetBench(b)
	bench.record(100)
	bench.translate(b)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		bench.translate(b)
	}
}

// The registry is read without a lock while the ResourceQueue publishes to it:
// a reader that sees a set live sees its program, across the chunk growth that
// publishing hundreds of sets forces.
func TestTheRegistryIsReadWhilePublished(t *testing.T) {
	layout := setLayout()
	backend := &fakeBackend{reflection: &layout}
	program, err := shader.CompileShader(nil, shader.ShaderWithText(programSource), backend.ReflectShader)
	if err != nil {
		t.Fatal(err)
	}
	registry := &drawParamsRegistry{}
	const sets = 4 << setChunkBits
	done := make(chan struct{})
	go func() {
		defer close(done)
		for id := uint32(1); id <= sets; id++ {
			registry.publish(id, program, setLive)
			if id%3 == 0 {
				registry.release(id)
			}
		}
	}()
	for reading := true; reading; {
		select {
		case <-done:
			reading = false
		default:
		}
		for id := uint32(1); id <= sets; id += 7 {
			if state, seen := registry.state(id); state == setLive && !seen.Valid() {
				t.Fatalf("set %d read live with no program", id)
			}
		}
	}
	if state, _ := registry.state(sets); state != setLive {
		t.Errorf("the last set reads %d, want live", state)
	}
	if state, _ := registry.state(3); state != setReleased {
		t.Errorf("a released set reads %d, want released", state)
	}
}

// SetDrawParams copies a param's bytes before it returns, so a record borrowed
// through RawParameterRef can be refilled for the next batch at once: each draw
// binds the fill it was recorded with.
func TestSetDrawParamsCopiesABorrowedRecord(t *testing.T) {
	_, backend, k, reported := shaderEngine(t, fstest.MapFS{})
	layout := shader.ShaderLayout{Resources: []shader.ShaderResource{
		{Name: "record", Kind: shader.ResourceUniformBuffer, Group: 0, Binding: 0, Size: 96},
	}}
	backend.reflection = &layout
	program := compileShader(k, nil, shader.ShaderWithText(programSource)).Program
	var set descriptors.DrawParams
	withShaders(k, func(k kernel.Kernel, q *ResourceQueue) {
		id := q.NewShader()
		q.UploadProgram(k, id, program)
		set = q.NewDrawParams(k, id, types.DrawState{})
	})
	record := &wellPacked{}
	k.ExecuteCommand[recordCmd](recordRequest{withKernel: func(k kernel.Kernel, q *OpQueue) {
		pass := screenPass(q, 0, "main")
		for _, x := range []float32{1, 2} {
			record.Amount.X = x
			q.SetDrawParams(k, set, descriptors.RawParameterRef("record", record))
			q.Draw(pass, triangle(), set, 1, 0)
		}
		record.Amount.X = 3
	}})
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()
	if len(*reported) != 0 {
		t.Fatalf("reported %v", *reported)
	}
	var bound []float32
	for _, op := range backend.lastOps {
		if op.kind == testOpSetUniformBlock {
			bound = append(bound, floatsOf(op.data)[0])
		}
	}
	if !equalFloats(bound, []float32{1, 2}) {
		t.Fatalf("draws bound %v, want each its own fill", bound)
	}
}
