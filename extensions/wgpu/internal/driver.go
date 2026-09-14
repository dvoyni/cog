package internal

import "github.com/dvoyni/cog/slots/app"

// driver is app's Driver Adapter: the gogpu main loop. It is a view of the
// plugin rather than the plugin itself, so the Driver's two methods do not join
// the methods the engine finds on the plugin value.
type driver struct{ plugin *plugin }

var _ app.Driver = driver{}

// Attach keeps the Loop app hands over. app calls it from its Start, which
// precedes Run, so the main and render threads find it set when they start.
func (d driver) Attach(loop app.Loop) { d.plugin.loop = loop }

// Quit stops the gogpu main loop, which unwinds Run and shuts the engine down.
// app calls it for app.QuitCmd, from the goroutine that dispatched the command.
func (d driver) Quit() { d.plugin.gpu.Quit() }
