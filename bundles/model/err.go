package model

import "github.com/dvoyni/cog/bundles/model/internal/types"

// ErrModelTextureUnavailable reports one texture of an otherwise sound model: a
// texture index the file has nothing at, an image it holds no bytes for, or a
// picture that opened and would not decode. The model still becomes resident,
// and the slot binds a placeholder.
type ErrModelTextureUnavailable = types.ErrModelTextureUnavailable

// ErrModelPrimitiveSkipped reports one primitive that cannot be drawn - a
// POINTS primitive, which gfx has no topology for, or geometry with no POSITION
// attribute at all. The rest of the model loads.
type ErrModelPrimitiveSkipped = types.ErrModelPrimitiveSkipped

// ErrModelBoundsMissing reports a primitive whose POSITION accessor carries no
// min/max, which glTF requires. The whole model becomes never-cull.
type ErrModelBoundsMissing = types.ErrModelBoundsMissing

// ErrModelNodeDuplicated reports two nodes of one file sharing a name. A Node
// selector reaches only the first depth-first match.
type ErrModelNodeDuplicated = types.ErrModelNodeDuplicated

// ErrModelSkinUnbound reports a skin whose inverse bind accessor could not be
// read. Every joint of that skin binds at the identity.
type ErrModelSkinUnbound = types.ErrModelSkinUnbound
