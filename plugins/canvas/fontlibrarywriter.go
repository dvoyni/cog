package canvas

import (
	"cmp"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/platform"
)

// fontLibraryWriter is the FontLibrary resource's writing side, which ElevateFontLibrary
// answers from the write handle on it: the table's reads, and its writes,
// with platform's FS, which making a set reads.
type fontLibraryWriter struct {
	*fontLibrary
	files platform.FS
}

// MakeFontSet makes a font set of descriptor, loading every font file it
// names through platform's FS now, in the calling task, and answers it.
func (w fontLibraryWriter) MakeFontSet(descriptor FontSetDescriptor) FontSet {
	slot := makeFontSetSlot(w.files, w.logger, descriptor)
	if len(w.freeIndexes) == 0 {
		w.sets = append(w.sets, fontSetSlot{generation: 1})
		w.freeIndexes = append(w.freeIndexes, uint32(len(w.sets)-1))
	}
	index := w.freeIndexes[len(w.freeIndexes)-1]
	w.freeIndexes = w.freeIndexes[:len(w.freeIndexes)-1]
	slot.generation = w.sets[index].generation
	w.sets[index] = slot
	return FontSet{index: index, generation: slot.generation}
}

// DestroyFontSet destroys set, freeing its slot; a set canvas does not hold,
// or the default set, is reported once instead.
func (w fontLibraryWriter) DestroyFontSet(set FontSet) {
	if set == (FontSet{}) || w.findSlot(set) == nil {
		w.reportSetNotHeld(set, "destroying")
		return
	}
	// A generation counted past the last wraps to 1, never zero.
	w.sets[set.index] = fontSetSlot{generation: cmp.Or(set.generation+1, 1)}
	w.freeIndexes = append(w.freeIndexes, set.index)
}

// SetLanguage sets the current language to tag.
func (w fontLibraryWriter) SetLanguage(tag string) { w.language = tag }

// RegisterIcon registers icon under name, replacing an icon registered under
// it before.
func (w fontLibraryWriter) RegisterIcon(name string, icon IconDescriptor) { w.icons[name] = icon }

// RegisterColor registers color under name, replacing a colour registered
// under it before.
func (w fontLibraryWriter) RegisterColor(name string, color m.Color) { w.colors[name] = color }
