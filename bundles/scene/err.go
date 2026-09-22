package scene

import (
	"fmt"

	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/bundles/scene/internal/types"
)

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
type ErrCameraProjectionDegenerate = types.ErrCameraProjectionDegenerate

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
type ErrTextureUVSetUnsupported = model.ErrTextureUVSetUnsupported

// ErrSpotConeInverted reports a spot light whose InnerCone is at or past its
// OuterCone, which leaves no cone to smooth across. The light is skipped for
// the frame. OuterCone is the resolved value, so a zero one reads as pi/4.
type ErrSpotConeInverted struct {
	InnerCone, OuterCone float32
}

func (e ErrSpotConeInverted) Error() string {
	return fmt.Sprintf("scene: spot light inner cone %g is not inside its outer cone %g", e.InnerCone, e.OuterCone)
}

// ErrSpotDirectionMissing reports a spot light with a zero Direction. Its cone
// would evaluate to zero everywhere and the light would silently render
// black, so it is reported and skipped instead.
type ErrSpotDirectionMissing struct{}

func (ErrSpotDirectionMissing) Error() string {
	return "scene: spot light has no direction"
}

// ErrMeshGeometryInvalid reports geometry that could only draw garbage: no
// vertices at all, an index past the last vertex, or an index count that is not
// a multiple of three under a triangle list. The mint yields a zero MeshRef,
// which then draws nothing.
//
// This departs from canvas.DrawTriangles, which silently returns on bad input,
// because that is a per-frame recording call where a report would spam every
// frame, whereas a bake happens once.
type ErrMeshGeometryInvalid = model.ErrMeshGeometryInvalid

// ErrMeshUnavailable reports a MeshRef that no longer names a mesh: released,
// stale against a slot that has been reissued, or temporary and used in a later
// frame. The draw is skipped, and the report fires once per ref per frame
// however many draws named it - a mesh that quietly stops appearing is the same
// failure class the generation counter exists to catch.
type ErrMeshUnavailable = model.ErrMeshUnavailable

// ErrMeshCustomLayoutNeedsMaterial reports a draw handing the bundled PBR a
// layout it does not know. It knows exactly two - the standard layout every
// scene.Vertex mesh takes, and the skinned layout the glTF loader gives a
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
type ErrMeshCustomLayoutNeedsMaterial struct{ Mesh uint32 }

func (e ErrMeshCustomLayoutNeedsMaterial) Error() string {
	return fmt.Sprintf(
		"scene: mesh %d has a custom vertex layout, which the bundled PBR cannot draw; give the draw a Material",
		e.Mesh)
}

// ErrMeshUpdateRejected reports an UpdateMesh that would change something fixed
// for a ref's life. The mesh keeps the geometry it had.
type ErrMeshUpdateRejected = model.ErrMeshUpdateRejected

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
type ErrModelUnavailable = model.ErrModelUnavailable

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
type ErrModelTextureUnavailable = model.ErrModelTextureUnavailable

// ErrModelPrimitiveSkipped reports one primitive scene cannot draw - a POINTS
// primitive, which gfx has no topology for, or geometry with no POSITION
// attribute at all. The rest of the model loads: a mesh that is mostly
// triangles should not be lost to one point cloud.
type ErrModelPrimitiveSkipped = model.ErrModelPrimitiveSkipped

// ErrModelBoundsMissing reports a primitive whose POSITION accessor carries no
// min/max, which glTF requires. The whole model becomes never-cull rather than
// taking a guessed box: drawing too much is a cost you can profile, where a
// wrong box is a model that vanishes at one camera angle and nowhere else.
type ErrModelBoundsMissing = model.ErrModelBoundsMissing

// ErrModelPathInvalid is a path that is not a resource path at all - empty,
// absolute, NUL-bearing or escaping the mount root. Such a path never reaches
// the cache: it is refused where the caller is standing, leaving no entry and
// no tombstone, so a typo is permanently a typo until UnloadModel clears the
// report under the string that was passed.
type ErrModelPathInvalid = model.ErrModelPathInvalid

// ErrModelNodeDuplicated reports two nodes of one file sharing a name. A Node
// selector is the first depth-first match, so the second is unaddressable and
// the file has to be renamed for it to be drawn on its own. The model loads
// either way: the duplicate costs nothing to anything but the selector.
type ErrModelNodeDuplicated = model.ErrModelNodeDuplicated

// ErrModelSceneMissing reports a draw naming a scene the file does not carry.
// The draw is skipped and never falls back to the default scene, for the same
// reason an unmatched node does not fall back to the whole file.
//
// glTF scene names are optional, and a file whose scenes are unnamed has no
// addressable scene but its default - which is what an empty Scene selects.
type ErrModelSceneMissing = model.ErrModelSceneMissing

// ErrModelNodeMissing reports a draw naming a node the selected scene does not
// carry. The draw is skipped and never falls back to the whole scene: one
// typo'd node name rendering an entire building at the origin is the worse
// failure of the two.
type ErrModelNodeMissing = model.ErrModelNodeMissing

// ErrModelNodeDegenerate reports a Node draw of a node whose authored world
// transform collapses an axis and so cannot be inverted. Re-rooting is exactly
// that inverse, so there is nothing to draw the subtree through; a whole-scene
// draw of the same file is unaffected and still draws it flat where the file
// put it.
type ErrModelNodeDegenerate = model.ErrModelNodeDegenerate

// ErrModelSkinUnbound reports a skin whose inverse bind accessor could not be
// read. Every joint of that skin falls back to an identity inverse bind, which
// draws the mesh in its joints' own space rather than losing it.
type ErrModelSkinUnbound = model.ErrModelSkinUnbound

// ErrModelPoseApproximated reports a joint whose baked world matrix carries
// something translation, rotation and scale cannot represent - shear, almost
// always, from a non-uniformly scaled parent under a rotated child.
//
// The pose is baked from the decomposition anyway. A slightly wrong elbow
// beats a missing character, shear is invisible on virtually every real rig,
// and the report is what makes the approximation visible rather than silent.
// It fires once per model however many joints and frames carry it.
type ErrModelPoseApproximated = model.ErrModelPoseApproximated

// ErrModelClipMissing reports a ClipPlay naming a clip the model does not
// declare. The play is dropped and the rest of the draw's plays still blend:
// one typo'd clip name should cost the one play, not the character.
type ErrModelClipMissing = model.ErrModelClipMissing

// ErrModelPlaysOverLimit reports a draw that asked for more clip plays than one
// draw may blend. The heaviest are kept and the rest dropped by weight, which
// is what the character mostly looks like anyway.
type ErrModelPlaysOverLimit = model.ErrModelPlaysOverLimit

// ErrModelMorphWeightsOverLength reports a draw whose MorphWeights is longer
// than the model's flattened target list. The tail is ignored and the draw
// renders: MorphWeights is positional, so a caller whose array outlives an edit
// to the file should lose the shapes that went away, not the model.
//
// The short case is not an error at all and has no report. A caller animating
// the first two shapes of a fifty-shape face should not have to carry the other
// forty-eight zeros, so a short slice leaves the rest at 0.
type ErrModelMorphWeightsOverLength = model.ErrModelMorphWeightsOverLength

// ErrModelMorphTargetsOverLimit reports a draw whose active morph targets
// exceed what one draw may blend. The heaviest are kept and the rest dropped by
// absolute weight, which is what the shape mostly looks like anyway.
//
// Stored targets are unlimited: with sparse packing the cap constrains neither
// memory nor layout, and is purely a guard against runaway per-vertex ALU.
type ErrModelMorphTargetsOverLimit = model.ErrModelMorphTargetsOverLimit
