// Package types declares the concrete types anim's root aliases: Timelines and
// Timeline, whose unexported state the plugin advances, and everything those
// refer to or a track is built from (Params, State, Easing, Sequence, Lerp and
// Flipbook), with the easings, the Lerp constructors and Over that the root
// forwards to. It also holds the consume side of the resource: advancing every
// timeline by a tick.
//
// Each type is declared here and aliased in the root (type Timelines =
// types.Timelines). It stays a concrete type, and its exported methods are
// anim's public API through the alias. What anim's internal/ needs beyond that
// goes through the plain functions in friends.go: Go allows nothing outside
// bundles/anim to import this package. Nothing declared here imports the root,
// which keeps the arrangement acyclic.
package types
