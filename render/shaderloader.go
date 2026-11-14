package render

import (
	"fmt"
	"io/fs"

	"github.com/dvoyni/cog/gfx"
)

// shaderLoader shares, counts and frees the shaders render loads from files:
// one variant for each path and set of defines, compiled in the job that
// first acquires it, counted by the acquires holding it, and freed at the end
// of the frame its count reaches zero in. It compiles holding no mutex.
type shaderLoader struct {
	assetCounter[shaderKey, Shader]
}

// acquire hands out the variant of key, defined with defines, counting one
// more holder, and compiles it when the loader holds none: it preprocesses
// the file with defines, makes its module, and builds its layout from the
// module's bindings through table's layout interner. A file that fails to
// preprocess or compile is reported, and one declaring a binding in
// @group(3), or past the device's limits, is refused and reported; either
// gets a shader with no module, whose draws are skipped. Two acquires
// compiling one key at once each compile it, and the one that finishes
// second releases what it made and answers the first's.
func (l *shaderLoader) acquire(table *assetTable, device gfx.Device, files fs.FS, key shaderKey, defines []gfx.ShaderDefine) Shader {
	l.mutex.Lock()
	shader, ok := l.hold(key)
	l.mutex.Unlock()
	if ok {
		return shader
	}
	shader = table.addEmptyShader()
	slot, reportKind, err := key.compile(device, &table.layouts, files, shader, defines)
	table.fillShader(shader, slot)
	l.mutex.Lock()
	heldShader, ok := l.hold(key)
	if !ok {
		l.add(key, shader)
	}
	l.mutex.Unlock()
	if ok {
		table.removeShader(device, shader)
		return heldShader
	}
	switch {
	case reportKind != 0:
		table.report(reportKind, fmt.Errorf("render: shader %q with defines %q: %w", key.path, key.defines, err))
	case err != nil:
		table.logUnlessLost(fmt.Errorf("render: acquiring shader %q with defines %q: %w", key.path, key.defines, err))
	}
	return shader
}
