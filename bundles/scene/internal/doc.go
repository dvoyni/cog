// Package internal holds what scene's contract root and sceneimpl share but no
// consumer may reach: the declarations of the recording vocabulary and the two
// resources, the recording methods of OpQueue and the queries and mutations of
// LookupAccess, the consume side of the queue, the model table with its
// residency, unloads and the glTF loader behind it, the mesh table, the bundled
// PBR material, the vertex, morph and animation packing, and the camera maths
// the flush and the coordinate helpers both build on.
//
// A contract type whose unexported state sceneimpl reads (OpQueue, Lookup,
// LookupAccess, MeshRef, Pass, MaterialTag, LayerMask) is declared here with its
// fields unexported and aliased in the root (type OpQueue = internal.OpQueue).
// It stays a concrete type - no per-instance call goes through an interface -
// and its exported methods are scene's public API through the alias. What
// sceneimpl reads beyond that goes through the plain functions in friends.go.
// The types only sceneimpl ever holds (DrawRecord, MeshRecord, ModelView,
// AnimBinding and the rest) export what the flush reads, because no public API
// hands a consumer one of them.
//
// The model load is two commands. LoadModelCmd, the parse, is declared here
// because the Lookup enqueues it from every draw and query of a path that is
// not resident; sceneimpl handles it, and declares and handles the install that
// follows.
//
// Only the root and sceneimpl can import this package: Go allows nothing
// outside bundles/scene to. Nothing declared here imports the root, which is
// what keeps the arrangement acyclic, so everything a recording method refers
// to - down to the enums in its fields - is declared here too.
package internal
