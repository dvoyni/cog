package bindingdeclaration

import (
	"fmt"

	"github.com/dvoyni/cog/gfx"
)

// shaderStages are the stages an entry point runs in, by WGSL's spelling of
// its attribute.
var shaderStages = map[string]gfx.ShaderStageFlags{
	"vertex":   gfx.ShaderStageFlagVertex,
	"fragment": gfx.ShaderStageFlagFragment,
	"compute":  gfx.ShaderStageFlagCompute,
}

// declaredEntryPoint is one entry point a shader declares: its name, and the
// stage it runs in, vertex, fragment or compute.
type declaredEntryPoint struct {
	Name  string `json:"name"`
	Stage string `json:"stage"`
}

// makeDeclaredEntryPoint answers entryPoint as a declaration spells it, or
// the error naming a stage no name spells, such as several stages at once.
func makeDeclaredEntryPoint(entryPoint gfx.ShaderEntryPoint) (declaredEntryPoint, error) {
	stage, err := findName(shaderStages, "stage", entryPoint.Stage)
	if err != nil {
		return declaredEntryPoint{}, fmt.Errorf("entry point %q: %w", entryPoint.Name, err)
	}
	return declaredEntryPoint{Name: entryPoint.Name, Stage: stage}, nil
}

// makeShaderEntryPoint answers e as gfx reflects it.
func (e declaredEntryPoint) makeShaderEntryPoint() (gfx.ShaderEntryPoint, error) {
	stage, err := findNamed(shaderStages, "stage", e.Stage)
	if err != nil {
		return gfx.ShaderEntryPoint{}, fmt.Errorf("entry point %q: %w", e.Name, err)
	}
	return gfx.ShaderEntryPoint{Name: e.Name, Stage: stage}, nil
}
