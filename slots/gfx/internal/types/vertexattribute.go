package types

// VertexAttribute describes one attribute of the single interleaved vertex array: its
// byte offset and element type. Attributes bind to shader @location values in the
// order given.
type VertexAttribute struct {
	Offset int
	Type   VertexType
}
