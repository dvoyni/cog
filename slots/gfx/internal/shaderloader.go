package internal

import (
	"io/fs"

	"github.com/dvoyni/cog/slots/gfx/internal/types"

	"github.com/dvoyni/cog/slots/gfx/internal/shader"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/assets"
)

// loadedShader is the shader cache's value: one module, as a load left it. It
// was cachedShader while the table lived in a translator field, and "cached"
// described where it lived; it is not plain shader because that is the name of
// the package the shader vocabulary lives in.
//
// It holds the backend id when the module compiled, the error when it did not,
// the sources it was built from, which is what eviction reads, and the label
// every report about the module names it by.
//
// A failed shader is cached as failed, on the same key. Without that, the next
// frame re-reads every source, re-flattens, re-fails and re-reports - at the
// frame rate.
//
// It is stored by pointer, and the texture beside it by value. That is not an
// inconsistency and is worth recording so the next reader does not "fix" it:
// nothing mutates a texture after its load, while report() sets reported on
// every hit, and a value stored in a map cannot be mutated in place.
type loadedShader struct {
	id      types.ShaderID
	err     error
	sources []string
	// label is ShaderLabel of the descriptor, spelled once here because
	// a supplied shader's label is a new string: built per draw, it was the
	// frame's one allocation on every draw through a supply.
	label    string
	reported bool
}

// report returns the entry's error the first time it is asked and nothing
// afterwards: a condition true every frame is worth saying once. The caller
// drops the draw on a zero id rather than on the error, so silence never lets a
// bad draw through.
//
// This is where the two questions the migration had to answer together land.
// A compile failure is **returned**, not reported from inside Load - it travels
// back through ensureShader to renderOnRender's firstErr, which holds the only
// Kernel gfx's render thread ever has - and so the entry keeps its own flag.
// Reporting it from Load with kernel.ReportErrorOnce would make the flag dead
// weight and the value storable by value; doing both would say it twice. The
// kernel's own doc carves this case out by name: a render-thread object with no
// Kernel hands its error back to whoever does hold one, so what it dedupes is
// its own return value rather than a report.
//
// Only the read failure moved. The Library performs the root read, so it is the
// Library that reports a shader whose file is not there - which is why gfx no
// longer has an error type for one.
//
// The translator's other three gates - the failed-pipeline entry,
// badIndexLengths and unsuppliedBuffers - are the same mechanism for the same
// reason: firstErr carries one error per frame, so a condition that re-reported
// every frame would mask every later error in every later frame.
func (s *loadedShader) report() error {
	if s.err == nil || s.reported {
		return nil
	}
	s.reported = true
	return s.err
}

// shaderUserData is what the shader loader needs that only the render handler
// holds. It is U, the cache's opaque pass-through, so it travels per call and
// the loader stores none of it.
//
// It carries the whole translator because a shader load and a shader release
// both reach further into it than a texture's do: Load writes t.layouts through
// shaderLayout and may set t.diagnostic, and Free sweeps t.pipelines and
// t.parameterPlans for the dead id.
type shaderUserData struct {
	t       *translator
	backend Backend
	// root is the storage path of the module being loaded, and empty for inline
	// text or for a call that frees rather than loads.
	//
	// It rides here because Load is handed the bytes and the Params but not the
	// Name, and a shader's root name is load-bearing in a way a texture's is
	// not: relative includes resolve against its directory, every source
	// location is spelled with it, and it is the first entry of the include set
	// eviction scans. Free needs none of that, which is why a free site leaves
	// it empty rather than inventing one.
	root string
}

// shaderLoader flattens a module and compiles it. It is stateless and is built
// once with the translator, which is what lets everything lock-bound arrive per
// call.
type shaderLoader struct{}

// Load flattens the root the Library read - or the inline text the descriptor
// carried - together with its includes, and hands the result to the backend.
//
// The kernel is unused: every failure here is gfx's own and is returned through
// the entry rather than reported, which is the decision report() records.
func (shaderLoader) Load(
	_ kernel.Kernel, data assets.Blob, params shader.ShaderDescrParams, fsys fs.FS, userData shaderUserData,
) *loadedShader {
	descr := shader.ShaderDescr{Name: userData.root, Blob: data, Params: params}
	label := shader.ShaderLabel(descr)
	// Flatten happens here, on the render thread, on a cache miss only - the
	// first draw of a given (root, supply). The cost changes from one file read
	// to N, which is the same shape as today's hitch rather than a new class of
	// problem: if it ever bites, it bites the first frame a material appears,
	// which is already true.
	flattened, err := shader.FlattenShader(fsys, descr)
	// The include set is recorded on failure as well as on success, because a
	// failed entry must evict like any other: without it a release naming one of
	// the sources would clear every module that compiled and leave the one that
	// did not behind.
	value := &loadedShader{sources: flattened.Sources, label: label}
	if err != nil {
		value.err = err
		return value
	}
	id, err := userData.backend.NewShader(shader.ShaderDesc{Code: []byte(flattened.Text), Label: label})
	if err != nil {
		// Nothing the backend said is rewritten and no line number is parsed out
		// of its message: gfx appends the rendered segment table and lets the
		// reader subtract.
		value.err = shader.CompileError(label, err, flattened.SourceMap)
		return value
	}
	// Every shader gfx reflects is measured, not only an engine's bundled ones:
	// a caller-supplied material is what actually gets bound at draw time. The
	// shader is cached, so this reports once rather than once a frame.
	layout := userData.t.shaderLayout(userData.backend, id)
	value.id = id
	limits := userData.backend.Limits()
	if diagnostic := checkWebLimits(label, layout, limits); diagnostic != nil && userData.t.diagnostic == nil {
		userData.t.diagnostic = diagnostic
	}
	return value
}

// Default is the module a read failure leaves behind: no id, so the draw is
// dropped, and no error, because the Library has already reported the missing
// file under this descriptor and gfx would only be saying it twice.
//
// The root path is still recorded as the entry's one source, so a failed module
// evicts by path exactly as a compiled one does.
func (shaderLoader) Default(d assets.Descr[shader.ShaderDescrParams], _ shaderUserData) *loadedShader {
	return &loadedShader{sources: []string{d.Name}, label: shader.ShaderLabel(shader.ShaderDescr(d))}
}

// Free releases the module and everything the translator derived from it. This
// cascade is the release half of what Load built - the pipelines keyed on the
// dead ShaderID, the parameter plans behind them and the reflected layout - so
// it belongs to the loader rather than beside the table.
//
// It is also why freeCachedResources clears pipelines and plans before it frees
// the entries: run per entry over full maps, this is O(shaders x pipelines).
func (shaderLoader) Free(value *loadedShader, userData shaderUserData) {
	if value.id == 0 {
		return
	}
	t := userData.t
	for key, pipeline := range t.pipelines {
		if key.shader != value.id {
			continue
		}
		// A zero entry is the marker for a pipeline that failed to build, not
		// a resource: there is nothing to hand back.
		if pipeline != 0 {
			userData.backend.FreePipeline(pipeline)
		}
		delete(t.pipelines, key)
	}
	for key := range t.parameterPlans {
		if key.shader == value.id {
			delete(t.parameterPlans, key)
		}
	}
	delete(t.layouts, value.id)
	userData.backend.FreeShader(value.id)
}
