package render

import (
	"fmt"
	"io/fs"
	"slices"

	"github.com/dvoyni/cog/gfx"
)

// shaderLoader shares, counts and frees the shaders render loads from files:
// one variant for each path and set of defines, compiled in the job that
// first acquires it, counted by the acquires holding it, and freed at the end
// of the frame its count reaches zero in. Each variant keeps the files its
// preprocessing read, its own and those it included, which ReloadChanged
// stats. It compiles holding no mutex.
type shaderLoader struct {
	assetCounter[shaderKey, Shader]
	// pendingReloads are the variants ReloadChanged compiled again, to be
	// swapped in at the start of the next frame.
	pendingReloads []assetReload[Shader, *shaderSlot]
}

// acquire hands out the variant of path defined with sortedDefines, whose
// key joins them as joinedDefines, counting one more holder, and compiles it
// when the loader holds none: it preprocesses the file with the defines,
// makes its module, and builds its layout from the module's bindings
// through table's layout interner. A file that fails to preprocess or
// compile is reported, and one declaring a binding in @group(3), or past the
// device's limits, is refused and reported; either gets a shader with no
// module, whose draws are skipped. Two acquires compiling one key at once
// each compile it, and the one that finishes second releases what it made
// and answers the first's. It keeps neither slice: a variant held is found
// by them, and one compiled keeps copies.
func (l *shaderLoader) acquire(table *assetTable, device gfx.Device, files fs.FS, path string, joinedDefines []byte, sortedDefines []gfx.ShaderDefine) Shader {
	l.mutex.Lock()
	l.requireLocked("acquire")
	shader, ok := l.handles[shaderKey{path: path, defines: string(joinedDefines)}]
	if ok {
		l.holdHandle(shader)
	}
	l.mutex.Unlock()
	if ok {
		return shader
	}
	key := shaderKey{path: path, defines: string(joinedDefines)}
	defines := slices.Clone(sortedDefines)
	shader = table.addEmptyShader()
	stampingFiles := &stampingFS{files: files}
	slot, reportKind, err := key.compile(device, &table.layouts, table.inspector, stampingFiles, shader, defines)
	slot.defines = defines
	table.fillShader(shader, slot)
	l.mutex.Lock()
	heldShader, ok := l.hold(key)
	if !ok {
		l.add(key, shader)
		l.setFiles(shader, stampingFiles.stampedFiles)
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

// reloadChanged compiles again, in the caller's job, each variant l holds
// one of whose files, its own or one its preprocessing included, changed
// since it was compiled, as acquire compiles it, into a slot that
// swapReloads puts behind its handle at the start of the next frame. The
// variant keeps the files the new preprocessing read, so an include added
// is watched from then on. A variant that fails to preprocess or compile,
// or is refused, is reported once for that change and keeps drawing with
// the module it had; it keeps the files its preprocessing read or looked
// for, so their next change, or an include it lacked appearing, tries again.
func (l *shaderLoader) reloadChanged(table *assetTable, device gfx.Device, files fs.FS) {
	for _, changed := range l.claimChanged(files) {
		previous, _ := table.findSlot(&table.shaders, changed.handle)
		stampingFiles := &stampingFS{files: files}
		slot, reportKind, err := changed.key.compile(device, &table.layouts, table.inspector, stampingFiles, changed.handle, previous.defines)
		slot.defines = previous.defines
		if err != nil {
			l.mutex.Lock()
			l.replaceFiles(changed.handle, changed.files, stampingFiles.stampedFiles)
			l.mutex.Unlock()
			if reportKind != 0 {
				table.report(reportKind, fmt.Errorf("render: shader %q with defines %q failed to reload, drawing with the module it had: %w", changed.key.path, changed.key.defines, err))
			} else {
				table.logUnlessLost(fmt.Errorf("render: reloading shader %q with defines %q: %w", changed.key.path, changed.key.defines, err))
			}
			continue
		}
		reload := assetReload[Shader, *shaderSlot]{handle: changed.handle, files: stampingFiles.stampedFiles, replacement: &slot}
		l.queueReload(&l.pendingReloads, changed.files, reload, func(replacement *shaderSlot) { replacement.releaseModule(device, &table.layouts) })
	}
}
