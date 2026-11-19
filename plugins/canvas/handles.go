package canvas

// Material names one of canvas's 2D materials: a pointer-free, comparable
// handle an ECS component can hold. The zero Material is the default
// material, which draws texture × tint, and the only one canvas makes yet.
type Material struct{ index, generation uint32 }

// Image names an image, a path in platform's FS and its ImageOptions,
// interned by ImageFromFile into a pointer-free, comparable value an ECS
// component can hold. The zero Image is canvas's white texel, which shapes
// draw over: one opaque white texel, centred.
type Image struct{ index uint32 }
