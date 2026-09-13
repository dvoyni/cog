// Package gfx is cog's driver-agnostic high-level renderer (renderer v2). It
// exposes declarative, high-level concepts to gameplay - Mesh, Material,
// OpQueue, Viewport - over the low-level GPU contract a driver implements,
// which is declared in the gpu package beneath it.
//
// Gameplay records into the writable OpQueue kernel resource; the plugin rotates
// completed queues through an internal triple buffer and consumes the latest on
// the render thread. The plugin translates high-level commands into a
// backend-agnostic gpu.Queue and hands it to gpu.Backend.Execute, so gameplay
// never touches a GPU API and the plugin never imports one.
//
// gfx is a Port. This package is its recording contract - commands, resource
// types, descriptors, the views, Name and the ordering identities - and
// declares no plugin. The GPU contract an Adapter implements, and every ID,
// format and enum both halves speak, is package gpu; this package aliases
// none of it, so each of those types has the one name gpu gives it. The plugin
// is gfximpl.New, and it works only once a gpu.Backend Adapter is bound to it:
// a driver such as wgpu provides one with kernel.Registrar.ProvideAdapter, and a
// composition without one fails with kernel.ErrMissingAdapter.
//
// Most recording types are declared in gfx/internal and aliased here, so that
// they stay concrete while their unexported state stays readable to gfximpl;
// see that package.
package gfx

// ViewportMode selects how the logical viewport responds to window aspect
// changes. ViewportWindow uses the window dimensions directly; fixed modes keep
// one dimension constant; Fit shows the full desired rectangle, while Cover
// fills the viewport from it.
type ViewportMode uint8

const (
	ViewportWindow ViewportMode = iota
	ViewportFixedWidth
	ViewportFixedHeight
	ViewportFit
	ViewportCover
)
