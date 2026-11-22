package canvas

import (
	"bytes"
	"fmt"
	"io/fs"
	"sync"

	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/font/opentype"
)

// builtInFontPath is the path among canvas's embedded assets of its
// built-in font, JetBrains Mono, mounted at cog.canvas/jetbrainsmono.ttf.
const builtInFontPath = "assets/jetbrainsmono.ttf"

// findBuiltInFont answers canvas's built-in font, JetBrains Mono, parsed
// from its embedded assets once for the process: a parsed font is
// read-only, so every kernel's faces share it.
var findBuiltInFont = sync.OnceValue(func() *font.Font {
	file, err := embeddedAssets.Open(builtInFontPath)
	if err != nil {
		panic(err) // canvas embeds its built-in font, which is always there.
	}
	face, err := font.ParseTTF(file.(opentype.Resource))
	if err != nil {
		panic(err) // canvas's built-in font always parses.
	}
	return face.Font
})

// loadedFont is a font loaded from its file: the font parsed, read-only, the
// coordinates of its variations, which a face of it is made at, and its
// vertical measures at an em of one world unit, read once as it is loaded.
type loadedFont struct {
	font        *font.Font
	coordinates []font.VarCoord
	perEm       FontMetrics
}

// loadFont answers the font descriptor names, its file read through files,
// or canvas's built-in JetBrains Mono when its Path is empty, at its
// Variations. A file that fails to read or parse, or whose collection has
// no font at descriptor's Index, is an error.
func loadFont(files fs.FS, descriptor Font) (loadedFont, error) {
	// go-text's shaper keeps its state of a font by the font, so each
	// loaded font is a font of its own, the built-in font copied, sharing
	// its read-only tables, lest two variations of it shape alike.
	builtIn := *findBuiltInFont()
	parsed := &builtIn
	if descriptor.Path != "" {
		data, err := fs.ReadFile(files, descriptor.Path)
		if err != nil {
			return loadedFont{}, err
		}
		loaders, err := opentype.NewLoaders(bytes.NewReader(data))
		if err != nil {
			return loadedFont{}, err
		}
		if int(descriptor.Index) >= len(loaders) {
			return loadedFont{}, fmt.Errorf("its collection of %d fonts has none at index %d", len(loaders), descriptor.Index)
		}
		if parsed, err = font.NewFont(loaders[descriptor.Index]); err != nil {
			return loadedFont{}, err
		}
	}
	face := font.NewFace(parsed)
	var variations []font.Variation
	for _, variation := range descriptor.Variations {
		if variation.Axis != (AxisTag{}) {
			axis := variation.Axis
			variations = append(variations, font.Variation{Tag: opentype.NewTag(axis[0], axis[1], axis[2], axis[3]), Value: variation.Value})
		}
	}
	face.SetVariations(variations)
	extents, _ := face.FontHExtents()
	unitsPerEm := float32(face.Upem())
	return loadedFont{
		font:        parsed,
		coordinates: face.Coords(),
		perEm: FontMetrics{
			Ascent:     extents.Ascender / unitsPerEm,
			Descent:    -extents.Descender / unitsPerEm,
			LineGap:    extents.LineGap / unitsPerEm,
			LineHeight: (extents.Ascender - extents.Descender + extents.LineGap) / unitsPerEm,
			XHeight:    face.LineMetric(font.XHeight) / unitsPerEm,
			CapHeight:  face.LineMetric(font.CapHeight) / unitsPerEm,
		},
	}, nil
}

// findMetrics answers the font's measures at size, its em in world units.
func (f loadedFont) findMetrics(size float32) FontMetrics {
	return FontMetrics{
		Ascent:     f.perEm.Ascent * size,
		Descent:    f.perEm.Descent * size,
		LineGap:    f.perEm.LineGap * size,
		LineHeight: f.perEm.LineHeight * size,
		XHeight:    f.perEm.XHeight * size,
		CapHeight:  f.perEm.CapHeight * size,
	}
}
