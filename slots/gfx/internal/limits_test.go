package internal

import (
	"errors"
	"testing"

	"github.com/dvoyni/cog/slots/gfx/internal/descriptors"

	"github.com/dvoyni/cog/slots/gfx/internal/types"

	"github.com/dvoyni/cog/slots/gfx/internal/shader"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
)

func TestDefaultLimitsAreTheBrowserFloor(t *testing.T) {
	// These are the WebGPU spec floor, not any device's numbers: a desktop
	// adapter reports hardware limits, and checking against those passes a build
	// that cannot run in a browser.
	want := types.Limits{
		MaxBindGroups:                   4,
		MaxStorageBuffersPerShaderStage: 8,
		MaxStorageBufferBindingSize:     128 << 20,
		MaxUniformBuffersPerShaderStage: 12,
		MaxUniformBufferBindingSize:     64 << 10,
		MaxBufferSize:                   256 << 20,
	}
	if DefaultLimits() != want {
		t.Errorf("DefaultLimits = %+v, want the web floor %+v", DefaultLimits(), want)
	}
}

func TestShaderOverTheWebFloorIsReportedOnceAndStillRenders(t *testing.T) {
	p := newPlugin()
	var reported []error
	k := newTestKernelWithErrors(t, p, func(err error) { reported = append(reported, err) })
	layout := shader.ShaderLayout{
		Resources: []shader.ShaderResource{{Name: "params", Kind: shader.ResourceUniformBuffer, Group: 0, Binding: 0, Size: 64, Members: []shader.StorageMember{{Name: "mvp", Offset: 0}}}},
	}
	for i := range 9 {
		layout.Resources = append(layout.Resources, shader.ShaderResource{
			Name: "records", Kind: shader.ResourceStorageBuffer, Group: 1, Binding: i,
		})
	}
	backend := &fakeBackend{layout: &layout}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	// The nine bindings share one name, so one parameter fills them all. They
	// have to be filled: an unsupplied storage binding is fatal to the draw,
	// and this test is about a draw that renders despite the diagnostic.
	records := descriptors.BufferParam("records", descriptors.BufferWithBytes([]byte{1, 2, 3, 4}, true))
	for range 2 {
		w := recordList(t, k)
		w.Draw(triangle(), testMaterial(records), descriptors.MatParam("mvp", m.NewMat4()))
		k.ExecuteCommand[PresentCmd](PresentRequest{})
		k.PublishEvent(app.RenderEvent{}).Wait()
	}

	// The shader runs here: the diagnostic is about a browser refusing it, so
	// dropping the draw would break a desktop build over a portability warning.
	if backend.passDraws[0] != 1 {
		t.Errorf("draws = %d, want the draw rendered anyway", backend.passDraws[0])
	}
	var exceeded shader.ErrShaderExceedsWebLimits
	found := 0
	for _, err := range reported {
		if errors.As(err, &exceeded) {
			found++
		}
	}
	// Shaders are cached, so the check runs at creation: two frames, one report.
	if found != 1 {
		t.Fatalf("reports = %d over two frames, want exactly 1: %v", found, reported)
	}
	if exceeded.Declared != 9 || exceeded.Floor != DefaultLimits().MaxStorageBuffersPerShaderStage {
		t.Errorf("report = %+v, want 9 declared against the floor of %d", exceeded, DefaultLimits().MaxStorageBuffersPerShaderStage)
	}
	if exceeded.Device != backend.Limits().MaxStorageBuffersPerShaderStage {
		t.Errorf("report device limit = %d, want the backend's %d", exceeded.Device, backend.Limits().MaxStorageBuffersPerShaderStage)
	}
}

func TestCheckWebLimitsMeasuresAgainstTheFloorNotTheDevice(t *testing.T) {
	// A desktop adapter reports far more than the web floor, so a check against
	// the device would pass a shader no browser can run.
	device := types.Limits{MaxStorageBuffersPerShaderStage: 200, MaxBindGroups: 8, MaxUniformBufferBindingSize: 1 << 20}
	within := shader.ShaderLayout{
		Resources: []shader.ShaderResource{
			{Name: "params", Kind: shader.ResourceUniformBuffer, Group: 0, Binding: 0, Size: 256},
			{Kind: shader.ResourceStorageBuffer, Group: 1},
		},
	}
	if err := checkWebLimits("canvas.sprite", within, device); err != nil {
		t.Errorf("a shader within the floor was rejected: %v", err)
	}

	groups := shader.ShaderLayout{Resources: []shader.ShaderResource{{Group: 7}}}
	if err := checkWebLimits("scene.pbr", groups, device); err == nil {
		t.Error("eight bind groups were accepted, want an error")
	}

	uniform := shader.ShaderLayout{
		Resources: []shader.ShaderResource{{Name: "params", Kind: shader.ResourceUniformBuffer, Group: 0, Binding: 0, Size: DefaultLimits().MaxUniformBufferBindingSize + 1}},
	}
	if err := checkWebLimits("scene.pbr", uniform, device); err == nil {
		t.Error("an oversized uniform block was accepted, want an error")
	}

	// Twelve uniform blocks is the floor; the thirteenth is past it.
	blocks := shader.ShaderLayout{}
	for i := range DefaultLimits().MaxUniformBuffersPerShaderStage + 1 {
		blocks.Resources = append(blocks.Resources, shader.ShaderResource{Kind: shader.ResourceUniformBuffer, Binding: i, Size: 16})
	}
	var exceeded shader.ErrShaderExceedsWebLimits
	if err := checkWebLimits("scene.pbr", blocks, device); !errors.As(err, &exceeded) || exceeded.Declared != 13 || exceeded.Floor != 12 {
		t.Errorf("thirteen uniform blocks = %v, want 13 declared against the floor of 12", err)
	}
	blocks.Resources = blocks.Resources[:12]
	if err := checkWebLimits("scene.pbr", blocks, device); err != nil {
		t.Errorf("twelve uniform blocks were rejected: %v", err)
	}
}

func TestBufferRangeParamBindsItsOwnSlice(t *testing.T) {
	p := newPlugin()
	k := newTestKernel(t, p)
	backend := &fakeBackend{layout: &shader.ShaderLayout{
		Resources: []shader.ShaderResource{
			{Name: "params", Kind: shader.ResourceUniformBuffer, Group: 0, Binding: 0, Size: 64, Members: []shader.StorageMember{{Name: "mvp", Offset: 0}}},
			{Name: "records", Kind: shader.ResourceStorageBuffer, Group: 1, Binding: 0},
		},
	}}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	var records descriptors.BufferDescr
	withResourceQueue(t, k, func(resources *ResourceQueue) {
		records = resources.BakeBuffer(make([]byte, 1024), true)
	})
	w := recordList(t, k)
	w.Draw(triangle(), testMaterial(descriptors.BufferRangeParam("records", records, 256, 512)), descriptors.MatParam("mvp", m.NewMat4()))
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	found := false
	for _, op := range backend.lastOps {
		if op.kind != testOpSetBuffer {
			continue
		}
		found = true
		if op.offset != 256 || op.size != 512 {
			t.Errorf("buffer binding = (offset %d, size %d), want (256, 512)", op.offset, op.size)
		}
	}
	if !found {
		t.Fatal("no storage buffer binding was recorded")
	}
}

func TestFirstInstanceReachesTheDraw(t *testing.T) {
	p := newPlugin()
	k := newTestKernel(t, p)
	backend := &fakeBackend{}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	w := recordList(t, k)
	// A batch reads its own slice of the shared instance arena: WebGPU's
	// instance_index starts at firstInstance, so no offset plumbing is needed.
	w.DrawInstancedFrom(triangle(), testMaterial(), 7, 3, descriptors.MatParam("mvp", m.NewMat4()))
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if len(backend.draws) != 1 {
		t.Fatalf("draws = %d, want 1", len(backend.draws))
	}
	if got := backend.draws[0]; got.instances != 3 || got.firstInstance != 7 {
		t.Errorf("draw = %+v, want 3 instances starting at 7", got)
	}
}

func TestUniformBlockOverTheSlotIsReportedOnceAndDropped(t *testing.T) {
	p := newPlugin()
	var reported []error
	k := newTestKernelWithErrors(t, p, func(err error) { reported = append(reported, err) })
	layout := shader.ShaderLayout{
		Resources: []shader.ShaderResource{
			{Name: "params", Kind: shader.ResourceUniformBuffer, Group: 0, Binding: 0, Size: 64, Members: []shader.StorageMember{{Name: "mvp", Offset: 0}}},
			{Name: "lights", Kind: shader.ResourceUniformBuffer, Group: 0, Binding: 1, Size: uniformMax + 1},
		},
	}
	backend := &fakeBackend{layout: &layout}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	for range 2 {
		w := recordList(t, k)
		w.Draw(triangle(), testMaterial(), descriptors.MatParam("mvp", m.NewMat4()))
		k.ExecuteCommand[PresentCmd](PresentRequest{})
		k.PublishEvent(app.RenderEvent{}).Wait()
	}

	// Rendering the block cut to the slot is the silent wrong output this
	// check exists to remove, so the shader is refused rather than warned about.
	if backend.passDraws[0] != 0 {
		t.Errorf("draws = %d, want the draw dropped", backend.passDraws[0])
	}
	if len(backend.freedShaders) != 1 {
		t.Errorf("freed shaders = %d, want the refused module freed", len(backend.freedShaders))
	}
	var tooLarge shader.ErrUniformBlockTooLarge
	found := 0
	for _, err := range reported {
		if errors.As(err, &tooLarge) {
			found++
		}
	}
	if found != 1 {
		t.Fatalf("reports = %d over two frames, want exactly 1: %v", found, reported)
	}
	if tooLarge.Block != "lights" || tooLarge.Declared != 257 || tooLarge.Max != 256 {
		t.Errorf("report = %+v, want lights' 257 declared against 256", tooLarge)
	}
}

func TestUniformBlockThatFillsTheSlotRenders(t *testing.T) {
	p := newPlugin()
	var reported []error
	k := newTestKernelWithErrors(t, p, func(err error) { reported = append(reported, err) })
	backend := &fakeBackend{layout: &shader.ShaderLayout{
		Resources: []shader.ShaderResource{{Name: "params", Kind: shader.ResourceUniformBuffer, Group: 0, Binding: 0, Size: uniformMax, Members: []shader.StorageMember{{Name: "mvp", Offset: 0}}}},
	}}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	w := recordList(t, k)
	w.Draw(triangle(), testMaterial(), descriptors.MatParam("mvp", m.NewMat4()))
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if backend.passDraws[0] != 1 {
		t.Errorf("draws = %d, want a 256-byte block rendered", backend.passDraws[0])
	}
	var tooLarge shader.ErrUniformBlockTooLarge
	for _, err := range reported {
		if errors.As(err, &tooLarge) {
			t.Errorf("a 256-byte block was reported: %v", err)
		}
	}
}
