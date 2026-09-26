package descriptors

// DrawParams names one durable set of draw params: a shader, a fixed
// MaterialState and a value for some or all of the shader's bindings, created by
// ResourceQueue.NewDrawParams. It is an opaque handle, and its identity is the
// set's: two draws naming one DrawParams share shader, state and values, so the
// handle alone is the complete batch key a recorder needs.
//
// It is comparable and pointer-free, so a Component may hold one. The zero value
// names no set.
type DrawParams struct{ id uint32 }
