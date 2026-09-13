// Package internal holds what anim's contract root and animimpl share but no
// consumer may reach: the declarations of the contract types whose unexported
// state the plugin advances (Timelines and Timeline), everything those refer to
// (Params, State, Easing, Linear and Sequence), and the consume side of the
// resource — advancing every timeline by a tick.
//
// A contract type is declared here with its fields unexported and aliased in
// the root (type Timelines = internal.Timelines). It stays a concrete type, and
// its exported methods are anim's public API through the alias. What the root
// and animimpl need beyond that goes through the plain functions in friends.go,
// which only they can call: Go allows nothing outside bundles/anim to import
// this package. Nothing declared here imports the root, which is what keeps the
// arrangement acyclic.
package internal
