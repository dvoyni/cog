package plugin

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/render"
)

// Handlers is the resource holding the event handler the plugin subscribes,
// so a plugin listed after it orders its own around it with
// configurator.OrderSubscriptions.
type Handlers struct {
	// DeclareBindings handles DeclareEvent: it declares the bindings of the
	// shaders the plugin's Config names.
	DeclareBindings kernel.Sub[DeclareEvent]
}

// declarationRequest is the private resource holding what Config asks the
// plugin to declare: the shaders, the define sets each has a variant for,
// and where each declaration is handed.
type declarationRequest struct {
	shaderPaths     []string
	defineSets      [][]render.ShaderDefine
	takeDeclaration func(shaderPath string, declaration []byte)
}
