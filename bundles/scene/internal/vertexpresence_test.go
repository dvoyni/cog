package internal

import (
	"testing"

	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/bundles/scene/internal/types"
	"github.com/qmuntal/gltf"
)

// The bytes, end to end. A static primitive's vertex buffer is 32 bytes a
// vertex and its layout is the standard six; a skinned one is 40 and eight.
// Everything downstream of the mesh record is a buffer id and a size, so this
// is where the saving is observable at all.
func TestAModelPrimitiveStoresTheStrideItsLayoutNames(t *testing.T) {
	for _, c := range []struct {
		what   string
		doc    *gltf.Document
		attrs  int
		stride int
	}{
		{"a static primitive", indexedTriangleModel(t), types.StandardVertexAttrs, types.StorageStride},
		{"a skinned primitive", skinnedDoc(), 8, types.StorageSkinnedStride},
	} {
		h := residentModel(t, c.doc)
		var attrs, bytes, vertices int
		found := 0
		h.kernel.ExecuteCommand[lookupProbeCmd](lookupProbeRequest{lookup: func(lookup *scene.Lookup) {
			for i := range types.LookupMeshes(lookup) {
				if types.LookupMeshes(lookup)[i].VertexCount == 0 {
					continue
				}
				found++
				attrs = len(types.LookupMeshes(lookup)[i].Layout)
				bytes = types.LookupMeshes(lookup)[i].Vertices.Size()
				vertices = types.LookupMeshes(lookup)[i].VertexCount
			}
		}})
		if found != 1 {
			t.Fatalf("%s: the load left %d meshes, want the one", c.what, found)
		}
		if attrs != c.attrs {
			t.Errorf("%s supplies %d attributes, want %d", c.what, attrs, c.attrs)
		}
		if bytes != vertices*c.stride {
			t.Errorf("%s stores %d bytes for %d vertices, want %d a vertex",
				c.what, bytes, vertices, c.stride)
		}
	}
}
