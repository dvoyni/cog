package types

const (
	// StorageAlignment is the offset alignment a storage binding requires. A record
	// a draw binds a range of therefore pads up to a multiple of it - a pad, not a
	// cap on what a record may hold.
	StorageAlignment = 256

	MaxVertexAttributes = 16
	MaxVertexStride     = 2048

	// ParameterDescriptorInlineBytesCount is the largest value a parameter carries inline: a mat4, the
	// largest typed constructor's value.
	ParameterDescriptorInlineBytesCount = 64
)
