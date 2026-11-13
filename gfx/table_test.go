package gfx

import (
	"errors"
	"maps"
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

func TestTableKeepsAReleasedSlotWhileAnObjectMadeFromItIsHeld(t *testing.T) {
	var modules table[ShaderModule, ShaderModuleDescriptor]
	var pipelines table[ComputePipeline, pipelineSource[ComputePipelineDescriptor]]
	module := modules.acquire(ShaderModuleDescriptor{Label: "a"})
	pipeline := pipelines.acquireDependent(t, pipelineSource[ComputePipelineDescriptor]{}, dependency{&modules, module.handle})
	modules.retireAndRelease(t, module)

	if err := modules.check(module); !errors.Is(err, ErrReleased) {
		t.Errorf("the released module answered %v, want ErrReleased", err)
	}
	if _, err := modules.descriptor(module); !errors.Is(err, ErrReleased) {
		t.Errorf("the released module's descriptor answered %v, want ErrReleased", err)
	}
	if otherModule := modules.acquire(ShaderModuleDescriptor{Label: "b"}); otherModule.Index() == module.Index() {
		t.Fatalf("index %d was handed out again while a pipeline made from it is held", module.Index())
	}
	keptModules := maps.Collect(modules.handles(slotStateKept))
	if want := map[ShaderModule]ShaderModuleDescriptor{module: {Label: "a"}}; !maps.Equal(keptModules, want) {
		t.Errorf("kept %v, want %v", keptModules, want)
	}

	pipelines.retireAndRelease(t, pipeline)

	if keptModules := maps.Collect(modules.handles(slotStateKept)); len(keptModules) != 0 {
		t.Errorf("kept %v once the pipeline was released, want none", keptModules)
	}
	if nextModule := modules.acquire(ShaderModuleDescriptor{Label: "c"}); nextModule.Index() != module.Index() || nextModule == module {
		t.Errorf("acquired %+v, want index %d under a new generation than %+v", nextModule, module.Index(), module)
	}
}

func TestTableFreesAKeptSlotThroughWhatWasMadeFromIt(t *testing.T) {
	var groupLayouts table[BindGroupLayout, BindGroupLayoutDescriptor]
	var layouts table[PipelineLayout, PipelineLayoutDescriptor]
	var pipelines table[ComputePipeline, pipelineSource[ComputePipelineDescriptor]]
	groupLayout := groupLayouts.acquire(BindGroupLayoutDescriptor{Label: "camera"})
	layout := layouts.acquireDependent(t, PipelineLayoutDescriptor{BindGroupLayouts: []BindGroupLayout{groupLayout}}, dependency{&groupLayouts, groupLayout.handle})
	pipeline := pipelines.acquireDependent(t, pipelineSource[ComputePipelineDescriptor]{}, dependency{&layouts, layout.handle})
	groupLayouts.retireAndRelease(t, groupLayout)
	layouts.retireAndRelease(t, layout)
	if keptGroupLayouts := maps.Collect(groupLayouts.handles(slotStateKept)); len(keptGroupLayouts) != 1 {
		t.Fatalf("kept group layouts %v, want the one the kept layout is made from", keptGroupLayouts)
	}

	pipelines.retireAndRelease(t, pipeline)

	if keptLayouts := maps.Collect(layouts.handles(slotStateKept)); len(keptLayouts) != 0 {
		t.Errorf("kept layouts %v once the pipeline was released, want none", keptLayouts)
	}
	if keptGroupLayouts := maps.Collect(groupLayouts.handles(slotStateKept)); len(keptGroupLayouts) != 0 {
		t.Errorf("kept group layouts %v once the layout made from them was freed, want none", keptGroupLayouts)
	}
	if nextGroupLayout := groupLayouts.acquire(BindGroupLayoutDescriptor{}); nextGroupLayout.Index() != groupLayout.Index() {
		t.Errorf("acquired index %d, want %d handed out again", nextGroupLayout.Index(), groupLayout.Index())
	}
}

func TestTableAddsNoDependentToAStaleHandleAndUndoesTheOnesAdded(t *testing.T) {
	var modules table[ShaderModule, ShaderModuleDescriptor]
	liveModule := modules.acquire(ShaderModuleDescriptor{Label: "live"})
	staleModule := modules.acquire(ShaderModuleDescriptor{Label: "stale"})
	modules.retireAndRelease(t, staleModule)

	if err := addDependents([]dependency{{&modules, liveModule.handle}, {&modules, staleModule.handle}}); !errors.Is(err, ErrReleased) {
		t.Fatalf("adding a dependent to a stale handle answered %v, want ErrReleased", err)
	}

	modules.retireAndRelease(t, liveModule)
	if keptModules := maps.Collect(modules.handles(slotStateKept)); len(keptModules) != 0 {
		t.Errorf("kept %v, want the live module freed: its dependent was undone", keptModules)
	}
}

func TestTableHandsAnIndexOutOnceWhenItsLastDependentGoesBetweenRetireAndRelease(t *testing.T) {
	var modules table[ShaderModule, ShaderModuleDescriptor]
	var pipelines table[ComputePipeline, pipelineSource[ComputePipelineDescriptor]]
	module := modules.acquire(ShaderModuleDescriptor{})
	pipeline := pipelines.acquireDependent(t, pipelineSource[ComputePipelineDescriptor]{}, dependency{&modules, module.handle})
	if err := modules.retire(module); err != nil {
		t.Fatal(err)
	}
	pipelines.retireAndRelease(t, pipeline)

	modules.release(module)

	firstModule := modules.acquire(ShaderModuleDescriptor{})
	if secondModule := modules.acquire(ShaderModuleDescriptor{}); firstModule.Index() == secondModule.Index() {
		t.Fatalf("index %d was handed out twice", firstModule.Index())
	}
}

func TestTableReleasingAHandleNotRetiredPanics(t *testing.T) {
	var modules table[ShaderModule, ShaderModuleDescriptor]
	module := modules.acquire(ShaderModuleDescriptor{})
	defer func() {
		if recover() == nil {
			t.Error("releasing a held handle did not panic")
		}
	}()

	modules.release(module)
}

func TestTableKeepsAndFreesSlotsFromParallelGoroutines(t *testing.T) {
	const goroutineCount, perGoroutine = 8, 200
	var modules table[ShaderModule, ShaderModuleDescriptor]
	var pipelines table[ComputePipeline, pipelineSource[ComputePipelineDescriptor]]
	var group sync.WaitGroup
	for range goroutineCount {
		group.Go(func() {
			for range perGoroutine {
				module := modules.acquire(ShaderModuleDescriptor{})
				dependencies := []dependency{{&modules, module.handle}}
				if err := addDependents(dependencies); err != nil {
					t.Error(err)
					return
				}
				pipeline := pipelines.acquire(pipelineSource[ComputePipelineDescriptor]{}, dependencies...)
				var releaseGroup sync.WaitGroup
				releaseGroup.Go(func() {
					_ = modules.retire(module)
					modules.release(module)
				})
				releaseGroup.Go(func() {
					_ = pipelines.retire(pipeline)
					pipelines.release(pipeline)
				})
				releaseGroup.Wait()
			}
		})
	}
	group.Wait()

	if keptModules := maps.Collect(modules.handles(slotStateKept)); len(keptModules) != 0 {
		t.Errorf("kept %d modules, want none: every pipeline made from one was released", len(keptModules))
	}
	if freeCount := len(modules.freeIndices); freeCount != len(modules.slots) {
		t.Errorf("%d of %d slots free, want every one", freeCount, len(modules.slots))
	}
	seen := map[uint32]bool{}
	for range len(modules.slots) {
		index := modules.acquire(ShaderModuleDescriptor{}).Index()
		if seen[index] {
			t.Fatalf("index %d was handed out twice", index)
		}
		seen[index] = true
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

// acquireDependent acquires a handle holding descriptor, made from
// dependencies, as createResource does.
func (t *table[H, D]) acquireDependent(test *testing.T, descriptor D, dependencies ...dependency) H {
	test.Helper()
	if err := addDependents(dependencies); err != nil {
		test.Fatal(err)
	}
	return t.acquire(descriptor, dependencies...)
}

// retireAndRelease retires h, then releases it, as releaseResource does.
func (t *table[H, D]) retireAndRelease(test *testing.T, h H) {
	test.Helper()
	if err := t.retire(h); err != nil {
		test.Fatal(err)
	}
	t.release(h)
}
