package scene

import "fmt"

// ErrModelUnavailable reports a model that could not be loaded at all: the file
// is missing, it does not parse, it declares no scenes, or it requires an
// extension scene has no decoder for. The path is left in the failed state and
// never retried, so a typo does not spawn a load command every frame forever;
// UnloadModel is the only way back.
//
// The report fires from the load command's goroutine, so it lands a frame or
// more after the draw that triggered it. An error can therefore outlive the
// draw call that caused it, and a caller who draws a bad path once and never
// again still gets exactly one report.
type ErrModelUnavailable struct {
	Model string
	Err   error
}

func (e ErrModelUnavailable) Error() string {
	return fmt.Sprintf("scene: model %q was not loaded because %v", e.Model, e.Err)
}

func (e ErrModelUnavailable) Unwrap() error { return e.Err }

// ErrModelTextureUnavailable reports one texture of an otherwise sound model.
// The model still becomes resident and the slot binds the 1x1 default, because
// a model with one flat texture is a model you can see and fix, where a model
// dropped over a missing picture is a hole in the level with nothing in it to
// point at.
type ErrModelTextureUnavailable struct {
	Model   string
	Texture string
	Err     error
}

func (e ErrModelTextureUnavailable) Error() string {
	return fmt.Sprintf("scene: texture %q of model %q binds the default because %v",
		e.Texture, e.Model, e.Err)
}

func (e ErrModelTextureUnavailable) Unwrap() error { return e.Err }

// ErrModelPrimitiveSkipped reports one primitive scene cannot draw - a POINTS
// primitive, which gfx has no topology for, or geometry with no POSITION
// attribute at all. The rest of the model loads: a mesh that is mostly
// triangles should not be lost to one point cloud.
type ErrModelPrimitiveSkipped struct {
	Model string
	Mesh  string
	Err   error
}

func (e ErrModelPrimitiveSkipped) Error() string {
	return fmt.Sprintf("scene: a primitive of mesh %q in model %q was skipped because %v",
		e.Mesh, e.Model, e.Err)
}

func (e ErrModelPrimitiveSkipped) Unwrap() error { return e.Err }

// ErrModelBoundsMissing reports a primitive whose POSITION accessor carries no
// min/max, which glTF requires. The whole model becomes never-cull rather than
// taking a guessed box: drawing too much is a cost you can profile, where a
// wrong box is a model that vanishes at one camera angle and nowhere else.
type ErrModelBoundsMissing struct{ Model string }

func (e ErrModelBoundsMissing) Error() string {
	return fmt.Sprintf(
		"scene: model %q has a primitive whose POSITION accessor declares no min/max, so it is never culled",
		e.Model)
}

// ErrModelNodeDuplicated reports two nodes of one file sharing a name. A Node
// selector is the first depth-first match, so the second is unaddressable and
// the file has to be renamed for it to be drawn on its own. The model loads
// either way: the duplicate costs nothing to anything but the selector.
type ErrModelNodeDuplicated struct {
	Model string
	Node  string
}

func (e ErrModelNodeDuplicated) Error() string {
	return fmt.Sprintf(
		"scene: model %q has more than one node named %q, so a Node selector reaches only the first",
		e.Model, e.Node)
}

// ErrModelSceneMissing reports a draw naming a scene the file does not carry.
// The draw is skipped and never falls back to the default scene, for the same
// reason an unmatched node does not fall back to the whole file.
//
// glTF scene names are optional, and a file whose scenes are unnamed has no
// addressable scene but its default - which is what an empty Scene selects.
type ErrModelSceneMissing struct {
	Model string
	Scene string
}

func (e ErrModelSceneMissing) Error() string {
	return fmt.Sprintf("scene: model %q has no scene named %q, so the draw was skipped",
		e.Model, e.Scene)
}

// ErrModelNodeMissing reports a draw naming a node the selected scene does not
// carry. The draw is skipped and never falls back to the whole scene: one
// typo'd node name rendering an entire building at the origin is the worse
// failure of the two.
type ErrModelNodeMissing struct {
	Model string
	Scene string
	Node  string
}

func (e ErrModelNodeMissing) Error() string {
	return fmt.Sprintf("scene: model %q has no node named %q, so the draw was skipped",
		e.Model, e.Node)
}

// ErrModelNodeDegenerate reports a Node draw of a node whose authored world
// transform collapses an axis and so cannot be inverted. Re-rooting is exactly
// that inverse, so there is nothing to draw the subtree through; a whole-scene
// draw of the same file is unaffected and still draws it flat where the file
// put it.
type ErrModelNodeDegenerate struct {
	Model string
	Node  string
}

func (e ErrModelNodeDegenerate) Error() string {
	return fmt.Sprintf(
		"scene: node %q of model %q has a collapsed world transform, which cannot be re-rooted, so the draw was skipped",
		e.Node, e.Model)
}

// ErrModelSkinUnbound reports a skin whose inverse bind accessor could not be
// read. Every joint of that skin falls back to an identity inverse bind, which
// draws the mesh in its joints' own space rather than losing it.
type ErrModelSkinUnbound struct {
	Model string
	Skin  string
	Err   error
}

func (e ErrModelSkinUnbound) Error() string {
	return fmt.Sprintf(
		"scene: skin %q of model %q has unreadable inverse bind matrices (%v), so its joints bind at the identity",
		e.Skin, e.Model, e.Err)
}

func (e ErrModelSkinUnbound) Unwrap() error { return e.Err }

// ErrModelPoseApproximated reports a joint whose baked world matrix carries
// something translation, rotation and scale cannot represent - shear, almost
// always, from a non-uniformly scaled parent under a rotated child.
//
// The pose is baked from the decomposition anyway. A slightly wrong elbow
// beats a missing character, shear is invisible on virtually every real rig,
// and the report is what makes the approximation visible rather than silent.
// It fires once per model however many joints and frames carry it.
type ErrModelPoseApproximated struct {
	Model string
	Joint string
}

func (e ErrModelPoseApproximated) Error() string {
	return fmt.Sprintf(
		"scene: joint %q of model %q has a pose no TRS record can hold, so it was baked from its decomposition",
		e.Joint, e.Model)
}

// ErrModelClipMissing reports a ClipPlay naming a clip the model does not
// declare. The play is dropped and the rest of the draw's plays still blend:
// one typo'd clip name should cost the one play, not the character.
type ErrModelClipMissing struct {
	Model string
	Clip  string
}

func (e ErrModelClipMissing) Error() string {
	return fmt.Sprintf("scene: model %q declares no clip %q, so that play was dropped", e.Model, e.Clip)
}

// ErrModelPlaysOverLimit reports a draw that asked for more clip plays than one
// draw may blend. The heaviest are kept and the rest dropped by weight, which
// is what the character mostly looks like anyway.
type ErrModelPlaysOverLimit struct {
	Model string
	Plays int
	Limit int
}

func (e ErrModelPlaysOverLimit) Error() string {
	return fmt.Sprintf(
		"scene: a draw of model %q asked for %d clip plays against a limit of %d, so the lightest were dropped",
		e.Model, e.Plays, e.Limit)
}
