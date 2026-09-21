// Package types declares the concrete types scene's root aliases, and the
// machinery behind them: the recording vocabulary and the two resources with
// the recording methods of OpQueue and the queries and mutations of
// LookupAccess and LookupDeviceAccess; the consume side of the queue; Config,
// which the Lookup holds; the model cache with its loader and unloads, and the
// conversion of model's decoded glTF into vertices, baked poses, morph blocks
// and PBR records behind them; the mesh table; the bundled PBR material; the
// vertex, morph and animation packing; and the camera maths the flush and the
// coordinate helpers both build on.
//
// A type whose unexported state the plugin reads (OpQueue, Lookup,
// LookupAccess, LookupDeviceAccess, MeshRef, Pass, MaterialTag, LayerMask) is declared here with its
// fields unexported and aliased in the root (type OpQueue = types.OpQueue). It
// stays a concrete type - no per-instance call goes through an interface - and
// its exported methods are scene's public API through the alias. The root's
// exported functions forward here. What scene's internal/ reads beyond that
// goes through the plain functions in friends.go. The types only internal/ ever
// holds (DrawRecord, MeshRecord, ModelView, AnimBinding and the rest) export
// what the flush reads, because no public API hands a consumer one of them.
//
// The model load is no command at all. It is a libs/assets cache whose loader
// reads, parses and uploads inside Get, so a load runs in the handler that
// asked for it - the flush for a draw, the caller's own handler for a query -
// and nothing about it is in flight between frames.
//
// Go allows nothing outside bundles/scene to import this package. Nothing
// declared here imports the root, which is what keeps the arrangement acyclic,
// so everything a recording method refers to - down to the enums in its fields -
// is declared here too.
package types
