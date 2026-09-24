package ecsscene

import "github.com/dvoyni/cog/bundles/ecsscene/internal"

// Layer is the mask of one layer. There are 32 of them; an index past the end
// wraps rather than silently becoming zero, which would read as every layer.
func Layer(i uint) LayerMask { return internal.Layer(i) }
