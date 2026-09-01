//go:build js

package wgpu

import "syscall/js"

var animationFrameDone = make(chan struct{}, 1)

var animationFrameCallback = js.FuncOf(func(js.Value, []js.Value) any {
	animationFrameDone <- struct{}{}
	return nil
})

// yieldMainThread blocks the calling goroutine until the browser fires the next
// animation frame, returning control to the JS event loop. gogpu's Run is a
// blocking Go loop with no requestAnimationFrame integration; on wasm that
// starves the event loop, so the WebGPU canvas is never composited (present is
// a no-op that relies on returning to the event loop) and wall-clock time
// barely advances between updates. Awaiting rAF once per frame lets the browser
// present the submitted frame and paces the loop to real vsync.
func yieldMainThread() {
	js.Global().Call("requestAnimationFrame", animationFrameCallback)
	<-animationFrameDone
}
