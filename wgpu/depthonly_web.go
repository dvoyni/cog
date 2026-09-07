//go:build js

package wgpu

// depthOnlyPassSupported is true in a browser: gogpu's web path hands the
// descriptor to the platform's own WebGPU implementation, which encodes a pass
// with a depth attachment and no colour attachment exactly as specified. The
// cameras demo's depth prepass runs here and is skipped on the desktop, which
// is the only observable difference between the two builds of it.
const depthOnlyPassSupported = true
