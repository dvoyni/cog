package rendertest

import "github.com/dvoyni/cog/gfx"

// harnessDevice is a recording device as the harness wraps it. Its GPU
// finishes the work submitted to it only when the harness's next Step
// begins, as a GPU a frame behind would: a frame submitted in one beat is
// seen complete in the next, so FrameEvent's GPULatency reads
// FrameInterval. It answers a module's named and vertex-input reflection
// from the binding declaration beside its shader, unless an answer queued
// for the call waits.
type harnessDevice struct {
	gfx.BackendDevice
	harness *Harness
}

// Submit submits buffers to the recording device, and keeps the index it
// answers as the newest submitted.
func (d harnessDevice) Submit(buffers ...gfx.CommandBuffer) (uint64, error) {
	index, err := d.BackendDevice.Submit(buffers...)
	if err == nil {
		d.harness.submittedIndex.Store(index)
	}
	return index, err
}

// Poll answers the newest submission the recording device finished, but none
// after the newest the harness's last Step found submitted.
func (d harnessDevice) Poll() uint64 {
	return min(d.BackendDevice.Poll(), d.harness.finishedIndex.Load())
}

// CreateShaderModule makes module on the recording device from the text the
// harness preprocessed, without the comment it appended, and notes it made
// from that shader, failing the test when the shader has no binding
// declaration.
func (d harnessDevice) CreateShaderModule(module gfx.ShaderModule, descriptor gfx.ShaderModuleDescriptor) error {
	shader, preprocessed := d.harness.declarations.findPreprocessed(descriptor.WGSL)
	if preprocessed {
		descriptor.WGSL = shader.text
	}
	if err := d.BackendDevice.CreateShaderModule(module, descriptor); err != nil {
		return err
	}
	d.harness.declarations.noteCompiled(module, descriptor.Label, shader, preprocessed)
	return nil
}

// ReflectBindings answers the bindings module's declaration declares.
func (d harnessDevice) ReflectBindings(module gfx.ShaderModule) ([]gfx.ShaderBinding, error) {
	return d.harness.declarations.answerBindings(module, func() ([]gfx.ShaderBinding, error) {
		return d.BackendDevice.ReflectBindings(module)
	})
}

// ReflectVertexInputs answers the inputs module's declaration declares for
// its vertex entry point named entryPoint, an empty name its only one.
func (d harnessDevice) ReflectVertexInputs(module gfx.ShaderModule, entryPoint string) ([]gfx.VertexInput, error) {
	return d.harness.declarations.answerVertexInputs(module, entryPoint, func() ([]gfx.VertexInput, error) {
		return d.BackendDevice.ReflectVertexInputs(module, entryPoint)
	})
}
