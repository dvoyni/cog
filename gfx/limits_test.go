package gfx

import (
	"errors"
	"testing"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/m"
)

func TestDefaultLimitsAreTheBrowserFloor(t *testing.T) {
	// These are the WebGPU spec floor, not any device's numbers: a desktop
	// adapter reports hardware limits, and checking against those passes a build
	// that cannot run in a browser.
	want := Limits{
		MaxBindGroups:                   4,
		MaxStorageBuffersPerShaderStage: 8,
		MaxStorageBufferBindingSize:     128 << 20,
		MaxUniformBufferBindingSize:     64 << 10,
		MaxBufferSize:                   256 << 20,
	}
	if DefaultLimits != want {
		t.Errorf("DefaultLimits = %+v, want the web floor %+v", DefaultLimits, want)
	}
}

func TestShaderOverTheWebFloorIsReportedOnceAndStillRenders(t *testing.T) {
	p := New()
	var reported []error
	k := newTestKernelWithErrors(t, p, func(err error) { reported = append(reported, err) })
	layout := ShaderLayout{
		UniformSize: 64, UniformGroup: 0, UniformBinding: 0,
		Uniforms: []UniformMember{{Name: "mvp", Offset: 0}},
	}
	for i := range 9 {
		layout.Resources = append(layout.Resources, ShaderResource{
			Name: "records", StorageBuffer: true, Group: 1, Binding: i,
		})
	}
	backend := &fakeBackend{layout: &layout}
	k.ExecuteCommand[SetBackendCmd](SetBackendRequest{Backend: backend})

	for range 2 {
		w := recordList(t, k)
		w.Draw(triangle(), testMaterial(), MatParam("mvp", m.NewMat4()))
		k.ExecuteCommand[PresentCmd](PresentRequest{})
		k.PublishEvent(app.RenderEvent{}).Wait()
	}

	// The shader runs here: the diagnostic is about a browser refusing it, so
	// dropping the draw would break a desktop build over a portability warning.
	if backend.passDraws[0] != 1 {
		t.Errorf("draws = %d, want the draw rendered anyway", backend.passDraws[0])
	}
	var exceeded ErrShaderExceedsWebLimits
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
	if exceeded.Declared != 9 || exceeded.Floor != DefaultLimits.MaxStorageBuffersPerShaderStage {
		t.Errorf("report = %+v, want 9 declared against the floor of %d", exceeded, DefaultLimits.MaxStorageBuffersPerShaderStage)
	}
	if exceeded.Device != backend.Limits().MaxStorageBuffersPerShaderStage {
		t.Errorf("report device limit = %d, want the backend's %d", exceeded.Device, backend.Limits().MaxStorageBuffersPerShaderStage)
	}
}

func TestCheckWebLimitsMeasuresAgainstTheFloorNotTheDevice(t *testing.T) {
	// A desktop adapter reports far more than the web floor, so a check against
	// the device would pass a shader no browser can run.
	device := Limits{MaxStorageBuffersPerShaderStage: 200, MaxBindGroups: 8, MaxUniformBufferBindingSize: 1 << 20}
	within := ShaderLayout{UniformSize: 256, Resources: []ShaderResource{{StorageBuffer: true, Group: 1}}}
	if err := checkWebLimits("canvas.sprite", within, device); err != nil {
		t.Errorf("a shader within the floor was rejected: %v", err)
	}

	groups := ShaderLayout{Resources: []ShaderResource{{Group: 7}}}
	if err := checkWebLimits("scene.pbr", groups, device); err == nil {
		t.Error("eight bind groups were accepted, want an error")
	}

	uniform := ShaderLayout{UniformSize: DefaultLimits.MaxUniformBufferBindingSize + 1}
	if err := checkWebLimits("scene.pbr", uniform, device); err == nil {
		t.Error("an oversized uniform block was accepted, want an error")
	}
}

func TestBufferRangeParamBindsItsOwnSlice(t *testing.T) {
	p := New()
	k := newTestKernel(t, p)
	backend := &fakeBackend{layout: &ShaderLayout{
		UniformSize: 64, UniformGroup: 0, UniformBinding: 0,
		Uniforms:  []UniformMember{{Name: "mvp", Offset: 0}},
		Resources: []ShaderResource{{Name: "records", StorageBuffer: true, Group: 1, Binding: 0}},
	}}
	k.ExecuteCommand[SetBackendCmd](SetBackendRequest{Backend: backend})

	var records BufferDescr
	withResourceQueue(t, k, func(resources *ResourceQueue) {
		records = resources.BakeBuffer(make([]byte, 1024), true)
	})
	w := recordList(t, k)
	w.Draw(triangle(), testMaterial(BufferRangeParam("records", records, 256, 512)), MatParam("mvp", m.NewMat4()))
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	found := false
	for _, op := range backend.lastOps {
		if op.kind != gpuSetBakedBuffer {
			continue
		}
		found = true
		if int(op.arg0) != 256 || int(op.arg1) != 512 {
			t.Errorf("buffer binding = (offset %d, size %d), want (256, 512)", op.arg0, op.arg1)
		}
	}
	if !found {
		t.Fatal("no storage buffer binding was recorded")
	}
}

func TestFirstInstanceReachesTheDraw(t *testing.T) {
	p := New()
	k := newTestKernel(t, p)
	backend := &fakeBackend{}
	k.ExecuteCommand[SetBackendCmd](SetBackendRequest{Backend: backend})

	w := recordList(t, k)
	// A batch reads its own slice of the shared instance arena: WebGPU's
	// instance_index starts at firstInstance, so no offset plumbing is needed.
	w.DrawInstancedFrom(triangle(), testMaterial(), 7, 3, MatParam("mvp", m.NewMat4()))
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if len(backend.draws) != 1 {
		t.Fatalf("draws = %d, want 1", len(backend.draws))
	}
	if got := backend.draws[0]; got.instances != 3 || got.firstInstance != 7 {
		t.Errorf("draw = %+v, want 3 instances starting at 7", got)
	}
}
