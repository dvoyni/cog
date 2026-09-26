package internal

import (
	"io/fs"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/gfx"
)

// bundledShader is one variant of the bundled shader as the Lookup holds it:
// not tried yet, compiled and uploaded under id, or failed to compile.
type bundledShader struct {
	id    gfx.ShaderID
	state bundledShaderState
}

type bundledShaderState uint8

const (
	bundledUntried bundledShaderState = iota
	bundledReady
	bundledFailed
)

// bundledShaderReport is the report-once key a variant that did not compile
// is spoken under: model's own type, so one broken variant is one report for
// the engine's life however many models load after it.
type bundledShaderReport struct{ variant ShaderVariant }

// ensureShaders compiles and uploads the bundled shader's four variants the
// first time a load has a compiler, and returns the four forever after. Every
// model's draw params are built on these: the programs are the Lookup's, not a
// model's, and live as long as it does, as the two default textures do.
//
// It runs inside a load, on the loading System's goroutine. CompileShaderCmd's
// lock is empty, so declaring it costs that System nothing, and a steady-state
// load finds all four here and compiles nothing.
//
// A variant that does not compile is reported once and remembered as failed,
// so it is not compiled again a load; a model's set for it stays zero and its
// Forward material draws as before. A nil compile tries nothing and remembers
// nothing, so a later load that has one still compiles.
func (l *Lookup) ensureShaders(
	k kernel.Kernel, fsys fs.FS, resources *gfx.ResourceQueue, compile gfx.ShaderCompiler,
) *[VariantCount]bundledShader {
	if compile == nil {
		return &l.shaders
	}
	for variant := range l.shaders {
		shader := &l.shaders[variant]
		if shader.state != bundledUntried {
			continue
		}
		response := compile(k, gfx.CompileShaderRequest{FS: fsys, Descr: ShaderVariant(variant).shader()})
		if response.Err != nil {
			shader.state = bundledFailed
			k.ReportErrorOnce(bundledShaderReport{variant: ShaderVariant(variant)},
				ErrSceneShaderUnavailable{Variant: ShaderVariant(variant), Err: response.Err})
			continue
		}
		shader.id = resources.NewShader()
		resources.UploadProgram(k, shader.id, response.Program)
		shader.state = bundledReady
	}
	return &l.shaders
}

// newMaterialSets creates one material's durable draw params, one set per
// variant whose shader is ready: the ten texture and sampler params that lead
// its ingredients, and its numbers as ScenePbrMaterial whole.
//
// The numbers are borrowed rather than copied - they are 160 bytes, past what a
// param carries inline - and NewDrawParams copies them before it returns.
func (l *Lookup) newMaterialSets(
	k kernel.Kernel, resources *gfx.ResourceQueue, shaders *[VariantCount]bundledShader,
	built *modelMaterial, values *PbrValues,
) (sets [VariantCount]gfx.DrawParams) {
	params := append(l.setParams[:0], built.Params[:2*pbrSlotCount]...)
	params = append(params, gfx.RawParameterRef(BindingScenePbrMaterial, values))
	for variant := range sets {
		if shaders[variant].state == bundledReady {
			sets[variant] = resources.NewDrawParams(k, shaders[variant].id, built.State, params...)
		}
	}
	// The scratch is kept for the next material, and cleared so it holds no
	// texture or borrowed record past the load.
	clear(params)
	l.setParams = params[:0]
	return sets
}

// releaseSets queues a freed model's draw params for release at the frame
// boundary. A free holds no resource queue - UnloadModel is on the facade that
// needs no device - so the sets wait beside the buffers for the drain.
func (l *Lookup) releaseSets(model *residentModel) {
	for i := range model.Materials {
		for _, set := range model.Materials[i].Sets {
			if set != (gfx.DrawParams{}) {
				l.pendingSets = append(l.pendingSets, set)
			}
		}
	}
}
