package scene

import (
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/m"
)

// OpKind identifies which recording call produced an Op.
type OpKind uint8

const (
	OpCamera OpKind = iota
	OpBox
)

// Op is a read-only view of one recorded operation, canvas's shape exactly.
type Op struct {
	Kind OpKind
	// Camera and Descr describe an OpCamera. Descr.Passes aliases the queue's
	// frame arena, like every other borrowed slice scene hands back.
	Camera CameraID
	Descr  CameraDescr
	// Layers, Transform and Color describe a recorded draw.
	Layers    LayerMask
	Transform Transform
	Color     m.Color
}

// PassView is the flush result for one pass: what scene decided, in numbers a
// test can assert with no GPU anywhere.
//
// Recorded is how many draws the camera's cull mask selected, Culled how many
// of those its frustum rejected, and Instances how many the pass packed after
// its tag filtered the survivors. Batches are in emission order: every opaque
// and alpha-masked draw sorted by material then mesh, then every blended draw
// back to front. Recording order is not preserved within a pass.
//
// Frustum is published because asserting that a specific sphere was rejected by
// a specific frustum is the whole point of a culling test; without it such a
// test can only count. The batch-shaped naming is kept even while InstanceCount
// is 1 for everything but an explicit instanced draw, because that is the shape
// it takes once instancing lands and renaming later would churn every test.
type PassView struct {
	CameraID  CameraID
	Order     gfx.Order
	Tag       PassTag
	Frustum   m.Frustum
	Recorded  int
	Culled    int
	Instances int
	Batches   []BatchView
}

// BatchView is one run of instances drawn from one mesh with one material.
type BatchView struct {
	MeshID, MaterialID           uint32
	FirstInstance, InstanceCount int
}
