package canvas

import (
	"reflect"
	"slices"
	"unsafe"

	"github.com/dvoyni/cog/plugins/render"
)

// listInstanceData is the instance data of the command list being drawn, in
// temporary storage buffers, one per stride, so entries of every material
// share them: fixed, of the entries of materials taking no argument and of
// the quads canvas draws over viewports and into cells, and a stream for
// each stride of the materials taking one. Each is written over for each
// command list.
type listInstanceData struct {
	fixed      []instanceData
	fixedRange render.BufferRange
	streams    []instanceStream
}

// instanceStream is the instance data of the entries of materials taking an
// argument whose instance data is stride bytes: canvas's fixed part, then
// the argument, then zeros to the stride, WGSL's of CanvasInstanceData with
// the argument in it; and the temporary buffer it was copied into.
type instanceStream struct {
	stride      uint32
	data        []byte
	bufferRange render.BufferRange
}

// findInstanceStride answers the stride of the instance data of a material
// whose argument is of argumentType, zero for one taking none, whose
// instances are the fixed ones: canvas's fixed part, then the argument, at
// the fixed part's end, which is aligned to 16, the most any WGSL type
// aligns to, the whole rounded up to 16, the alignment of the fixed part's
// vec4<f32> fields.
func findInstanceStride(argumentType reflect.Type) uint32 {
	if argumentType == nil {
		return 0
	}
	return instanceDataSize + roundUpToMultiple(findArgumentSize(argumentType), 16)
}

// findArgumentSize answers the size of an argument of argumentType, zero
// for none, nil.
func findArgumentSize(argumentType reflect.Type) uint32 {
	if argumentType == nil {
		return 0
	}
	return uint32(argumentType.Size())
}

// castToBytes answers the bytes of values, as Go lays them out, sharing their
// memory.
func castToBytes[T any](values []T) []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(unsafe.SliceData(values))), uintptr(len(values))*unsafe.Sizeof(*new(T)))
}

// reset empties the instance data, keeping its storage.
func (f *listInstanceData) reset() {
	f.fixed = f.fixed[:0]
	for index := range f.streams {
		f.streams[index].data = f.streams[index].data[:0]
	}
}

// count answers how many instances there are of stride, zero for the fixed
// ones: the fixed instances', or its stream's.
func (f *listInstanceData) count(stride uint32) uint32 {
	if stride == 0 {
		return uint32(len(f.fixed))
	}
	stream := f.findStream(stride)
	if stream == nil {
		return 0
	}
	return uint32(len(stream.data)) / stride
}

// moveToStream moves the fixed instances from first on into the stream of
// stride, added when there is none, each followed by argument, or zeros
// for none, and zeros to the stride.
func (f *listInstanceData) moveToStream(first int, stride uint32, argument []byte) {
	stream := f.findStream(stride)
	if stream == nil {
		f.streams = append(f.streams, instanceStream{stride: stride})
		stream = &f.streams[len(f.streams)-1]
	}
	moved := f.fixed[first:]
	start := len(stream.data)
	stream.data = slices.Grow(stream.data, len(moved)*int(stride))[:start+len(moved)*int(stride)]
	for index := range moved {
		row := stream.data[start+index*int(stride):][:stride]
		copy(row, castToBytes(moved[index:index+1]))
		written := copy(row[instanceDataSize:], argument)
		clear(row[instanceDataSize+uint32(written):])
	}
	f.fixed = f.fixed[:first]
}

// findStream answers the stream of stride, nil when there is none.
func (f *listInstanceData) findStream(stride uint32) *instanceStream {
	for index := range f.streams {
		if f.streams[index].stride == stride {
			return &f.streams[index]
		}
	}
	return nil
}

// acquireBuffers copies the instance data into temporary buffers of list,
// one per stride, a stride of no instances into none.
func (f *listInstanceData) acquireBuffers(list *render.CommandList) {
	f.fixedRange = list.AcquireTemporaryBuffer(f.fixed)
	for index := range f.streams {
		f.streams[index].bufferRange = list.AcquireTemporaryBuffer(f.streams[index].data)
	}
}

// findRange answers the temporary buffer holding the instances of stride,
// zero for the fixed ones, once acquireBuffers has copied them.
func (f *listInstanceData) findRange(stride uint32) render.BufferRange {
	if stride == 0 {
		return f.fixedRange
	}
	return f.findStream(stride).bufferRange
}
