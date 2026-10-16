package internal

import "github.com/dvoyni/cog/slots/app"

// mainLoop is app's MainLoop Adapter: the gogpu main loop. It is a view of the
// plugin rather than the plugin itself, so the MainLoop's two methods do not join
// the methods the engine finds on the plugin value.
type mainLoop struct{ plugin *plugin }

var _ app.MainLoop = mainLoop{}

// Attach keeps the Loop app hands over. app calls it from its Start, which
// precedes Run, so the main and render threads find it set when they start.
func (d mainLoop) Attach(loop app.Loop) { d.plugin.loop = loop }

// Quit stops the gogpu main loop, which unwinds Run and shuts the engine down.
// app calls it for app.QuitCmd, from the goroutine that dispatched the command.
func (d mainLoop) Quit() { d.plugin.gpu.Quit() }

// ClipboardWrite puts text on the system clipboard through gogpu. app calls it
// for app.ClipboardWriteCmd, from the goroutine that dispatched the command.
func (d mainLoop) ClipboardWrite(text string) error { return d.plugin.gpu.ClipboardWrite(text) }

// SetCursorHidden records whether the cursor is to be hidden. app calls it for
// app.CursorCmd, from the goroutine that dispatched the command; the main
// thread applies it in its next onUpdate, since a window's cursor belongs to
// the thread that owns the window.
func (d mainLoop) SetCursorHidden(hidden bool) { d.plugin.cursorHidden.Store(hidden) }
