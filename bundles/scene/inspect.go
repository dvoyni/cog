package scene

import "github.com/dvoyni/cog/bundles/scene/internal"

// OpKind identifies which recording call produced an Op.
type OpKind = internal.OpKind

const (
	OpCamera     = internal.OpCamera
	OpBox        = internal.OpBox
	OpSphere     = internal.OpSphere
	OpPlane      = internal.OpPlane
	OpLine3D     = internal.OpLine3D
	OpWireBox    = internal.OpWireBox
	OpMesh       = internal.OpMesh
	OpPointLight = internal.OpPointLight
	OpSpotLight  = internal.OpSpotLight
	OpModel      = internal.OpModel
)

// Op is a read-only view of one recorded operation, canvas's shape exactly: it
// reports the call as the recorder made it, not the draws scene derived from
// it, so a WireBox is one Op.
type Op = internal.Op

// PassView is the flush result for one pass: what scene decided, in numbers a
// test can assert with no GPU anywhere.
//
// Recorded is how many draws the camera's cull mask selected, Culled how many
// of those its frustum rejected, and Instances how many the pass packed after
// its tag filtered the survivors. Instances counts instances, so it is not
// len(Batches): an instanced call's survivors are one batch of N. Batches are
// in emission order: every opaque and alpha-masked draw sorted by material then
// mesh, then every blended draw back to front. Recording order is not preserved
// within a pass.
//
// The numbers are per pass, not per frame: a draw two cameras both see is
// counted, sorted and packed once in each of their passes, because the sort is
// what a pass is. That is the cost of sorting and it is not worked around.
//
// Frustum is published because asserting that a specific sphere was rejected by
// a specific frustum is the whole point of a culling test; without it such a
// test can only count.
//
// Lights is how many punctual lights the pass packed after its own frustum
// culled them and the cap of 16 took the brightest at the eye. The lights
// dropped past the cap are not counted anywhere: the drop is silent by design.
type PassView = internal.PassView

// BatchView is one run of instances drawn from one mesh with one material: one
// gfx draw call, one material record, and InstanceCount contiguous instances of
// the pass's own instance slice starting at FirstInstance.
//
// InstanceCount is 1 for everything but an explicit instanced draw. What the
// flush batches is the surviving instances of one instanced call - not
// consecutive equal draws recorded separately, which is a deferred
// optimisation, and not a blended instanced draw, which stays one batch per
// instance so its entries keep their own depths.
type BatchView = internal.BatchView
