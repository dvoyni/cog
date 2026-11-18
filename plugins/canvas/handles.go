package canvas

// Material names one of canvas's 2D materials: a pointer-free, comparable
// handle an ECS component can hold. The zero Material is the default
// material, which draws texture × tint, and the only one canvas makes yet.
type Material struct{ index, generation uint32 }
