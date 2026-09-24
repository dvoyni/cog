package internal

import "github.com/dvoyni/cog/bundles/model"

// AnimBinding is everything one batch's instances say about animation: the
// group 2 buffers they read, and what model's instance packer writes - the
// offset of their sceneAnim block, whether the placement is skinned at all, and
// the one joint it rides when it is plain-bound.
//
// It is per batch rather than per instance because a batch is one primitive of
// one recorded call, and the instances of one call share the draw's animation
// — a hundred crates is one call, and a hundred independently-animated
// characters is a hundred calls.
type AnimBinding struct {
	Skin model.SkinBuffers
	// MorphAt indexes the frame's per-primitive morph offsets, or is -1 when
	// the model has no shapes and the draw's one block serves every primitive.
	// It is an index rather than a slice because the arena it points into is
	// still being appended to while these are written.
	MorphAt int
	model.InstanceAnim
}
