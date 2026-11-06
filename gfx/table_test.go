package gfx

import (
	"errors"
	"sync"
	"testing"
)

func TestTableAnswersTheDescriptorOfALiveHandle(t *testing.T) {
	var buffers table[Buffer, BufferDescriptor]
	buffer := buffers.acquire(BufferDescriptor{Label: "a", Size: 16})

	descriptor, err := buffers.descriptor(buffer)
	if err != nil || descriptor != (BufferDescriptor{Label: "a", Size: 16}) {
		t.Fatalf("descriptor = %+v, %v, want the acquired one", descriptor, err)
	}
}

func TestTableRetiredHandleIsReleased(t *testing.T) {
	var buffers table[Buffer, BufferDescriptor]
	buffer := buffers.acquire(BufferDescriptor{Size: 16})
	if err := buffers.retire(buffer); err != nil {
		t.Fatalf("first retire answered %v", err)
	}

	if _, err := buffers.descriptor(buffer); !errors.Is(err, ErrReleased) {
		t.Errorf("descriptor answered %v, want ErrReleased", err)
	}
	if err := buffers.retire(buffer); !errors.Is(err, ErrReleased) {
		t.Errorf("second retire answered %v, want ErrReleased", err)
	}
}

func TestTableZeroHandleIsReleased(t *testing.T) {
	var buffers table[Buffer, BufferDescriptor]
	buffers.acquire(BufferDescriptor{Size: 16})

	if _, err := buffers.descriptor(Buffer{}); !errors.Is(err, ErrReleased) {
		t.Fatalf("descriptor of the zero handle answered %v, want ErrReleased", err)
	}
}

func TestTableChecksWhetherAHandleIsLive(t *testing.T) {
	var buffers table[Buffer, BufferDescriptor]
	buffer := buffers.acquire(BufferDescriptor{Size: 16})
	if err := buffers.check(buffer); err != nil {
		t.Fatalf("check of a live handle answered %v", err)
	}
	if err := buffers.retire(buffer); err != nil {
		t.Fatal(err)
	}

	if err := buffers.check(buffer); !errors.Is(err, ErrReleased) {
		t.Errorf("check of a retired handle answered %v, want ErrReleased", err)
	}
	if err := buffers.check(Buffer{}); !errors.Is(err, ErrReleased) {
		t.Errorf("check of the zero handle answered %v, want ErrReleased", err)
	}
}

func TestTableReusesAReleasedIndexUnderANewGeneration(t *testing.T) {
	var buffers table[Buffer, BufferDescriptor]
	first := buffers.acquire(BufferDescriptor{Size: 16})
	if err := buffers.retire(first); err != nil {
		t.Fatal(err)
	}
	buffers.release(first)

	second := buffers.acquire(BufferDescriptor{Size: 32})
	if second.Index() != first.Index() || second == first {
		t.Fatalf("second = %+v, want index %d under a new generation than %+v", second, first.Index(), first)
	}
	if _, err := buffers.descriptor(first); !errors.Is(err, ErrReleased) {
		t.Errorf("the first handle answered %v, want ErrReleased", err)
	}
}

func TestTableRetiredIndexIsNotReusedBeforeRelease(t *testing.T) {
	var buffers table[Buffer, BufferDescriptor]
	first := buffers.acquire(BufferDescriptor{Size: 16})
	if err := buffers.retire(first); err != nil {
		t.Fatal(err)
	}

	if second := buffers.acquire(BufferDescriptor{Size: 32}); second.Index() == first.Index() {
		t.Fatalf("index %d was handed out again before its release", first.Index())
	}
}

func TestTableHandlesFromParallelGoroutinesAreDistinct(t *testing.T) {
	const goroutineCount, perGoroutine = 8, 200
	var buffers table[Buffer, BufferDescriptor]
	handles := make([][]Buffer, goroutineCount)
	var group sync.WaitGroup
	for g := range goroutineCount {
		group.Add(1)
		go func() {
			defer group.Done()
			for i := range perGoroutine {
				buffer := buffers.acquire(BufferDescriptor{Size: uint64(i)})
				if i%2 == 1 {
					if err := buffers.retire(buffer); err != nil {
						t.Error(err)
					}
					buffers.release(buffer)
					continue
				}
				handles[g] = append(handles[g], buffer)
			}
		}()
	}
	group.Wait()

	seen := map[uint32]bool{}
	for _, list := range handles {
		for _, buffer := range list {
			if seen[buffer.Index()] {
				t.Fatalf("index %d is held by two live handles", buffer.Index())
			}
			seen[buffer.Index()] = true
			if _, err := buffers.descriptor(buffer); err != nil {
				t.Fatalf("live handle %+v answered %v", buffer, err)
			}
		}
	}
}
