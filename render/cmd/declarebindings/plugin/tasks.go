package plugin

import (
	"fmt"
	"io/fs"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/platform"
	"github.com/dvoyni/cog/render"
	"github.com/dvoyni/cog/render/internal/bindingdeclaration"
)

// declareShaders declares the bindings of each shader request names, and
// hands each declaration to its takeDeclaration; a shader it cannot declare
// is logged.
func declareShaders(k *kernel.Kernel, _ kernel.NoArg, request kernel.Read[declarationRequest], inventory render.Inventory, device kernel.Read[gfx.Device], files kernel.Read[platform.FS]) kernel.NoResult {
	for _, path := range request.Get().shaderPaths {
		declaration, err := declareShader(inventory, device.Get(), files.Get(), path, request.Get().defineSets)
		if err != nil {
			k.Logger().LogErr(fmt.Errorf("declarebindings: %s: %w", path, err))
			continue
		}
		request.Get().takeDeclaration(path, declaration)
	}
	return kernel.NoResult{}
}

// declareShader answers the declaration of the shader at path in files: what
// device reflects of it with no defines, and of a variant for each of
// defineSets.
func declareShader(inventory render.Inventory, device gfx.Device, files fs.FS, path string, defineSets [][]render.ShaderDefine) ([]byte, error) {
	baseVariant, err := reflectVariant(inventory, device, files, path, nil)
	if err != nil {
		return nil, err
	}
	variants := make([]bindingdeclaration.Variant, len(defineSets))
	for index, defines := range defineSets {
		if variants[index], err = reflectVariant(inventory, device, files, path, defines); err != nil {
			return nil, err
		}
	}
	return bindingdeclaration.Marshal(baseVariant.Reflection, variants)
}

// reflectVariant acquires the variant of the shader at path in files with
// defines through inventory, so render reports one it fails to compile or
// refuses, and answers it with what device reflects of it: its bindings, and
// each vertex entry point's inputs.
func reflectVariant(inventory render.Inventory, device gfx.Device, files fs.FS, path string, defines []render.ShaderDefine) (bindingdeclaration.Variant, error) {
	inventory.ReleaseShader(inventory.AcquireShader(path, defines...))
	variant := bindingdeclaration.Variant{Defines: make([]gfx.ShaderDefine, len(defines))}
	for index, define := range defines {
		variant.Defines[index] = gfx.ShaderDefine(define)
	}
	reflection, err := reflectShader(device, files, path, variant.Defines)
	if err != nil {
		return bindingdeclaration.Variant{}, fmt.Errorf("with defines %q: %w", variant.Defines, err)
	}
	variant.Reflection = reflection
	return variant, nil
}

// reflectShader answers what device reflects of the shader at path in files
// preprocessed with defines: its bindings, and each vertex entry point's
// inputs.
func reflectShader(device gfx.Device, files fs.FS, path string, defines []gfx.ShaderDefine) (bindingdeclaration.Reflection, error) {
	source, err := device.PreprocessShader(files, path, defines)
	if err != nil {
		return bindingdeclaration.Reflection{}, err
	}
	module, err := device.CreateShaderModule(gfx.ShaderModuleDescriptor{Label: "declarebindings: " + path, WGSL: source})
	if err != nil {
		return bindingdeclaration.Reflection{}, err
	}
	defer device.ReleaseShaderModule(module)
	return bindingdeclaration.Reflect(
		func() ([]gfx.ShaderBinding, error) { return device.ShaderModuleBindings(module) },
		func() ([]gfx.ShaderEntryPoint, error) { return device.ShaderModuleEntryPoints(module) },
		func(entryPoint string) ([]gfx.VertexInput, error) {
			return device.ShaderModuleVertexInputs(module, entryPoint)
		},
	)
}
