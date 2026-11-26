package canvas

import (
	"cmp"
	"slices"
	"sync"

	"github.com/dvoyni/cog/plugins/canvas/font"
)

// glyphTable is the glyphs made for drawing, distance fields or a bitmap
// font's coverage, each at an index of its own, which a glyph laid out for
// drawing names: made in the jobs recording text, under the FontLibrary
// resource's read lock, and read by the drawing job, so it is guarded by a
// lock of its own. A field is changed once made only when its font is
// retired, emptied, so a copy of it is read outside the lock. An index is
// never given to another glyph, so no entry draws one glyph's texels at
// another's.
type glyphTable struct {
	mutex sync.Mutex
	// indexes are each glyph's index among fields.
	indexes map[glyphKey]uint32
	fields  []glyphField
	// retiredFields are the fields of the glyphs whose fonts were retired,
	// which entries recorded before the retirement may still draw, so the
	// drawing job lets go of them once it has drawn its frame.
	retiredFields []uint32
	// fieldEm and fieldRange are the em, in field texels, a glyph is made
	// at and how far its field reaches across its edge when its font gives
	// none, Config.GlyphFieldEm's and Config.GlyphFieldRange's.
	fieldEm    int
	fieldRange int
}

// glyphKey is a glyph of one loaded font, which its set holds once, and
// its id in it.
type glyphKey struct {
	font *font.Loaded
	id   font.GlyphID
}

// assignFields sets the field of each of glyphs to the index of its field,
// made the first time it is drawn by makeField, filled by shaper, and
// answers glyphs without those of no field, such as spaces, which draw
// nothing, its icons kept, which have none, and how many fields it made. A field is made outside the
// lock, so jobs make theirs together; one made by two jobs at once is kept
// once, and counted by the job keeping it.
func (t *glyphTable) assignFields(glyphs []placedGlyph, shaper *font.Shaper) ([]placedGlyph, int) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	drawnGlyphs := glyphs[:0]
	madeCount := 0
	for _, glyph := range glyphs {
		if glyph.font == nil {
			drawnGlyphs = append(drawnGlyphs, glyph)
			continue
		}
		key := glyphKey{font: glyph.font, id: glyph.id}
		field, made := t.indexes[key]
		if !made {
			t.mutex.Unlock()
			madeField := t.makeField(shaper, glyph.font, key.id)
			t.mutex.Lock()
			if field, made = t.indexes[key]; !made {
				field = uint32(len(t.fields))
				t.fields = append(t.fields, madeField)
				t.indexes[key] = field
				madeCount++
			}
		}
		if t.fields[field].Texels != nil {
			glyph.field = field
			drawnGlyphs = append(drawnGlyphs, glyph)
		}
	}
	return drawnGlyphs, madeCount
}

// retireFonts forgets the glyphs made of fonts, those of a set replaced, so
// a glyph of theirs laid out again is made anew at an index of its own,
// and queues their fields to be let go once the frame is drawn, since an
// entry recorded before the retirement may still draw them.
func (t *glyphTable) retireFonts(fonts []*font.Loaded) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	for key, field := range t.indexes {
		if slices.Contains(fonts, key.font) {
			delete(t.indexes, key)
			t.retiredFields = append(t.retiredFields, field)
		}
	}
}

// takeRetiredFields empties the fields of the glyphs retired, and answers
// their indexes appended to fields, for the drawing job to free their cells
// once it has drawn its frame.
func (t *glyphTable) takeRetiredFields(fields []uint32) []uint32 {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	for _, field := range t.retiredFields {
		t.fields[field] = glyphField{}
	}
	fields = append(fields, t.retiredFields...)
	t.retiredFields = t.retiredFields[:0]
	return fields
}

// makeField answers glyph id of glyphFont, filled by shaper: its coverage
// for a bitmap font, else its distance field at its font's field em and
// range, zero meaning the table's.
func (t *glyphTable) makeField(shaper *font.Shaper, glyphFont *font.Loaded, id font.GlyphID) glyphField {
	descriptor := glyphFont.Font()
	if descriptor.BitmapEm > 0 {
		return makeGlyphBitmap(shaper, glyphFont, id, descriptor.BitmapEm)
	}
	return makeGlyphField(shaper, glyphFont, id, cmp.Or(descriptor.FieldEm, t.fieldEm), cmp.Or(descriptor.FieldRange, t.fieldRange))
}

// findField answers the field at index, one assignFields assigned.
func (t *glyphTable) findField(index uint32) glyphField {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	return t.fields[index]
}
