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
