package gfx

import (
	"github.com/dvoyni/cog/extensions/gfx/internal"
	"github.com/dvoyni/cog/extensions/storage"
)

// FlattenShader resolves one shader's sources into the single WGSL string a
// Backend compiles, and reports where every line of it came from.
//
// It is an exported plain function rather than a command because gfx cannot
// write the result anywhere: the render handler holds kernel.Read[storage.FileSystem],
// and writing needs kernel.Write, which would serialise rendering against every
// reader. Exporting it is a signature decision rather than extra work - the
// translator needs the function regardless - and it pays for itself twice. It is
// the test surface for the whole preprocessor, which otherwise could only be
// exercised through a backend, and what a developer dumps is byte-identical to
// what compiled, because it is the same call.
func FlattenShader(filesystem storage.FileSystem, descr ShaderDescr) (string, ShaderSourceMap, error) {
	flattened, err := internal.FlattenShader(filesystem, descr)
	return flattened.Text, flattened.SourceMap, err
}
