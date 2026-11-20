package canvas

import (
	"cmp"
	"fmt"
	"reflect"
	"sync"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/render"
)

// materialTable is the 2D materials Assets.MakeMaterial made, each at its
// Material's index. Assets and the drawing job both reach it under their
// read locks of the draw state, so it is guarded by a lock of its own.
type materialTable struct {
	mutex sync.Mutex
	// slots are the materials made, by index. A freed slot is reused, its
	// generation counted up, so a Material naming it before names nothing,
	// and a generation is never zero, so the zero Material, the default
	// material, names no slot.
	slots       []materialSlot
	freeIndexes []uint32
	// releasedIndexes are the slots of the materials released whose render
	// material and shader are still to give back, which the drawing job does
	// once it has drawn the frame, so the frame recorded as a material is
	// released still draws it.
	releasedIndexes []uint32
}

// make makes a 2D material of descriptor through inventory, and answers it,
// its argument's layout checked against its shader's, a mismatch reported
// once to logger for the shader and the type.
func (t *materialTable) make(inventory render.Inventory, logger *kernel.Logger, descriptor MaterialDescriptor) Material {
	material, shader := descriptor.makeRenderMaterial(inventory)
	slot := materialSlot{material: material, shader: shader, cullMargin: descriptor.CullMargin, argumentType: descriptor.Argument.goType, stride: findInstanceStride(descriptor.Argument.goType)}
	if slot.argumentType != nil {
		if err := descriptor.Argument.checkLayout(inventory, shader); err != nil {
			slot.argumentLayoutMismatch = true
			mismatch := Report{Kind: ReportKindArgumentLayoutMismatch, Err: fmt.Errorf("canvas: the argument %v of a material of shader %q does not match the shader's struct Argument, so its entries are skipped: %w", slot.argumentType, descriptor.Shader, err)}
			logger.LogErrOnce(argumentLayoutMismatchLogKey+descriptor.Shader+" "+slot.argumentType.String(), mismatch)
		}
	}
	t.mutex.Lock()
	defer t.mutex.Unlock()
	if len(t.freeIndexes) == 0 {
		t.slots = append(t.slots, materialSlot{generation: 1})
		t.freeIndexes = append(t.freeIndexes, uint32(len(t.slots)-1))
	}
	index := t.freeIndexes[len(t.freeIndexes)-1]
	t.freeIndexes = t.freeIndexes[:len(t.freeIndexes)-1]
	slot.generation = t.slots[index].generation
	t.slots[index] = slot
	return Material{index: index, generation: slot.generation}
}

// findHeld answers material's render material, and whether it is held: made
// and not released.
func (t *materialTable) findHeld(material Material) (render.Material, bool) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	slot, found := findSlot(t.slots, material)
	if !found || slot.released {
		return render.Material{}, false
	}
	return slot.material, true
}

// release marks material released, its render material and shader given
// back by freeReleased, and answers whether it was held.
func (t *materialTable) release(material Material) bool {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	slot, found := findSlot(t.slots, material)
	if !found || slot.released {
		return false
	}
	slot.released = true
	t.releasedIndexes = append(t.releasedIndexes, material.index)
	return true
}

// freeReleased gives back through inventory the render material and the
// shader of each material released, and frees its slot, so a Material
// naming it names nothing.
func (t *materialTable) freeReleased(inventory render.Inventory) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	for _, index := range t.releasedIndexes {
		slot := &t.slots[index]
		inventory.ReleaseMaterial(slot.material)
		inventory.ReleaseShader(slot.shader)
		// A generation counted past the last wraps to 1, never zero.
		*slot = materialSlot{generation: cmp.Or(slot.generation+1, 1)}
		t.freeIndexes = append(t.freeIndexes, index)
	}
	t.releasedIndexes = t.releasedIndexes[:0]
}

// appendSlots appends every slot to slots, as they stand, and answers it.
func (t *materialTable) appendSlots(slots []materialSlot) []materialSlot {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	return append(slots, t.slots...)
}

// reportArgumentRefused reports to logger once for material and given, the
// type of an entry's argument, that material, taking an argument of type
// taken, nil for none, refused it.
func reportArgumentRefused(logger *kernel.Logger, material Material, given, taken reflect.Type) {
	takes := "takes none"
	if taken != nil {
		takes = "takes a " + taken.String()
	}
	refusal := Report{Kind: ReportKindArgumentRefused, Err: fmt.Errorf("canvas: an entry gave material %v an argument of %v, which it refuses, as it %s, so the entry is skipped", material, given, takes)}
	logger.LogErrOnce(argumentRefusedLogKey+fmt.Sprint(material)+" "+given.String(), refusal)
}

// reportMaterialNotHeld reports material to logger once, refused as use,
// what was done with it, such as drawing an entry with it, says: a material
// canvas does not hold, or the default material, whose parameters are
// canvas's.
func reportMaterialNotHeld(logger *kernel.Logger, material Material, use string) {
	refusal := Report{Kind: ReportKindMaterialNotHeld, Err: fmt.Errorf("canvas: %s material %v, which canvas does not hold: never made by Assets.MakeMaterial, released, or the default material, whose parameters are canvas's", use, material)}
	logger.LogErrOnce(materialNotHeldLogKey+fmt.Sprint(material), refusal)
}
