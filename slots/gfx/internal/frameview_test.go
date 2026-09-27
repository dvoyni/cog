package internal

import (
	"github.com/dvoyni/cog/libs/assets"
	"testing"

	"github.com/dvoyni/cog/slots/gfx/internal/types"

	"github.com/dvoyni/cog/libs/m"
)

func TestTheNewAccessorsAnswerOutsideTheAgentPath(t *testing.T) {
	// They are ordinary gfx API, not a debug back door: nothing here goes near
	// a view, a capability or a JSON document.
	vertices := types.BufferDescrWithBlob(assets.NewBlob(make([]byte, 3*32)), false)
	indices := types.BufferDescrWithBlob(assets.NewBlob(make([]byte, 6*4)), false)
	mesh := types.MeshDescrWithIndices(vertices, indices, types.IndexUint32, types.TopologyTriangleList,
		types.VertexAttribute{Offset: 0, Type: types.Float32x4}, types.VertexAttribute{Offset: 16, Type: types.Float32x4})
	if mesh.VertexCount != 3 || mesh.IndexCount != 6 {
		t.Errorf("counts = %d vertices / %d indices, want 3 and 6",
			mesh.VertexCount, mesh.IndexCount)
	}
	if mesh.Topology != types.TopologyTriangleList {
		t.Errorf("topology = %v, want a triangle list", mesh.Topology)
	}
	if vertices.Size != 96 || vertices.Bytes.Len() != 96 || vertices.ID != 0 {
		t.Errorf("buffer = %d bytes / %d inline / id %d, want 96, 96 and no baked id",
			vertices.Size, vertices.Bytes.Len(), vertices.ID)
	}

	texture := types.TextureWithBytes(8, 4, types.FormatRGBA8Srgb, make([]byte, 128), false, true)
	if texture.Params.Format != types.FormatRGBA8Srgb || !texture.Params.Mipmaps || texture.Blob.Len() != 128 {
		t.Errorf("texture = format %v / mipmaps %v / %d bytes, want what it was built with",
			texture.Params.Format, texture.Params.Mipmaps, texture.Blob.Len())
	}

	// Each value accessor is keyed on the parameter's own kind, which is what
	// makes reading one arm of the union for another impossible rather than
	// merely unlikely.
	matrix := types.ShaderParameterMat4("mvp", m.NewMat4())
	if _, ok := matrix.MatValue(); !ok {
		t.Error("MatValue refused a mat4 parameter")
	}
}
