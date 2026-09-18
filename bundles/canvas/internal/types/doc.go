// Package types declares the concrete types canvas's root aliases, and the
// machinery behind them: the recording vocabulary and the two resources,
// OpQueue and Lookup, with their recording, measuring and unloading methods; the
// consume side of the queue; the five asset caches with their loaders, the two
// atlas packers behind them, and inline-text parsing; Config, which the packers
// hold; the halo profile; and the built-in materials.
//
// A type whose unexported state the plugin reads (OpQueue, Lookup, LookupAccess,
// LookupDeviceAccess) is declared here with its fields unexported and aliased in the
// root (type OpQueue = types.OpQueue). It stays a concrete type - no per-sprite
// call goes through an interface - and its exported methods are canvas's public
// API through the alias. The root's exported functions forward here. What
// canvas's internal/ reads beyond that goes through the plain functions in
// friends.go. The types only internal/ ever holds (LayerOps, DrawOp, AtlasEntry, Font
// and the rest) export what the flush reads, because no public API hands a
// consumer one of them.
//
// Go allows nothing outside bundles/canvas to import this package. Nothing
// declared here imports the root, which is what keeps the arrangement acyclic,
// so everything a recording method refers to - down to the enums in its fields -
// is declared here too.
package types
