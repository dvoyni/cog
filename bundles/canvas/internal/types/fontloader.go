package types

import (
	"fmt"
	"io/fs"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/assets"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
)

// A font file is one asset and a face baked from it is another thing made of
// it, so the font store is two caches rather than one. The release side is what
// proves it: unloading a font frees every sized face it backs, while a
// framebuffer resize frees every face and keeps every parsed source. One cache
// keyed by path with the faces inside its value cannot express the resize at
// all, and one cache keyed by path and size re-parses the file once per size.

// sourceDescrParams is the parsed-source tier's bake parameters, and there are
// none: a font file parses one way, and everything about the request is its
// path.
//
// It is a named type rather than a use of struct{} for the reason the tiled and
// measurement tiers already are - a descriptor is also the report-once key, the
// kernel's table is keyed by type, and two tiers sharing Descr[struct{}] would
// share one namespace and silence or forget each other's reports.
type sourceDescrParams struct{}

// faceDescrParams identifies one baked face: the file it was baked from and the
// pixel size it was baked at.
//
// The path is a parameter rather than the descriptor's Name, and that is the
// one place this tier departs from the other four. Name is what the Library
// reads, and a face is not read from storage - it is baked from an asset the
// source tier already holds. Setting Name would make the Library open and read
// the whole font file once per size, which is the cost the second cache exists
// to remove; it would also duplicate the read on every size of a file the
// source tier had already parsed. So the face tier names no file, the Library
// reads nothing for it, and the loader reaches the bytes through the cache that
// owns them.
//
// Both fields are unexported, so the only descriptor spellable outside this
// package carries the zero params, which name nothing.
type faceDescrParams struct {
	path string
	px   int
}

// fontUserData is what the face loader needs that it does not hold itself: the
// source cache it re-enters inside its own Load. Re-entering a *different*
// cache is what the Library allows and what this tier is built on.
//
// It travels by value: it is one pointer, a face Get happens per text op per
// frame, and building one on the heap each time would put an allocation on the
// frame path.
type fontUserData struct {
	sources *assets.Cache[sourceDescrParams, struct{}, *opentype.Font]
}

// fontSourceLoader parses a font file. Its user data is struct{} - no packer,
// no resource queue, no device - because a parsed font is CPU memory and
// nothing else, which is what keeps a handler that only measures text off the
// GPU's lock set.
type fontSourceLoader struct{}

// Load parses the file's bytes. The Library owns the read, so the one thing
// left to go wrong here is a file that is not a font, and that is reported at
// the point it is produced. It runs once per path by construction: whatever
// this returns is the entry until a Free.
//
// It cannot name the file it is parsing. Load is handed the bytes, the bake
// parameters and the filesystem, but not the descriptor's Name, so the report
// says what was wrong and the caller's own path is what places it.
func (fontSourceLoader) Load(k kernel.Kernel, data assets.Blob, _ sourceDescrParams, _ fs.FS, _ struct{}) *opentype.Font {
	parsed, err := opentype.Parse(data.Data())
	if err != nil {
		k.ReportError(fmt.Errorf("canvas: font source could not be parsed: %w", err))
		return nil
	}
	return parsed
}

// Default is a nil source, which bakes no face. A broken font is not
// substituted: the empty path asks for the built-in default, and a named font
// that cannot be loaded keeps failing, so a missing asset stays visible rather
// than quietly rendering in another typeface.
func (fontSourceLoader) Default(assets.Descr[sourceDescrParams], struct{}) *opentype.Font { return nil }

// Free has nothing to release: a parsed font is CPU memory the garbage
// collector reclaims once the entry is gone.
func (fontSourceLoader) Free(*opentype.Font, struct{}) {}

// fontFaceLoader bakes one face at one pixel size out of a parsed source, and
// is the second half of the font store.
type fontFaceLoader struct{}

// Load re-enters the source cache for the file this face is baked from, which
// is allowed because it is a different cache, and which is what makes a source
// read and parsed once serve every size baked from it.
//
// A source that did not load bakes no face and says nothing further: the source
// tier already reported it, once, and repeating it per size is what a second
// report would be.
func (fontFaceLoader) Load(k kernel.Kernel, _ assets.Blob, params faceDescrParams, fsys fs.FS, userData fontUserData) *Font {
	source := userData.sources.Get(k, assets.Descr[sourceDescrParams]{Name: params.path}, fsys, struct{}{})
	if source == nil {
		return nil
	}
	face, err := opentype.NewFace(source, &opentype.FaceOptions{
		Size: float64(params.px), DPI: 72, Hinting: font.HintingFull,
	})
	if err != nil {
		k.ReportError(fmt.Errorf("canvas: font could not be baked at %d pixels: %w", params.px, err))
		return nil
	}
	return &Font{
		Face: face, Glyphs: map[rune]Glyph{},
		LineHeight: float32(face.Metrics().Height.Ceil()),
	}
}

// Default is never reached: this tier names no file, so the Library performs no
// read for it and has no read to fail. It is a nil face for the same reason
// Load returns one, so the two answers agree if the Library ever gains a way to
// call it.
func (fontFaceLoader) Default(assets.Descr[faceDescrParams], fontUserData) *Font { return nil }

// Free closes the baked face. The glyph atlas slots that face rasterized into
// are not reclaimed here: the glyph side is a packer with no table, and nothing
// but a framebuffer scale change knows which slots a face owned.
func (fontFaceLoader) Free(value *Font, _ fontUserData) {
	if value == nil {
		return
	}
	_ = value.Face.Close()
}
