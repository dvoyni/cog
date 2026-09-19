//go:build !js

package internal

import (
	"github.com/dvoyni/cog/bundles/input"
	"github.com/dvoyni/cog/kernel"
)

// wirePaste has nothing to wire on the desktop: a paste is a key press there,
// and pasteOnKeyPress reads the clipboard from inside it.
func (p *plugin) wirePaste() {}

// pasteOnKeyPress turns Ctrl+V (Cmd+V on macOS) into a ClipboardPaste change
// carrying what the system clipboard holds. It runs on the gogpu main thread,
// inside the key press callback, and reports a clipboard that will not open.
func (p *plugin) pasteOnKeyPress(k kernel.Executioner, key input.Key, mods input.Mods) {
	if key != input.KeyV || !(mods.Has(input.ModCtrl) || mods.Has(input.ModSuper)) {
		return
	}
	text, err := p.gpu.ClipboardRead()
	if err != nil {
		k.ReportError(err)
		return
	}
	if text != "" {
		p.pending = append(p.pending, input.ClipboardPasteChange(text))
	}
}
