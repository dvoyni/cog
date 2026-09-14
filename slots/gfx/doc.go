// Package gfx declares the gfx Slot, cog's driver-agnostic high-level renderer
// (renderer v2). It exposes declarative, high-level concepts to gameplay -
// Mesh, Material, OpQueue, Viewport - over the low-level GPU contract a driver
// implements: the Backend interface, the Queue gfx replays into it, and the
// IDs, formats, enums and descriptors both halves speak, each with the one
// name gfx.X.
//
// Gameplay records into the writable OpQueue kernel resource; the plugin rotates
// completed queues through an internal triple buffer and consumes the latest on
// the render thread. The plugin translates high-level commands into a
// backend-agnostic Queue and hands it to Backend.Execute, so gameplay never
// touches a GPU API and the plugin never imports one.
//
// gfx is a Slot: its plugin, built by gfxplugin.New, works only once an Adapter
// for BackendPort is bound to it. A driver such as gogpu provides one with
// kernel.Registrar.ProvideAdapter, and a composition without one fails with
// kernel.ErrMissingAdapter.
//
// This package holds declarations only. The recording types and the GPU
// vocabulary are declared in gfx/internal/types and aliased here, so that they
// stay concrete while their unexported state stays readable to the plugin; see
// that package.
package gfx
