package handle_test

import (
	"testing"

	"github.com/dvoyni/cog/render/internal/handle"
)

func TestASlotMapFindsAHeldSlotByItsHandleAndNothingByAReleasedOrZeroHandle(t *testing.T) {
	var slots handle.SlotMap[handle.Texture, int]
	first, slot := slots.Acquire()
	*slot = 7

	if slot, ok := slots.Find(first); !ok || *slot != 7 {
		t.Errorf("finding a held handle answered %v, %v, want its slot holding 7", slot, ok)
	}
	slots.Release(first)
	if _, ok := slots.Find(first); ok {
		t.Errorf("finding a released handle found a slot, want none")
	}
	second, slot := slots.Acquire()
	if second == first || *slot != 7 {
		t.Errorf("acquiring after a release answered %v holding %d, want a handle other than %v to the released slot, holding 7", second, *slot, first)
	}
	if _, ok := slots.Find(first); ok {
		t.Errorf("finding a released handle whose slot was handed out again found it, want none")
	}
	if _, ok := slots.Find(handle.Texture{}); ok {
		t.Errorf("finding the zero handle found a slot, want none")
	}
}

func TestAHandleParsedFromTheLabelMadeForItIsItself(t *testing.T) {
	var slots handle.SlotMap[handle.Texture, int]
	slots.Acquire()
	texture, _ := slots.Acquire()

	if parsed, ok := handle.ParseLabel[handle.Texture](handle.MakeLabel(texture, "bloom: half")); !ok || parsed != texture {
		t.Errorf("parsing the label made for %v answered %v, %v, want it", texture, parsed, ok)
	}
	if parsed, ok := handle.ParseLabel[handle.Texture]("bloom"); ok {
		t.Errorf("parsing a label render did not make answered %v, want none", parsed)
	}
}
