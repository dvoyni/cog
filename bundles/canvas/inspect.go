package canvas

import "github.com/dvoyni/cog/bundles/canvas/internal"

// OpKind identifies which recording call produced an Op.
type OpKind = internal.OpKind

const (
	OpSprite    = internal.OpSprite
	OpText      = internal.OpText
	OpTriangles = internal.OpTriangles
)

// Op is a read-only view of one recorded operation. OpQueue.Ops returns them in
// flush order so a recorder can assert what it produced — layer, order,
// transform, clip and parameters — without running the GPU pipeline. Op.Param
// and Op.ColorParam look a recorded parameter up by name.
type Op = internal.Op
