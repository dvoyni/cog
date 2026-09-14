package ui

import "github.com/dvoyni/cog/bundles/ui/internal/types"

// Frame collects the complete UI declaration for one update tick. It is a
// writable resource: a producer adds its roots before ProcessOnUpdate, which
// consumes and clears it. Consuming it is the plugin's alone.
type Frame = types.Frame

// Interactions contains the results from the most recently processed UI frame.
// A reader before ProcessOnUpdate sees the previous tick's, and a reader after
// it this tick's.
type Interactions = types.Interactions
