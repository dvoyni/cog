package types

import (
	"fmt"

	"github.com/dvoyni/cog/bundles/model"
)

// ErrModelUnavailable reports a model the decode refused: it does not parse, it
// declares no scenes, or it requires an extension scene has no decoder for. The
// failure is cached as the model, so the file is never read again - a typo must
// not re-read it every frame forever - and UnloadModel is the only way back.
//
// A file that could not be read at all is not this: the read is the asset
// library's, and so is its report, which wraps the underlying error and names
// the path. State returns whichever of the two applies.
//
// The report fires from the handler whose call triggered the load - the flush
// for a draw, the caller's own handler for a query - so it cannot outlive the
// call that caused it.
type ErrModelUnavailable struct {
	Model string
	Err   error
}

func (e ErrModelUnavailable) Error() string {
	return fmt.Sprintf("scene: model %q was not loaded because %v", e.Model, e.Err)
}

func (e ErrModelUnavailable) Unwrap() error { return e.Err }

// ErrModelPathInvalid is a path that is not a resource path at all - empty,
// absolute, NUL-bearing or escaping the mount root. Such a path never reaches
// the cache: it is refused where the caller is standing, leaving no entry and
// no tombstone, so a typo is permanently a typo until UnloadModel clears the
// report under the string that was passed.
type ErrModelPathInvalid struct{ Model string }

func (e ErrModelPathInvalid) Error() string {
	return fmt.Sprintf("scene: invalid model path %q", e.Model)
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

// ErrModelMorphWeightsOverLength reports a draw whose MorphWeights is longer
// than the model's flattened target list. The tail is ignored and the draw
// renders: MorphWeights is positional, so a caller whose array outlives an edit
// to the file should lose the shapes that went away, not the model.
//
// The short case is not an error at all and has no report. A caller animating
// the first two shapes of a fifty-shape face should not have to carry the other
// forty-eight zeros, so a short slice leaves the rest at 0.
type ErrModelMorphWeightsOverLength struct {
	Model   string
	Weights int
	Slots   int
}

func (e ErrModelMorphWeightsOverLength) Error() string {
	return fmt.Sprintf(
		"scene: a draw of model %q gave %d morph weights against %d targets, so the tail was ignored",
		e.Model, e.Weights, e.Slots)
}

// ErrModelMorphTargetsOverLimit reports a draw whose active morph targets
// exceed what one draw may blend. The heaviest are kept and the rest dropped by
// absolute weight, which is what the shape mostly looks like anyway.
//
// Stored targets are unlimited: with sparse packing the cap constrains neither
// memory nor layout, and is purely a guard against runaway per-vertex ALU.
type ErrModelMorphTargetsOverLimit struct {
	Model   string
	Targets int
	Limit   int
}

func (e ErrModelMorphTargetsOverLimit) Error() string {
	return fmt.Sprintf(
		"scene: a draw of model %q has %d active morph targets against a limit of %d, so the lightest were dropped",
		e.Model, e.Targets, e.Limit)
}

// The decoder's reports are declared beside it, in model, and named here under
// the names scene has always given them.
type (
	ErrModelTextureUnavailable = model.ErrModelTextureUnavailable
	ErrModelPrimitiveSkipped   = model.ErrModelPrimitiveSkipped
	ErrModelBoundsMissing      = model.ErrModelBoundsMissing
	ErrModelNodeDuplicated     = model.ErrModelNodeDuplicated
	ErrModelSkinUnbound        = model.ErrModelSkinUnbound
)
