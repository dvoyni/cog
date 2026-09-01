//go:build !js

package wgpu

// yieldMainThread is a no-op on desktop; gogpu's native run loop paces itself.
func yieldMainThread() {}
