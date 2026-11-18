package meshfile

// Layout is how a mesh's vertices are laid out: Stride bytes apart, each
// holding Attributes.
type Layout struct {
	Stride     uint32
	Attributes []Attribute
}

// Attribute is one value each vertex holds: in Format, Offset bytes into the
// vertex, fed to the vertex stage's input at Location.
type Attribute struct {
	Format   Format
	Offset   uint32
	Location uint32
}

// Format is the type of one vertex attribute's value: its components' count,
// size and type, and how the vertex stage reads them, as a float, normalized
// or not, or an integer. Each is WebGPU's vertex format of the same name, and
// its value that of gfx's and render's, so a reader translates it by
// conversion.
type Format uint32

const (
	FormatUint8x2      Format = 0x1
	FormatUint8x4      Format = 0x2
	FormatSint8x2      Format = 0x3
	FormatSint8x4      Format = 0x4
	FormatUnorm8x2     Format = 0x5
	FormatUnorm8x4     Format = 0x6
	FormatSnorm8x2     Format = 0x7
	FormatSnorm8x4     Format = 0x8
	FormatUint16x2     Format = 0x9
	FormatUint16x4     Format = 0xA
	FormatSint16x2     Format = 0xB
	FormatSint16x4     Format = 0xC
	FormatUnorm16x2    Format = 0xD
	FormatUnorm16x4    Format = 0xE
	FormatSnorm16x2    Format = 0xF
	FormatSnorm16x4    Format = 0x10
	FormatFloat16x2    Format = 0x11
	FormatFloat16x4    Format = 0x12
	FormatFloat32      Format = 0x13
	FormatFloat32x2    Format = 0x14
	FormatFloat32x3    Format = 0x15
	FormatFloat32x4    Format = 0x16
	FormatUint32       Format = 0x17
	FormatUint32x2     Format = 0x18
	FormatUint32x3     Format = 0x19
	FormatUint32x4     Format = 0x1A
	FormatSint32       Format = 0x1B
	FormatSint32x2     Format = 0x1C
	FormatSint32x3     Format = 0x1D
	FormatSint32x4     Format = 0x1E
	FormatUnorm1010102 Format = 0x1F
)

// VertexType is a vertex type that answers its own layout, whatever its
// value: a plain struct of the values its attributes name, holding no
// pointers, whose layout's stride is its size.
type VertexType interface {
	VertexLayout() Layout
}
