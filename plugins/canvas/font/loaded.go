package font

import (
	"bytes"
	"fmt"
	"io/fs"
	"math"

	gotext "github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/font/opentype"
)

// Loaded is a font loaded from its file, read-only, so safe for concurrent
// use: what it was loaded of, the font parsed with the coordinates of its
// variations, which a shaper's face of it is made at, and its vertical
// measures at an em of one world unit, read once as it is loaded.
type Loaded struct {
	font        Font
	parsed      *gotext.Font
	coordinates []gotext.VarCoord
	perEm       Metrics
}

// loadFont answers the font descriptor names, its file read through files,
// or the built-in JetBrains Mono when its Path is empty, at its
// Variations, and whether one of them, of the zero Axis and a non-zero
// Value, names no axis and is ignored, answered whether or not the font
// loads. A file that fails to read or parse, or whose collection has no
// font at descriptor's Index, is an error.
func loadFont(files fs.FS, descriptor Font) (*Loaded, bool, error) {
	var variations []gotext.Variation
	variationIgnored := false
	for _, variation := range descriptor.Variations {
		axis := variation.Axis
		if axis != (AxisTag{}) {
			variations = append(variations, gotext.Variation{Tag: opentype.NewTag(axis[0], axis[1], axis[2], axis[3]), Value: variation.Value})
		} else if variation.Value != 0 {
			variationIgnored = true
		}
	}
	var parsed *gotext.Font
	if descriptor.Path == "" {
		// go-text's shaper keeps its state of a font by the font, so each
		// loaded font is a font of its own, the built-in font copied,
		// sharing its read-only tables, lest two variations of it shape
		// alike.
		builtIn := *findBuiltInFont()
		parsed = &builtIn
	} else {
		data, err := fs.ReadFile(files, descriptor.Path)
		if err != nil {
			return nil, variationIgnored, err
		}
		loaders, err := opentype.NewLoaders(bytes.NewReader(data))
		if err != nil {
			return nil, variationIgnored, err
		}
		if int(descriptor.Index) >= len(loaders) {
			return nil, variationIgnored, fmt.Errorf("its collection of %d fonts has none at index %d", len(loaders), descriptor.Index)
		}
		if parsed, err = gotext.NewFont(loaders[descriptor.Index]); err != nil {
			return nil, variationIgnored, err
		}
	}
	face := gotext.NewFace(parsed)
	face.SetVariations(variations)
	extents, _ := face.FontHExtents()
	unitsPerEm := float32(face.Upem())
	return &Loaded{
		font:        descriptor,
		parsed:      parsed,
		coordinates: face.Coords(),
		perEm: Metrics{
			Ascent:     extents.Ascender / unitsPerEm,
			Descent:    -extents.Descender / unitsPerEm,
			LineGap:    extents.LineGap / unitsPerEm,
			LineHeight: (extents.Ascender - extents.Descender + extents.LineGap) / unitsPerEm,
			XHeight:    face.LineMetric(gotext.XHeight) / unitsPerEm,
			CapHeight:  face.LineMetric(gotext.CapHeight) / unitsPerEm,
		},
	}, variationIgnored, nil
}

// Font answers what the font was loaded of: its Font in its set's
// descriptor, or the zero Font for the built-in JetBrains Mono replacing a
// default font that failed to load.
func (f *Loaded) Font() Font { return f.font }

// DrawnEm answers the em, in world units, a run of size is laid out at in
// the font: size, or for a bitmap font size rounded to the nearest whole
// multiple of its pixel size, one at least.
func (f *Loaded) DrawnEm(size float32) float32 {
	if f.font.BitmapEm <= 0 {
		return size
	}
	pixelSize := float32(f.font.BitmapEm)
	return max(float32(math.Round(float64(size/pixelSize))), 1) * pixelSize
}

// Metrics answers the font's measures at size, its em in world units.
func (f *Loaded) Metrics(size float32) Metrics {
	return Metrics{
		Ascent:     f.perEm.Ascent * size,
		Descent:    f.perEm.Descent * size,
		LineGap:    f.perEm.LineGap * size,
		LineHeight: f.perEm.LineHeight * size,
		XHeight:    f.perEm.XHeight * size,
		CapHeight:  f.perEm.CapHeight * size,
	}
}
