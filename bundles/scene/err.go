package scene

import (
	"github.com/dvoyni/cog/bundles/scene/internal"
)

// ErrCameraAlreadyRecorded reports a CameraID recorded twice in one frame. The
// first record wins and the second is dropped: Camera is a registration, not a
// free parameter, so a repeat means two systems each believe they own the
// camera and one of them is about to be surprised.
type ErrCameraAlreadyRecorded = internal.ErrCameraAlreadyRecorded

// ErrCameraClipPlanesMissing reports a camera with a zero Near or Far. The
// camera is skipped rather than defaulted: substituting a plausible number
// hides a real caller bug behind a degenerate projection that renders nothing
// for a reason nobody can see.
type ErrCameraClipPlanesMissing = internal.ErrCameraClipPlanesMissing

// ErrPassTargetUnsized reports a pass whose aspect cannot be resolved: a texture
// target that never reported a size, or a colourless pass whose depth
// attachment has none either. The pass is skipped, because a frustum built from
// a guessed aspect culls the wrong things and says nothing about why.
type ErrPassTargetUnsized = internal.ErrPassTargetUnsized

// ErrColourlessPassWithoutDepth reports a pass with no colour target and no
// explicit depth texture. Such a pass has no attachment at all to take a size
// from, and a shadow pass that falls through to the window's aspect silently
// drops casters.
type ErrColourlessPassWithoutDepth = internal.ErrColourlessPassWithoutDepth

// ErrColourlessPassClearsColour reports a pass that asks to clear a colour
// attachment it does not have.
type ErrColourlessPassClearsColour = internal.ErrColourlessPassClearsColour

// ErrCameraProjectionDegenerate reports a camera whose projection parameters
// cannot make a volume: a zero or negative FovY, a zero Height, a Near at or
// past Far, or a target with no area.
type ErrCameraProjectionDegenerate = internal.ErrCameraProjectionDegenerate

// ErrMaterialTagAlreadyServed reports a Material with two entries for one pass
// tag. The first entry wins and the second is dropped, matching the
// duplicate-CameraID ruling: a repeat means two intentions for one pass and one
// of them is about to be surprised. The check runs when the frame interns the
// material, not per draw.
type ErrMaterialTagAlreadyServed = internal.ErrMaterialTagAlreadyServed

// ErrMeshCustomLayoutNeedsMaterial reports a draw handing the bundled PBR a
// layout it does not know. It knows exactly two - the standard layout every
// model.Vertex mesh takes, and the skinned layout the glTF loader gives a
// geometry some placement skins - and every variant of it reads a prefix of
// one of them. The draw is skipped rather than handed to a pipeline that cannot
// describe it.
//
// The reverse - a named layout with a custom material - is fine, and so is a
// variant declaring fewer attributes than the mesh supplies: the direction that
// fails validation is a shader input no attribute supplies, never the other way
// round. A custom material over a named layout does have to read the stored
// formats, which are not the Go struct's: include VertexDecodePath for the
// normal and the tangent.
type ErrMeshCustomLayoutNeedsMaterial = internal.ErrMeshCustomLayoutNeedsMaterial
