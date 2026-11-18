package canvas

// DrawState is the private draw state, for canvas's tests to read the
// entries a frame merged.
type DrawState = drawState

// MergedShapes answers the shapes the last frame merged from its draw lists,
// in merged order.
func (s *drawState) MergedShapes() []Shape { return s.shapes }
