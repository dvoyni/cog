// Package internal holds what gfx's contract root and gfximpl share but no
// consumer may reach: the declarations of the contract types whose unexported
// state the renderer reads, their recording methods, the consume side of the
// queues, and the shader preprocessor.
//
// A contract type that gfximpl reads the insides of is declared here, with its
// fields unexported, and aliased in the root (type OpQueue = internal.OpQueue).
// It stays a concrete type - no call through it goes through an interface - and
// its exported methods are gfx's public API through the alias. What the root
// and gfximpl read beyond that goes through the plain functions this package
// exports, which only they can call: Go allows nothing outside
// extensions/gfx to import it. Nothing declared here imports the root, which
// is what keeps the arrangement acyclic.
//
// The IDs, formats and enums those types carry in their fields are not
// declared here: they are the GPU vocabulary, declared in gpu, which this
// package, the root and gfximpl all import.
package internal
