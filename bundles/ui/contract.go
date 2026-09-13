// Package ui is an immediate-mode layout and interaction vocabulary: a
// consumer declares a tree of Elements into the Frame every update tick, and
// ui measures and arranges it, hit-tests the pointer, records the visuals into
// canvas and publishes what was interacted with in Interactions.
//
// ui is a Bundle. This package is its contract root and declares no plugin:
// the Frame and Interactions resources, the Element and Modifier vocabulary
// with its built-in visuals and containers, Measure, HoverTracker, the
// layout-snapshot command ArmLayoutCmd and its views, and the ordering identity
// ProcessOnUpdate. The plugin - processing, the layout-snapshot slot and the
// mcp Provider - is in uiimpl, which only composition roots and tests import.
// The code the two share, including the layout engine and the consume side of
// the frame, is in internal.
//
// Element, Frame, Interactions and the vocabulary they carry are concrete
// types, aliased from internal, so declaring an element is a direct method call
// on a value with nothing between the caller and it.
package ui

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// Name is the ui plugin's name.
const Name kernel.PluginName = "ui"

// ProcessOnUpdate is the subscription type of the plugin's per-tick processing
// on app.UpdateEvent. It lays out the tick's Frame, resolves interactions
// against the pointer into Interactions, records every visual into
// canvas.OpQueue and clears the Frame. It is ordered
// After[input.AdvanceOnUpdate] and Before[canvas.FlushOnUpdate], so a producer
// declaring the frame orders Before it, and a reader of this tick's
// Interactions orders After it.
type ProcessOnUpdate kernel.Subscription[app.UpdateEvent]
