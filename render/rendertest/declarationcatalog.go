package rendertest

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"sync"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gfx/recording"
	"github.com/dvoyni/cog/render/internal/bindingdeclaration"
)

// declarationCatalog holds the binding declarations of the shaders the
// harness's device preprocessed and compiled, which answer their modules'
// reflection, gfx's recording adapter parsing no WGSL, or are checked
// against it on the software adapter.
type declarationCatalog struct {
	harness *Harness
	// mutex guards the maps, and is held across each reflection, so an
	// answer AnswerNext queued goes to the call that counted it.
	mutex sync.Mutex
	// preprocessed are the shaders PreprocessShader answered, by the text it
	// answered, and compiled those modules were made from, by module.
	preprocessed map[string]preprocessedShader
	compiled     map[gfx.ShaderModule]preprocessedShader
	// queuedAnswerCounts are how many answers queued with queueAnswer, by
	// kind, no call has taken yet.
	queuedAnswerCounts map[recording.CallKind]int
	// answeredBindings are the bindings each module's reflection answered,
	// its declaration's or a queued answer's.
	answeredBindings map[gfx.ShaderModule][]gfx.ShaderBinding
}

// newDeclarationCatalog answers the catalog of the shaders compiled on
// harness's instance, failing its test for a shader compiled without one.
func newDeclarationCatalog(harness *Harness) *declarationCatalog {
	return &declarationCatalog{
		harness:            harness,
		preprocessed:       map[string]preprocessedShader{},
		compiled:           map[gfx.ShaderModule]preprocessedShader{},
		queuedAnswerCounts: map[recording.CallKind]int{},
		answeredBindings:   map[gfx.ShaderModule][]gfx.ShaderBinding{},
	}
}

// queueAnswer makes the next call of kind the recording instance takes
// answer answer, a reflection's in place of its declaration's.
func (d *declarationCatalog) queueAnswer(kind recording.CallKind, answer recording.Answer) {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	d.queuedAnswerCounts[kind]++
	d.harness.instance.AnswerNext(kind, answer)
}

// notePreprocessed reads the declaration beside the shader at path in files
// for its variant with defines, and answers text, which the recording
// instance answered preprocessing it, with a comment appended naming the
// shader: a module made from the text answered is the shader's, whichever
// variants of it share their text.
func (d *declarationCatalog) notePreprocessed(files fs.FS, path string, defines []gfx.ShaderDefine, text string) string {
	declaration, err := bindingdeclaration.Read(files, path, defines)
	notedText := fmt.Sprintf("%s\n// rendertest: preprocessed %q with defines %q\n", text, path, defines)
	d.mutex.Lock()
	defer d.mutex.Unlock()
	name := path
	if len(defines) > 0 {
		name = fmt.Sprintf("%s with defines %q", path, defines)
	}
	d.preprocessed[notedText] = preprocessedShader{path: path, name: name, text: text, declaration: declaration, err: err}
	return notedText
}

// findPreprocessed answers the shader whose text notePreprocessed answered
// notedText, and whether there is one.
func (d *declarationCatalog) findPreprocessed(notedText string) (preprocessedShader, bool) {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	shader, ok := d.preprocessed[notedText]
	return shader, ok
}

// noteCompiled notes module as made from shader, which preprocessed tells
// whether the harness preprocessed, and fails the test when no declaration
// is known for it, naming it by its path or, unpreprocessed, by label.
func (d *declarationCatalog) noteCompiled(module gfx.ShaderModule, label string, shader preprocessedShader, preprocessed bool) {
	switch {
	case !preprocessed:
		d.harness.t.Errorf("rendertest: shader module %q was made from WGSL the harness did not preprocess, so it has no binding declaration", label)
		return
	case errors.Is(shader.err, fs.ErrNotExist):
		d.harness.t.Errorf("rendertest: %s was compiled with no binding declaration beside it, %s.json", shader.path, shader.path)
	case shader.err != nil:
		d.harness.t.Errorf("rendertest: the binding declaration %s.json: %v", shader.path, shader.err)
	}
	d.mutex.Lock()
	defer d.mutex.Unlock()
	d.compiled[module] = shader
}

// answerBindings answers what reflect, the recording device's reflection of
// module's bindings, answers when an answer queued for it waits or it fails,
// and the bindings module's declaration says otherwise, noting the answer.
func (d *declarationCatalog) answerBindings(module gfx.ShaderModule, reflect func() ([]gfx.ShaderBinding, error)) ([]gfx.ShaderBinding, error) {
	bindings, err := d.answer(recording.CallKindReflectBindings, module, reflect, func(shader preprocessedShader) ([]gfx.ShaderBinding, error) {
		return cloneBindings(shader.declaration.Bindings), nil
	})
	if err == nil {
		d.mutex.Lock()
		d.answeredBindings[module] = cloneBindings(bindings)
		d.mutex.Unlock()
	}
	return bindings, err
}

// findAnsweredBindings answers a copy of the bindings each module's
// reflection answered.
func (d *declarationCatalog) findAnsweredBindings() map[gfx.ShaderModule][]gfx.ShaderBinding {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	return maps.Clone(d.answeredBindings)
}

// answerEntryPoints answers as answerBindings does, for module's entry
// points.
func (d *declarationCatalog) answerEntryPoints(module gfx.ShaderModule, reflect func() ([]gfx.ShaderEntryPoint, error)) ([]gfx.ShaderEntryPoint, error) {
	return d.answer(recording.CallKindReflectEntryPoints, module, reflect, func(shader preprocessedShader) ([]gfx.ShaderEntryPoint, error) {
		return slices.Clone(shader.declaration.EntryPoints), nil
	})
}

// answerVertexInputs answers as answerBindings does, for the inputs of
// module's vertex entry point named entryPoint, an empty name its only one.
func (d *declarationCatalog) answerVertexInputs(module gfx.ShaderModule, entryPoint string, reflect func() ([]gfx.VertexInput, error)) ([]gfx.VertexInput, error) {
	return d.answer(recording.CallKindReflectVertexInputs, module, reflect, func(shader preprocessedShader) ([]gfx.VertexInput, error) {
		vertexInputs := shader.declaration.VertexInputs
		if entryPoint == "" {
			if len(vertexInputs) != 1 {
				return nil, fmt.Errorf("rendertest: %s declares %d vertex entry points, so no name picks none: want one", shader.path, len(vertexInputs))
			}
			for name := range vertexInputs {
				entryPoint = name
			}
		}
		inputs, ok := vertexInputs[entryPoint]
		if !ok {
			return nil, fmt.Errorf("rendertest: %s declares no vertex entry point %q", shader.path, entryPoint)
		}
		return slices.Clone(inputs), nil
	})
}

// answer answers a reflection of kind of module: what reflect, the wrapped
// device's, answers on the software adapter, when an answer queued for kind
// waits, when it fails, or when module has no declaration, and what
// answerDeclared answers of module's shader otherwise.
func (d *declarationCatalog) answer[T any](kind recording.CallKind, module gfx.ShaderModule, reflect func() (T, error), answerDeclared func(preprocessedShader) (T, error)) (T, error) {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	reflectedAnswer, err := reflect()
	shader, ok := d.compiled[module]
	if d.harness.softwareAdapter {
		ok = false
	} else if d.queuedAnswerCounts[kind] > 0 {
		d.queuedAnswerCounts[kind]--
		ok = false
	}
	if err != nil || !ok || shader.err != nil {
		return reflectedAnswer, err
	}
	return answerDeclared(shader)
}

// preprocessedShader is a shader the harness preprocessed: its path, its
// name, the path with its defines when it has some, as a failure names the
// variant, the text preprocessing answered for it, and what its declaration
// says of its variant, or why that is not known.
type preprocessedShader struct {
	path        string
	name        string
	text        string
	declaration bindingdeclaration.Reflection
	err         error
}

// cloneBindings answers a copy of bindings that shares no slice with it.
func cloneBindings(bindings []gfx.ShaderBinding) []gfx.ShaderBinding {
	bindings = slices.Clone(bindings)
	for index := range bindings {
		bindings[index].Members = slices.Clone(bindings[index].Members)
		bindings[index].EntryPoints = slices.Clone(bindings[index].EntryPoints)
	}
	return bindings
}
