package meshfile

// IndexFormat is the type of a mesh's indices, each WebGPU's index format of
// the same name, and its value that of gfx's and render's. Its zero value is
// no indices.
type IndexFormat uint32

const (
	IndexFormatUint16 IndexFormat = 0x1
	IndexFormatUint32 IndexFormat = 0x2
)

// size answers how many bytes an index of format f takes; zero for no
// indices, or a format there is none of.
func (f IndexFormat) size() uint64 {
	switch f {
	case IndexFormatUint16:
		return 2
	case IndexFormatUint32:
		return 4
	}
	return 0
}
