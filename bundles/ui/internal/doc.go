// Package internal holds what ui's contract root and uiimpl share but no
// consumer may reach: the declarations of the Element and Modifier vocabulary,
// the built-in visuals and containers, the Frame and Interactions resources, the
// layout engine (Processor) that both Measure and the plugin run, and the
// rendering of a resolved tree into the layout-snapshot views.
//
// A contract type whose unexported state uiimpl or the layout engine reads
// (Element, Frame, Interactions, Interaction) is declared here with its fields
// unexported and aliased in the root (type Element = internal.Element). It stays
// a concrete type - declaring an element goes through no interface - and its
// exported methods are ui's public API through the alias. What uiimpl reads
// beyond that goes through the plain functions in friends.go. The types only
// uiimpl ever holds (Processor, GlobalState, PointerState, PointerEvent) export
// what processing calls, because no public API hands a consumer one of them.
//
// Only the root and uiimpl can import this package: Go allows nothing outside
// bundles/ui to. Nothing declared here imports the root, which is what keeps the
// arrangement acyclic, so everything an Element refers to - down to the enums in
// its fields, the visuals' params and the snapshot views - is declared here too.
package internal
