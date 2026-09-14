// Package ui declares the immediate-mode layout and interaction Bundle: a
// consumer declares a tree of Elements into the Frame every update tick, and
// ui measures and arranges it, hit-tests the pointer, records the visuals into
// canvas and publishes what was interacted with in Interactions.
//
// ui is a Bundle. Its plugin, built by uiplugin.New, requires no Adapter and
// contributes one McpProvider. This package declares what it offers: the Frame
// and Interactions resources, the Element and Modifier vocabulary with its
// built-in visuals and containers, Measure, HoverTracker, the layout-snapshot
// command ArmLayoutCmd and its views, and the ordering identity
// ProcessOnUpdate. The processing, the layout-snapshot slot and the mcp
// Provider are in ui's internal/; the layout engine and the consume side of the
// frame are in internal/types.
//
// Element, Frame, Interactions and the vocabulary they carry are concrete
// types, aliased from internal/types, so declaring an element is a direct
// method call on a value with nothing between the caller and it.
package ui
