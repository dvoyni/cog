package gltf

import "fmt"

// The decoder's own reports. Each is non-fatal: the model still decodes, and
// the report rides Model.Reports for model to fire at install. model re-exports
// every one under the name scene has always given it, so a report is wrapped
// once and no settled name moves; the text still says "scene", because that is
// what a game has always read.

// ErrModelTextureUnavailable reports one texture of an otherwise sound model:
// a texture index the file has nothing at, an image it holds no bytes for, or
// a picture that opened and would not decode. A picture whose file could not be
// read at all is the asset library's own report, under the descriptor.
//
// The model still becomes resident either way, because a model with one wrong
// texture is a model you can see and fix, where a model dropped over a missing
// picture is a hole in the level with nothing in it to point at. What the slot
// binds is magenta for a colour slot and its own 1x1 default for a data one -
// magenta as a normal map is a surface lit from nowhere.
//
// It is reported once per picture rather than once per model, so two models
// naming one broken image report it once between them.
type ErrModelTextureUnavailable struct {
	Model   string
	Texture string
	Err     error
}

func (e ErrModelTextureUnavailable) Error() string {
	return fmt.Sprintf("scene: texture %q of model %q binds a placeholder because %v",
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
