package canvas

import (
	"cmp"
	"fmt"
	"slices"
	"sync"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/canvas/font"
	"github.com/dvoyni/cog/plugins/platform"
)

// fontLibraryWriter is the FontLibrary resource's writing side, which ElevateFontLibrary
// answers from the write handle on it: the table's reads, and its writes,
// with platform's FS, which making a set reads. Every way a set is replaced
// goes through its replace.
type fontLibraryWriter struct {
	*fontLibrary
	files platform.FS
}

// makeFontLibraryWriter answers library's writing side, its sets made
// through files: ElevateFontLibrary's, the default set's and a reload's.
func makeFontLibraryWriter(library *fontLibrary, files platform.FS) fontLibraryWriter {
	return fontLibraryWriter{fontLibrary: library, files: files}
}

// MakeFontSet makes a font set of descriptor, loading every font file it
// names through platform's FS now, in the calling task, at a free index,
// and answers it.
func (w fontLibraryWriter) MakeFontSet(descriptor font.SetDescriptor) FontSet {
	slot := makeFontSetSlot(w.files, w.reporter, descriptor)
	if len(w.freeIndexes) == 0 {
		w.sets = append(w.sets, &fontSetSlot{set: FontSet{index: uint32(len(w.sets)), generation: 1}})
		w.freeIndexes = append(w.freeIndexes, uint32(len(w.sets)-1))
	}
	index := w.freeIndexes[len(w.freeIndexes)-1]
	w.freeIndexes = w.freeIndexes[:len(w.freeIndexes)-1]
	w.replace(w.sets[index].set, slot)
	return slot.set
}

// DestroyFontSet destroys set, replacing its slot with an empty one under
// the next generation, so a FontSet naming it names nothing, and frees its
// index; its glyphs are let go once the frame is drawn. A set canvas does
// not hold, or the default set, is reported once instead.
func (w fontLibraryWriter) DestroyFontSet(set FontSet) {
	if set == (FontSet{}) || w.findSlot(set) == nil {
		w.reportSetNotHeld(set, "destroying")
		return
	}
	// A generation counted past the last wraps to 1, never zero.
	w.replace(FontSet{index: set.index, generation: cmp.Or(set.generation+1, 1)}, &fontSetSlot{})
	w.freeIndexes = append(w.freeIndexes, set.index)
}

// SetLanguage sets the current language to tag.
func (w fontLibraryWriter) SetLanguage(tag string) { w.language = tag }

// RegisterIcon registers icon under name, replacing an icon registered under
// it before, under a serial of its own, which the layout cache's key holds,
// so the layouts holding the name are laid out again and those before go
// idle: its image's frame's size read from its file's header through
// platform's FS. An icon whose image the text material cannot draw, or
// whose header cannot be read, is refused, reported once for the name, and
// the name left unregistered.
func (w fontLibraryWriter) RegisterIcon(name string, icon IconDescriptor) {
	frameSize, err := findIconFrameSize(w.files, icon)
	if err != nil {
		delete(w.icons, name)
		refusal := Report{Kind: ReportKindIconRefused, Err: fmt.Errorf("canvas: registering icon %q: %w, so the name is left unregistered", name, err)}
		w.reporter.reportOnce(iconRefusedLogKey+name, refusal)
		return
	}
	w.iconRegistrationCount++
	w.icons[name] = registeredIcon{descriptor: icon, frameSize: frameSize, serial: w.iconRegistrationCount}
	w.reporter.forget(iconRefusedLogKey + name)
	w.reporter.forget(iconNotRegisteredLogKey + name)
}

// RegisterColor registers color under name, replacing a colour registered
// under it before, which the markup read after takes.
func (w fontLibraryWriter) RegisterColor(name string, color m.Color) { w.colors[name] = color }

// loadDefaultSet loads the default set of Config.DefaultFontSet, under the
// zero FontSet, replacing the JetBrains Mono it held until canvas started.
func (w fontLibraryWriter) loadDefaultSet() {
	w.replace(FontSet{}, makeFontSetSlot(w.files, w.reporter, w.defaultDescriptor))
}

// reloadChanged makes again each set naming a font file that changed since
// the set was made, of its descriptor, as MakeFontSet makes it, under the
// same FontSet, so its runs are laid out anew, the layouts of the slot
// before going idle, and its old fonts' glyphs are let go once the frame
// is drawn. A set destroyed names no file.
func (w fontLibraryWriter) reloadChanged() {
	for _, slot := range w.sets {
		if slot.namesChangedFile(w.files) {
			w.replace(slot.set, makeFontSetSlot(w.files, w.reporter, slot.fonts.Descriptor()))
		}
	}
}

// replace names slot with set, swaps it in at set's index and retires the
// slot it replaces: the glyph table forgets the glyphs of its fonts, whose
// fields and cells the drawing job lets go of once it has drawn the frame,
// since an entry recorded before the replace may still draw them, and the
// pool of shapers is made anew, since its shapers hold faces of the fonts
// and the pool cannot be walked; every shaper is back in it under the
// write lock. Its layouts are found no more and go idle, so nothing then
// holds its fonts.
func (w fontLibraryWriter) replace(set FontSet, slot *fontSetSlot) {
	slot.set = set
	retired := w.sets[set.index]
	w.sets[set.index] = slot
	if retired.fonts == nil {
		return
	}
	w.glyphs.retireFonts(slices.Collect(retired.fonts.Fonts()))
	w.shapers = sync.Pool{New: func() any { return new(textShaper) }}
}
