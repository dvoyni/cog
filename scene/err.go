package scene

import "fmt"

// ErrCameraAlreadyRecorded reports a CameraID recorded twice in one frame. The
// first record wins and the second is dropped: Camera is a registration, not a
// free parameter, so a repeat means two systems each believe they own the
// camera and one of them is about to be surprised.
type ErrCameraAlreadyRecorded struct{ Camera CameraID }

func (e ErrCameraAlreadyRecorded) Error() string {
	return fmt.Sprintf("scene: camera %d was recorded twice this frame; the first record wins", e.Camera)
}

// ErrCameraClipPlanesMissing reports a camera with a zero Near or Far. The
// camera is skipped rather than defaulted: substituting a plausible number
// hides a real caller bug behind a degenerate projection that renders nothing
// for a reason nobody can see.
type ErrCameraClipPlanesMissing struct {
	Camera    CameraID
	Near, Far float32
}

func (e ErrCameraClipPlanesMissing) Error() string {
	return fmt.Sprintf("scene: camera %d has Near %v and Far %v; both are required and neither may be zero",
		e.Camera, e.Near, e.Far)
}

// ErrPassTargetUnsized reports a pass whose aspect cannot be resolved: a texture
// target that never reported a size, or a colourless pass whose depth
// attachment has none either. The pass is skipped, because a frustum built from
// a guessed aspect culls the wrong things and says nothing about why.
type ErrPassTargetUnsized struct {
	Camera CameraID
	Tag    PassTag
}

func (e ErrPassTargetUnsized) Error() string {
	return fmt.Sprintf("scene: pass %q of camera %d renders into a target of unknown size", e.Tag, e.Camera)
}

// ErrColourlessPassWithoutDepth reports a pass with no colour target and no
// explicit depth texture. Such a pass has no attachment at all to take a size
// from, and a shadow pass that falls through to the window's aspect silently
// drops casters.
type ErrColourlessPassWithoutDepth struct {
	Camera CameraID
	Tag    PassTag
}

func (e ErrColourlessPassWithoutDepth) Error() string {
	return fmt.Sprintf("scene: pass %q of camera %d has no colour target and no depth texture; name one with gfx.DepthTarget", e.Tag, e.Camera)
}

// ErrColourlessPassClearsColour reports a pass that asks to clear a colour
// attachment it does not have.
type ErrColourlessPassClearsColour struct {
	Camera CameraID
	Tag    PassTag
}

func (e ErrColourlessPassClearsColour) Error() string {
	return fmt.Sprintf("scene: pass %q of camera %d clears a colour it has no target for", e.Tag, e.Camera)
}

// ErrCameraProjectionDegenerate reports a camera whose projection parameters
// cannot make a volume: a zero or negative FovY, a zero Height, a Near at or
// past Far, or a target with no area.
type ErrCameraProjectionDegenerate struct {
	Camera CameraID
	Reason string
}

func (e ErrCameraProjectionDegenerate) Error() string {
	return fmt.Sprintf("scene: camera %d has no projection: %s", e.Camera, e.Reason)
}

// ErrMaterialTagAlreadyServed reports a Material with two entries for one pass
// tag. The first entry wins and the second is dropped, matching the
// duplicate-CameraID ruling: a repeat means two intentions for one pass and one
// of them is about to be surprised. The check runs when the frame interns the
// material, not per draw.
type ErrMaterialTagAlreadyServed struct{ Tag PassTag }

func (e ErrMaterialTagAlreadyServed) Error() string {
	return fmt.Sprintf("scene: a material has two entries for the %q pass tag; the first wins", e.Tag)
}

// ErrTextureUVSetUnsupported reports a material slot naming a TEXCOORD set past
// the two scene carries. The slot falls back to set 0 rather than being
// dropped, and says so: silently ignoring texCoord: 1 would be a wrong picture
// on a core glTF feature with nothing anywhere to explain it.
type ErrTextureUVSetUnsupported struct {
	Slot     string
	TexCoord int
}

func (e ErrTextureUVSetUnsupported) Error() string {
	return fmt.Sprintf("scene: %s names TEXCOORD_%d; scene carries two UV sets, so it falls back to TEXCOORD_0", e.Slot, e.TexCoord)
}
