package types

// DrawState controls fixed render-pipeline state. Depth compare and depth
// write are independent because the states 3D needs most - test but do not
// write, or test with another compare - are inexpressible as one flag. Every
// zero value is both the WebGPU default and what the backend did before the
// field existed, so DrawState{} renders as it always has.
type DrawState struct {
	Blend        BlendMode   `json:"blend"`
	DepthCompare CompareFunc `json:"depthCompare"`
	DepthWrite   bool        `json:"depthWrite"`
	Cull         CullMode    `json:"cull"`
	FrontFace    FrontFace   `json:"frontFace"`
}
