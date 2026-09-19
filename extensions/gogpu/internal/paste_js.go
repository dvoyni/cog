//go:build js

package internal

import (
	"syscall/js"

	"github.com/dvoyni/cog/bundles/input"
	"github.com/dvoyni/cog/kernel"
)

// wirePaste turns the browser's paste event into a ClipboardPaste change. A
// browser hands the clipboard to a page only through that event, and it fires
// only for a paste whose keydown was not cancelled - which gogpu does for every
// key on its canvas. So a capture listener on window, which runs before the
// canvas's own, stops Ctrl+V (Cmd+V) from reaching gogpu at all and queues the
// key press itself; the key's release is left alone and reaches gogpu as usual.
// Both callbacks run on the one wasm thread, as flushInput does, so pending
// still needs no lock.
func (p *plugin) wirePaste() {
	pasteKey := js.FuncOf(func(_ js.Value, args []js.Value) any {
		event := args[0]
		command := event.Get("ctrlKey").Bool() || event.Get("metaKey").Bool()
		if !command || event.Get("code").String() != "KeyV" {
			return nil
		}
		event.Call("stopPropagation")
		var mods input.Mods
		if event.Get("ctrlKey").Bool() {
			mods |= input.ModCtrl
		}
		if event.Get("metaKey").Bool() {
			mods |= input.ModSuper
		}
		if event.Get("shiftKey").Bool() {
			mods |= input.ModShift
		}
		if event.Get("altKey").Bool() {
			mods |= input.ModAlt
		}
		p.pending = append(p.pending, input.KeyChange(input.KeyV, mods, true))
		return nil
	})
	paste := js.FuncOf(func(_ js.Value, args []js.Value) any {
		data := args[0].Get("clipboardData")
		if data.IsUndefined() || data.IsNull() {
			return nil
		}
		if text := data.Call("getData", "text/plain").String(); text != "" {
			p.pending = append(p.pending, input.ClipboardPasteChange(text))
		}
		return nil
	})
	window := js.Global()
	window.Call("addEventListener", "keydown", pasteKey, map[string]any{"capture": true})
	window.Call("addEventListener", "paste", paste)
}

// pasteOnKeyPress has nothing to do in the browser: the paste event carries the
// text, and the key press that caused it never reaches gogpu.
func (p *plugin) pasteOnKeyPress(kernel.Executioner, input.Key, input.Mods) {}
