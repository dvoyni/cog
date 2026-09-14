// Package types declares the concrete types gfx's root aliases: the recording
// types whose unexported state the renderer reads - OpQueue, ResourceQueue and
// the descriptors - with their recording methods and the consume side of the
// queues; the GPU vocabulary they carry in their fields, from IDs and formats
// to the backend Queue and its sinks; the view types snapshots share; and the
// shader preprocessor.
//
// Each is declared here, with its fields unexported where the renderer reads
// the insides, and aliased in the root (type OpQueue = types.OpQueue). It stays
// a concrete type - no call through it goes through an interface - and its
// exported methods are gfx's public API through the alias. What the root's
// forwarders and gfx's internal/ read beyond that goes through the plain
// functions this package exports: Go allows nothing outside slots/gfx to import
// it. Nothing declared here imports the root, which is what keeps the
// arrangement acyclic.
package types
