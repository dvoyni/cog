// Package canvastest runs canvas in a real kernel on render's fake GPU, for
// canvas's own tests and those of the plugins that record into it.
//
// A test makes a Harness, spawns it with render's and canvas's configs and
// its own plugins, which are listed after canvas, records a frame and steps:
//
//	h := canvastest.NewHarness(t)
//	h.Spawn(nil, nil, myplugin.NewPlugin())
//	h.Record(func(list canvas.DrawList) {
//		list.DrawShape(canvas.Shape{Rect: m.Rect{Width: 10, Height: 10}, Tint: m.Color{R: 1, A: 1}})
//	})
//	h.Step()
//
// The harness is rendertest's, whose calls it keeps: Frames reads back what
// reached render, and OpenWindow, RecordNothing and UseSoftwareAdapter work
// as they do there. A test package with pixel tests runs its tests through
// RunMain, as rendertest's do.
//
// The harness fails the test on any error the kernel logs, unless it is a
// canvas.Report of a kind the test named with ExpectReport, a render.Report
// of a kind named with the embedded rendertest harness's ExpectReport, or an
// error a match given ExpectError answers true of.
package canvastest
