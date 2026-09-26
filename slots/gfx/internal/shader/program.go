package shader

import (
	"io/fs"
	"slices"
	"strings"

	"github.com/dvoyni/cog/libs/assets"
)

// Reflect is the reflection port as the compile step sees it: a pure function
// of a flattened module's source that returns its bindings. The backend fills
// it - gogpu with naga - because gfx cannot import a shader compiler, and a
// backend-neutral slot should not.
type Reflect func(code []byte) (ShaderLayout, error)

// ShaderProgram is what CompileShader makes of one shader descriptor: the
// flattened module, the map its errors are read against, its reflected layout
// and the binding table built from it by WGSL global name. It is pure data - no
// GPU object stands behind it until ResourceQueue.UploadProgram hands it to a
// shader - and it is immutable, so it is passed by value and shared freely.
//
// The zero value is no program. It is what a failed compile returns beside its
// error, and UploadProgram reports it rather than creating an empty module.
type ShaderProgram struct {
	p *program
}

// program is a ShaderProgram's body. It sits behind a pointer so the handle is
// one word however large the layout, and so a copy can never be edited apart
// from the table the shader it was uploaded to resolves against.
type program struct {
	label     string
	code      []byte
	sourceMap ShaderSourceMap
	sources   []string
	layout    ShaderLayout
	// bindings is layout.Resources sorted by name, which is the table a param
	// is resolved against. A WGSL module cannot declare one global twice, so
	// the name is a key without any check of its own.
	bindings []ShaderResource
}

// CompileShader flattens descr through the preprocessor, reflects the result
// through reflect, and builds the binding table by name.
//
// It is pure: it reads the root and its includes from fsys and touches nothing
// of gfx's, which is what lets CompileShaderCmd hold no lock. Its failures -
// a root that is not there, an include that does not resolve, WGSL the
// reflection refuses - are normal outcomes, so they are returned rather than
// reported, and the program beside them is the zero one.
func CompileShader(fsys fs.FS, descr ShaderDescr, reflect Reflect) (ShaderProgram, error) {
	label := ShaderLabel(descr)
	// The root read is this call's own. In the descriptor path the asset
	// Library performs it and reports a missing file itself; here there is no
	// Library, and the caller decides what a missing shader means.
	if descr.Name != "" {
		if fsys == nil {
			return ShaderProgram{}, ErrShaderSource{Shader: label, Message: "no filesystem to read the root from"}
		}
		text, err := fs.ReadFile(fsys, descr.Name)
		if err != nil {
			return ShaderProgram{}, ErrShaderSource{Shader: label, Message: "cannot read the root source", Err: err}
		}
		descr.Blob = assets.NewBlob(text)
	}
	flattened, err := FlattenShader(fsys, descr)
	if err != nil {
		return ShaderProgram{}, err
	}
	code := []byte(flattened.Text)
	layout, err := reflect(code)
	if err != nil {
		return ShaderProgram{}, CompileError(label, err, flattened.SourceMap)
	}
	bindings := slices.Clone(layout.Resources)
	slices.SortFunc(bindings, func(a, b ShaderResource) int { return strings.Compare(a.Name, b.Name) })
	return ShaderProgram{p: &program{
		label: label, code: code, sourceMap: flattened.SourceMap, sources: flattened.Sources,
		layout: layout, bindings: bindings,
	}}, nil
}

// Valid reports whether the program is one CompileShader built, rather than
// the zero value a failed compile left behind.
func (p ShaderProgram) Valid() bool { return p.p != nil }

// Label names the program in a report: its root path, or gfx.shader for inline
// text, followed by its supply.
func (p ShaderProgram) Label() string {
	if p.p == nil {
		return ""
	}
	return p.p.label
}

// Binding returns the binding the program declares under a WGSL global name,
// and whether it declares one.
func (p ShaderProgram) Binding(name string) (ShaderResource, bool) {
	if p.p == nil {
		return ShaderResource{}, false
	}
	i, ok := slices.BinarySearchFunc(p.p.bindings, name, func(r ShaderResource, name string) int {
		return strings.Compare(r.Name, name)
	})
	if !ok {
		return ShaderResource{}, false
	}
	return p.p.bindings[i], true
}

// Sources is every storage path the flatten read, root first, which is what a
// caller reloading the shader watches.
func (p ShaderProgram) Sources() []string {
	if p.p == nil {
		return nil
	}
	return slices.Clone(p.p.sources)
}

// The friend functions: what gfx's internal/ reads from a program. The
// flattened text in particular is not offered publicly, for the reason
// ShaderDescr does not offer its own: nothing outside gfx can do with it what
// the backend already does.

// ProgramDesc is the module a program creates: its flattened code and label.
func ProgramDesc(p ShaderProgram) ShaderDesc {
	if p.p == nil {
		return ShaderDesc{}
	}
	return ShaderDesc{Code: p.p.code, Label: p.p.label}
}

// ProgramSourceMap is the map a backend's refusal of the module is read against.
func ProgramSourceMap(p ShaderProgram) ShaderSourceMap {
	if p.p == nil {
		return ShaderSourceMap{}
	}
	return p.p.sourceMap
}

// ProgramLayout is the program's reflected layout, which the translator checks
// against the web floor and a mesh's vertex layout.
func ProgramLayout(p ShaderProgram) ShaderLayout {
	if p.p == nil {
		return ShaderLayout{}
	}
	return p.p.layout
}
