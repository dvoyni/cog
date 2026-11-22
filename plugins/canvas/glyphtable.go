package canvas

import (
	"cmp"
	"sync"

	"github.com/go-text/typesetting/font"
)

// glyphTable is the glyphs made distance fields for drawing, each at an
// index of its own, which a glyph laid out for drawing names: made in the
// jobs recording text, under the FontLibrary resource's read lock, and read
// by the drawing job, so it is guarded by a lock of its own. A field is
// never changed once made, so a copy of it is read outside the lock.
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

// glyphKey is a glyph of one loaded font: its parsed font, which each
// loaded font has of its own, and its id in it.
type glyphKey struct {
	font *font.Font
	id   font.GID
}

// assignFields sets the field of each of glyphs to the index of its field,
// made the first time it is drawn through shaper's face of the glyph's
// font, at its variations, at its font's field em and range, zero meaning
// the table's, and answers glyphs without those of no field, such as
// spaces, which draw nothing. A field is made outside the lock, so jobs make
// theirs together; one made by two jobs at once is kept once.
func (t *glyphTable) assignFields(glyphs []placedGlyph, shaper *textShaper) []placedGlyph {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	drawnGlyphs := glyphs[:0]
	for _, glyph := range glyphs {
		key := glyphKey{font: glyph.font.font, id: glyph.id}
		field, made := t.indexes[key]
		if !made {
			t.mutex.Unlock()
			madeField := makeGlyphField(shaper.findFace(glyph.font), key.id, cmp.Or(glyph.font.fieldEm, t.fieldEm), cmp.Or(glyph.font.fieldRange, t.fieldRange))
			t.mutex.Lock()
			if field, made = t.indexes[key]; !made {
				field = uint32(len(t.fields))
				t.fields = append(t.fields, madeField)
				t.indexes[key] = field
			}
		}
		if t.fields[field].texels != nil {
			glyph.field = field
			drawnGlyphs = append(drawnGlyphs, glyph)
		}
	}
	return drawnGlyphs
}

// findField answers the field at index, one assignFields assigned.
func (t *glyphTable) findField(index uint32) glyphField {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	return t.fields[index]
}
