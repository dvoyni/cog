package ecsscene

import "fmt"

// The errors the recording System reports, each once per frame it happens in.
// They are scene's, under ecsscene's names: ecsscene repeats scene's path, so
// it refuses what scene refuses, and says so in the same words.

// ErrCameraAlreadyRecorded reports a second Camera Entity with an ID another
// already holds this frame. The first one walked wins, and the rest are not
// drawn.
type ErrCameraAlreadyRecorded struct{ Camera CameraID }

func (e ErrCameraAlreadyRecorded) Error() string {
	return fmt.Sprintf("ecsscene: camera %d is held by two Entities this frame; the first wins", e.Camera)
}

// ErrCameraClipPlanesMissing reports a Camera with a zero Near or Far. Both
// are required, because culling tests every sphere against all six planes.
type ErrCameraClipPlanesMissing struct {
	Camera    CameraID
	Near, Far float32
}

func (e ErrCameraClipPlanesMissing) Error() string {
	return fmt.Sprintf("ecsscene: camera %d has Near %v and Far %v; both are required and neither may be zero",
		e.Camera, e.Near, e.Far)
}

// ErrCameraProjectionDegenerate reports a camera whose projection parameters
// cannot make a volume: a zero or negative FovY, a zero Height, a Near at or
// past Far, or a transform with no inverse.
type ErrCameraProjectionDegenerate struct {
	Camera CameraID
	Reason string
}

func (e ErrCameraProjectionDegenerate) Error() string {
	return fmt.Sprintf("ecsscene: camera %d has no projection: %s", e.Camera, e.Reason)
}

// ErrPassTargetUnsized reports a pass whose target has no size to take an
// aspect from.
type ErrPassTargetUnsized struct {
	Camera CameraID
	Tag    PassTag
}

func (e ErrPassTargetUnsized) Error() string {
	return fmt.Sprintf("ecsscene: pass %q of camera %d renders into a target of unknown size", e.Tag, e.Camera)
}

// ErrColourlessPassWithoutDepth reports a depth-only pass with no depth
// texture, which leaves it no size to build a frustum from.
type ErrColourlessPassWithoutDepth struct {
	Camera CameraID
	Tag    PassTag
}

func (e ErrColourlessPassWithoutDepth) Error() string {
	return fmt.Sprintf("ecsscene: pass %q of camera %d has no colour target and no depth texture; name one with gfx.DepthTarget", e.Tag, e.Camera)
}

// ErrColourlessPassClearsColour reports a depth-only pass that asks to clear
// a colour it has no target for.
type ErrColourlessPassClearsColour struct {
	Camera CameraID
	Tag    PassTag
}

func (e ErrColourlessPassClearsColour) Error() string {
	return fmt.Sprintf("ecsscene: pass %q of camera %d clears a colour it has no target for", e.Tag, e.Camera)
}

// ErrMaterialTagAlreadyServed reports a Material with two tags for one pass.
// The first one wins.
type ErrMaterialTagAlreadyServed struct{ Tag PassTag }

func (e ErrMaterialTagAlreadyServed) Error() string {
	return fmt.Sprintf("ecsscene: a material has two entries for the %q pass tag; the first wins", e.Tag)
}

// ErrMeshCustomLayoutNeedsMaterial reports a Mesh with a custom vertex
// layout and no Material, which the bundled PBR cannot draw.
type ErrMeshCustomLayoutNeedsMaterial struct{ Mesh uint32 }

func (e ErrMeshCustomLayoutNeedsMaterial) Error() string {
	return fmt.Sprintf(
		"ecsscene: mesh %d has a custom vertex layout, which the bundled PBR cannot draw; give the Entity a Material",
		e.Mesh)
}
