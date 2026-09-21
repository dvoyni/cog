package model

import (
	"io/fs"

	"github.com/dvoyni/cog/bundles/model/internal/types"
	"github.com/dvoyni/cog/slots/gfx"
	"github.com/qmuntal/gltf"
)

// SkinnedVertexLayout reports the skinned storage layout: the standard layout's
// six rows plus JOINTS_0 and WEIGHTS_0.
func SkinnedVertexLayout() []gfx.VertexAttr { return types.SkinnedVertexLayout() }

// UnitBoxGeometry builds the 1x1x1 cube centred on the origin, four vertices a
// face so every face keeps its own flat normal, wound counter-clockwise seen
// from outside.
func UnitBoxGeometry() ([]Vertex, []uint32) { return types.UnitBoxGeometry() }

// UnitPlaneGeometry builds the 1x1 square in the XZ plane centred on the
// origin, facing +Y, and its mirror facing -Y.
func UnitPlaneGeometry() ([]Vertex, []uint32) { return types.UnitPlaneGeometry() }

// UnitSphereGeometry builds the radius-1 UV sphere of 16 segments by 12 rings.
func UnitSphereGeometry() ([]Vertex, []uint32) { return types.UnitSphereGeometry() }

// DecodeModel parses and decodes one glTF or GLB file's bytes. modelPath is the
// file's storage path: a .gltf file's relative buffer URIs resolve against its
// directory in fsys, external images are named against it, and every report
// names the model by it. Images are named and never opened.
func DecodeModel(data []byte, modelPath string, fsys fs.FS) (*DecodedModel, error) {
	return types.DecodeModel(data, modelPath, fsys)
}

// DecodeDocument decodes one already parsed glTF document, opening nothing.
func DecodeDocument(doc *gltf.Document, modelPath string) (*DecodedModel, error) {
	return types.DecodeDocument(doc, modelPath)
}
