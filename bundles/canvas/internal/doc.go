// Package internal holds what canvas's contract root and canvasimpl share but
// no consumer may reach: the declarations of the recording vocabulary and the
// two resources, their recording and measuring methods, the consume side of the
// queue, the sprite and glyph atlases, the font store, inline-text parsing, and
// the built-in materials.
//
// A contract type whose unexported state canvasimpl reads (OpQueue, Lookup,
// LookupAccess) is declared here with its fields unexported and aliased in the
// root (type OpQueue = internal.OpQueue). It stays a concrete type - no
// per-sprite call goes through an interface - and its exported methods are
// canvas's public API through the alias. What canvasimpl reads beyond that goes
// through the plain functions in friends.go. The types only canvasimpl ever
// holds (LayerOps, DrawOp, Atlas, Font and the rest) export what the flush
// reads, because no public API hands a consumer one of them.
//
// Only the root and canvasimpl can import this package: Go allows nothing
// outside bundles/canvas to. Nothing declared here imports the root, which is
// what keeps the arrangement acyclic, so everything a recording method refers
// to - down to the enums in its fields - is declared here too.
package internal
