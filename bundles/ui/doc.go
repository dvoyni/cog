// Package ui declares the immediate-mode layout and interaction Bundle: a
// consumer declares a tree of Elements into the Frame every update tick, and
// ui measures and arranges it, hit-tests the pointer, records the visuals into
// canvas and publishes what was interacted with in Interactions.
//
// ui is a Bundle. Its plugin, built by uiplugin.New, requires no Adapter and
// contributes one McpProvider. This package offers the Frame and Interactions
// resources, the Element and Modifier vocabulary with its built-in visuals and
// containers, Measure, HoverTracker, the layout-snapshot command ArmLayoutCmd
// and its views, and the ordering identity ProcessOnUpdate, and declares none
// of them: each is an alias of, or a forwarder into, what ui's internal/
// declares, beside the processing, the layout engine, the consume side of the
// frame, the layout-snapshot slot and the mcp Provider.
//
// Element, Frame, Interactions and the vocabulary they carry are concrete
// types, aliased from internal, so declaring an element is a direct method call
// on a value with nothing between the caller and it.
package ui
