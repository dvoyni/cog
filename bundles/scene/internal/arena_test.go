package internal

import (
	"testing"
	"unsafe"

	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/slots/gfx"
)

func TestArenaAlignsEveryRecordToTheStorageAlignment(t *testing.T) {
	var a arena
	first := a.appendRecord(&model.Instance{})
	second := a.appendRecord(&model.FrameBlock{})
	if first != 0 {
		t.Fatalf("first record is at %d, want 0", first)
	}
	if second%gfx.StorageAlignment != 0 {
		t.Fatalf("second record is at %d, which is not %d-aligned", second, gfx.StorageAlignment)
	}
	if len(a.bytes()) < second+int(unsafe.Sizeof(model.FrameBlock{})) {
		t.Fatalf("arena is %d bytes, too short for the record at %d", len(a.bytes()), second)
	}
}

func TestArenaReusesItsBackingAcrossFrames(t *testing.T) {
	var a arena
	a.appendRecord(&model.FrameBlock{})
	backing := unsafe.SliceData(a.data[:cap(a.data)])
	a.reset()
	a.appendRecord(&model.FrameBlock{})
	if unsafe.SliceData(a.data[:cap(a.data)]) != backing {
		t.Fatal("the arena reallocated instead of reusing its backing")
	}
}
