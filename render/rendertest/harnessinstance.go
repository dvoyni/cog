package rendertest

import (
	"io/fs"

	"github.com/dvoyni/cog/gfx"
)

// harnessInstance is the harness's recording instance as the harness wraps
// it: each adapter it answers is a harnessAdapter, and each shader it
// preprocesses is noted with the binding declaration beside it.
type harnessInstance struct {
	gfx.InstanceSocket
	harness *Harness
}

// RequestAdapter answers the recording instance's adapter, wrapped.
func (i harnessInstance) RequestAdapter(options gfx.RequestAdapterOptions) (gfx.BackendAdapter, error) {
	adapter, err := i.InstanceSocket.RequestAdapter(options)
	if err != nil {
		return nil, err
	}
	return harnessAdapter{BackendAdapter: adapter, harness: i.harness}, nil
}

// PreprocessShader answers the recording instance's text of the shader at
// path, with a comment appended naming the shader and its defines, so the
// module made from it answers its reflection from the declaration beside it.
func (i harnessInstance) PreprocessShader(files fs.FS, path string, defines []gfx.ShaderDefine) (string, error) {
	text, err := i.InstanceSocket.PreprocessShader(files, path, defines)
	if err != nil {
		return text, err
	}
	return i.harness.declarations.notePreprocessed(files, path, defines, text), nil
}
