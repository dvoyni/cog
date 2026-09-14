package types

import "fmt"

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

// ErrMeshGeometryInvalid reports geometry that could only draw garbage: no
// vertices at all, an index past the last vertex, or an index count that is not
// a multiple of three under a triangle list. The mint yields a zero MeshRef,
// which then draws nothing.
//
// This departs from canvas.DrawTriangles, which silently returns on bad input,
// because that is a per-frame recording call where a report would spam every
// frame, whereas a bake happens once.
type ErrMeshGeometryInvalid struct{ Reason string }

func (e ErrMeshGeometryInvalid) Error() string {
	return "scene: the mesh was rejected because " + e.Reason
}

// ErrMeshUnavailable reports a MeshRef that no longer names a mesh: released,
// stale against a slot that has been reissued, or temporary and used in a later
// frame. The draw is skipped, and the report fires once per ref per frame
// however many draws named it - a mesh that quietly stops appearing is the same
// failure class the generation counter exists to catch.
type ErrMeshUnavailable struct{ Mesh uint32 }

func (e ErrMeshUnavailable) Error() string {
	return fmt.Sprintf("scene: mesh %d has been released or belongs to an earlier frame", e.Mesh)
}

// ErrMeshUpdateRejected reports an UpdateMesh that would change something fixed
// for a ref's life. The mesh keeps the geometry it had.
type ErrMeshUpdateRejected struct {
	Mesh   uint32
	Reason string
}

func (e ErrMeshUpdateRejected) Error() string {
	return fmt.Sprintf("scene: mesh %d was not updated because %s", e.Mesh, e.Reason)
}
