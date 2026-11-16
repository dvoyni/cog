package rendertest

import (
	"github.com/dvoyni/cog/plugins/gfx"
	"github.com/dvoyni/cog/plugins/render/internal/bindingdeclaration"
)

// harnessDevice is a recording device, or gogpu's on the software adapter,
// as the harness wraps it. Its GPU finishes the work submitted to it only
// when the harness's next Step begins, as a GPU a frame behind would, or
// later on the software adapter: a frame submitted in one beat is seen
// complete in the next, so FrameEvent's GPULatency reads FrameInterval. On
// the recording adapter it answers a module's named, entry-point and
// vertex-input reflection from the binding declaration beside its shader,
// unless an answer queued for the call waits; on the software adapter it
// answers the adapter's, and checks the declaration against it.
type harnessDevice struct {
	gfx.BackendDevice
	harness *Harness
}

// Submit submits buffers to the wrapped device, and keeps the index it
// answers as the newest submitted.
func (d harnessDevice) Submit(buffers ...gfx.CommandBuffer) (uint64, error) {
	index, err := d.BackendDevice.Submit(buffers...)
	if err == nil {
		d.harness.submittedIndex.Store(index)
	}
	return index, err
}

// Poll answers the newest submission the wrapped device finished, but none
// after the newest the harness's last Step found submitted.
func (d harnessDevice) Poll() uint64 {
	return min(d.BackendDevice.Poll(), d.harness.finishedIndex.Load())
}

// CreateShaderModule makes module on the wrapped device from the text the
// harness preprocessed, without the comment it appended, and notes it made
// from that shader, failing the test when the shader has no binding
// declaration or, on the software adapter, one that differs from what the
// adapter reflects of module.
func (d harnessDevice) CreateShaderModule(module gfx.ShaderModule, descriptor gfx.ShaderModuleDescriptor) error {
	shader, preprocessed := d.harness.declarations.findPreprocessed(descriptor.WGSL)
	if preprocessed {
		descriptor.WGSL = shader.text
	}
	if err := d.BackendDevice.CreateShaderModule(module, descriptor); err != nil {
		return err
	}
	d.harness.declarations.noteCompiled(module, descriptor.Label, shader, preprocessed)
	if d.harness.softwareAdapter && preprocessed && shader.err == nil {
		reflection, err := bindingdeclaration.Reflect(
			func() ([]gfx.ShaderBinding, error) { return d.BackendDevice.ReflectBindings(module) },
			func() ([]gfx.ShaderEntryPoint, error) { return d.BackendDevice.ReflectEntryPoints(module) },
			func(entryPoint string) ([]gfx.VertexInput, error) {
				return d.BackendDevice.ReflectVertexInputs(module, entryPoint)
			},
		)
		if err != nil {
			d.harness.t.Errorf("rendertest: reflecting %s on the software adapter: %v", shader.name, err)
		} else if difference := bindingdeclaration.FindDifference(shader.declaration, reflection); difference != "" {
			d.harness.t.Errorf("rendertest: the binding declaration %s.json is stale for %s: %s; generate it again with declarebindings", shader.path, shader.name, difference)
		}
	}
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

// ReflectEntryPoints answers the entry points module's declaration declares.
func (d harnessDevice) ReflectEntryPoints(module gfx.ShaderModule) ([]gfx.ShaderEntryPoint, error) {
	return d.harness.declarations.answerEntryPoints(module, func() ([]gfx.ShaderEntryPoint, error) {
		return d.BackendDevice.ReflectEntryPoints(module)
	})
}
