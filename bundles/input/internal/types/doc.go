// Package types declares the concrete types input's root aliases: Key with its
// name table, Mods, Pos, Change and State, whose unexported state the plugin
// reads or writes, and the consume side of State — folding a change in and
// advancing the per-tick edges. It also holds Play, and what Play dispatches
// and carries: SynthesizeCmd, its request and response, and Action.
//
// Each is declared here and aliased in the root (type State = types.State). It
// stays a concrete type, and its exported methods are input's public API
// through the alias. What the root's forwarders and input's internal/ need
// beyond that goes through the plain functions this package exports, in
// friends.go: Go allows nothing outside bundles/input to import it. Nothing
// declared here imports the root, which keeps the arrangement acyclic.
package types
