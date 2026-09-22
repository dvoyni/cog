// Package types declares the concrete types scene's root aliases, and the
// machinery behind them: the recording vocabulary and the OpQueue with its
// recording methods; the consume side of the queue; the frame-local temporary
// mesh; scene's pass-tagged Material and its content key; and the camera maths
// the flush and the coordinate helpers both build on.
//
// Everything that outlives a frame is model's: the Lookup and its facades, the
// model and texture caches, the mesh table, the vertex, morph and animation
// packing, and the bundled PBR material. residency.go names them under the
// names scene has always used, until the sweep rewrites scene's callers.
//
// A type whose unexported state the plugin reads (OpQueue, Pass, MaterialTag,
// LayerMask) is declared here with its fields unexported and aliased in the
// root (type OpQueue = types.OpQueue). It stays a concrete type - no
// per-instance call goes through an interface - and its exported methods are
// scene's public API through the alias. The root's exported functions forward
// here. What scene's internal/ reads beyond that goes through the plain
// functions in friends.go. The types only internal/ ever holds (DrawRecord,
// ModelDrawRecord, AnimBinding and the rest) export what the flush reads,
// because no public API hands a consumer one of them.
//
// Go allows nothing outside bundles/scene to import this package. Nothing
// declared here imports the root, which is what keeps the arrangement acyclic,
// so everything a recording method refers to - down to the enums in its fields -
// is declared here too, or named here from model's root.
package types
