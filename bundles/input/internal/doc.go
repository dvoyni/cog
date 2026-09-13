// Package internal holds what input's contract root and inputimpl share but no
// consumer may reach: the declarations of the contract types whose unexported
// state the plugin reads or writes (Key and its name table, Mods, Pos, Change
// and State), and the consume side of State — folding a change in and
// advancing the per-tick edges.
//
// A contract type is declared here with its fields unexported and aliased in
// the root (type State = internal.State). It stays a concrete type, and its
// exported methods are input's public API through the alias. What the root and
// inputimpl need beyond that goes through the plain functions in friends.go,
// which only they can call: Go allows nothing outside bundles/input to import
// this package. Nothing declared here imports the root, which is what keeps
// the arrangement acyclic.
package internal
