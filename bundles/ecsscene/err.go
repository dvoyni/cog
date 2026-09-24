package ecsscene

import "github.com/dvoyni/cog/bundles/ecsscene/internal"

// ErrCameraAlreadyRecorded reports a second Camera Entity with an ID another
// already holds this frame. The first one walked wins, and the rest are not
// drawn.
type ErrCameraAlreadyRecorded = internal.ErrCameraAlreadyRecorded

// ErrCameraClipPlanesMissing reports a Camera with a zero Near or Far. Both
// are required, because culling tests every sphere against all six planes.
type ErrCameraClipPlanesMissing = internal.ErrCameraClipPlanesMissing

// ErrCameraProjectionDegenerate reports a camera whose projection parameters
// cannot make a volume: a zero or negative FovY, a zero Height, a Near at or
// past Far, or a transform with no inverse.
type ErrCameraProjectionDegenerate = internal.ErrCameraProjectionDegenerate

// ErrPassTargetUnsized reports a pass whose target has no size to take an
// aspect from.
type ErrPassTargetUnsized = internal.ErrPassTargetUnsized

// ErrColourlessPassWithoutDepth reports a depth-only pass with no depth
// texture, which leaves it no size to build a frustum from.
type ErrColourlessPassWithoutDepth = internal.ErrColourlessPassWithoutDepth

// ErrColourlessPassClearsColour reports a depth-only pass that asks to clear
// a colour it has no target for.
type ErrColourlessPassClearsColour = internal.ErrColourlessPassClearsColour

// ErrMaterialTagAlreadyServed reports a Material with two tags for one pass.
// The first one wins.
type ErrMaterialTagAlreadyServed = internal.ErrMaterialTagAlreadyServed

// ErrMeshCustomLayoutNeedsMaterial reports a Mesh with a custom vertex
// layout and no Material, which the bundled PBR cannot draw.
type ErrMeshCustomLayoutNeedsMaterial = internal.ErrMeshCustomLayoutNeedsMaterial
