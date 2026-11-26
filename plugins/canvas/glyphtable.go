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
// lock of its own. A field is never changed once made, so a copy of it is
// read outside the lock.
type glyphTable struct {
	mutex sync.Mutex
	// indexes are each glyph's index among fields.
	indexes map[glyphKey]uint32
	fields  []glyphField
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

// dropFonts forgets the glyphs made of fonts and frees their fields, which
// no layout drawn after names; their cells in the glyph atlas, which frees
// none, stay.
func (t *glyphTable) dropFonts(fonts []*font.Loaded) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	for key, field := range t.indexes {
		if slices.Contains(fonts, key.font) {
			delete(t.indexes, key)
			t.fields[field] = glyphField{}
		}
	}
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
