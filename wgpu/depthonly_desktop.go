//go:build !js

package wgpu

// depthOnlyPassSupported is false on the native path, where gogpu's Vulkan HAL
// never begins a render pass with no colour attachments and then faults ending
// it. See gfxdepthonly.go for the upstream lines and the symptom.
//
// It is a constant per platform rather than a query of the adapter because
// there is nothing to query: gogpu exposes no capability for this, and the
// native HAL this tree's dependency actually builds is Vulkan. If another
// native HAL lands that can encode the pass, this constant is the one place
// that has to learn the difference.
const depthOnlyPassSupported = false
