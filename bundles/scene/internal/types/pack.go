package types

// SceneNoAnim is the animOffset of an instance that animates nothing.
const SceneNoAnim uint32 = ^uint32(0)

// AnimBinding is everything one batch's instances say about animation: the
// group 2 buffers they read, the offset of their sceneAnim block, whether the
// placement is skinned at all, and the one joint it rides when it is
// plain-bound.
//
// It is per batch rather than per instance because a batch is one primitive of
// one recorded call, and the instances of one call share the draw's animation
// — a hundred crates is one call, and a hundred independently-animated
// characters is a hundred calls.
type AnimBinding struct {
	Skin   SkinBuffers
	Offset uint32
	// MorphAt indexes the frame's per-primitive morph offsets, or is -1 when
	// the model has no shapes and the draw's one block serves every primitive.
	// It is an index rather than a slice because the arena it points into is
	// still being appended to while these are written.
	MorphAt int
	// Skinned is false for every buffer-built mesh and every debug shape, and
	// for a model primitive no clip can move. Riding the free rest-frame path
	// instead would be correct, but it charges a procedural terrain mesh — the
	// highest-vertex-count thing scene can be handed — a per-vertex pose fetch
	// and TRS blend for a guaranteed identity.
	Skinned bool
	// Joint is the model joint a plain-bound placement rides, and plain says
	// it is one: joint 0 is a joint like any other, and every buffer-built
	// draw's binding is the zero value. A plain binding implies skinned.
	Joint uint32
	Plain bool
}
