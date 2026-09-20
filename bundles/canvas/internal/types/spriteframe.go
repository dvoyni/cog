package types

import (
	"fmt"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/gfx"
)

// invalidSpriteFrame is the report-once key canvas speaks a Frame or a set of
// nine-slice insets that does not fit its source under, for a sprite named by a
// path. It is canvas's own type for the reason invalidSpritePath is: the
// kernel's report table is keyed by type, so a type of its own shares a
// namespace with nothing.
//
// A frame is resolved at every draw rather than cached the way an atlas entry
// is, so the key has to carry what the mistake was and not merely where it
// was made: without it a sprite recorded every frame would report every frame.
// The insets are part of the key and zero for a frame's own report, which is
// what keeps a bad frame and an unfittable nine-slice on the same sheet from
// silencing each other.
type invalidSpriteFrame struct {
	path   string
	frame  SpriteFrame
	insets SpriteFrame
}

// invalidTextureFrame is the same key for a sprite sourced from a gfx texture,
// which has no path of its own to be named by.
//
// It carries the texture's id and its resource path both, because neither alone
// identifies a texture: an allocated one is named by its id and leaves the path
// empty, and a TextureWithResource descriptor is named by its path and leaves
// the id at zero until something bakes it. Keying on the id alone would file
// every unbaked resource texture under zero and report only the first.
type invalidTextureFrame struct {
	texture gfx.TextureID
	path    string
	frame   SpriteFrame
	insets  SpriteFrame
}

// textureText names a texture the way a report can be traced back to the code
// that drew it: by resource path when it has one, and by baked id otherwise.
func textureText(texture gfx.TextureDescr) string {
	if path := texture.Path(); path != "" {
		return fmt.Sprintf("%q", path)
	}
	return fmt.Sprintf("texture %d", texture.ID())
}

// ReportInvalidSpriteFrame says once that a Frame does not select a non-empty
// sub-rectangle of the sheet it was given.
func ReportInvalidSpriteFrame(k kernel.Kernel, path string, width, height int, frame SpriteFrame) {
	k.ReportErrorOnce(invalidSpriteFrame{path: path, frame: frame},
		fmt.Errorf("canvas: sprite frame %s does not fit %q (%dx%d)", frameText(frame), path, width, height))
}

// ReportInvalidTextureFrame is ReportInvalidSpriteFrame for a texture-sourced
// sprite, which names its texture because it has no path.
func ReportInvalidTextureFrame(k kernel.Kernel, texture gfx.TextureDescr, width, height int, frame SpriteFrame) {
	k.ReportErrorOnce(invalidTextureFrame{texture: texture.ID(), path: texture.Path(), frame: frame},
		fmt.Errorf("canvas: sprite frame %s does not fit %s (%dx%d)", frameText(frame), textureText(texture), width, height))
}

// ReportInvalidSpriteNineSlice says once that nine-slice insets leave no middle
// in the source they were measured against. The dimensions it names are the
// frame's when the sprite carries one, because that is what the insets are
// measured into - insets that fit the sheet but not the frame are the case this
// exists to explain.
func ReportInvalidSpriteNineSlice(k kernel.Kernel, path string, width, height int, frame, insets SpriteFrame) {
	k.ReportErrorOnce(invalidSpriteFrame{path: path, frame: frame, insets: insets},
		fmt.Errorf("canvas: nine-slice insets %s do not fit the %dx%d source %q selects", frameText(insets), width, height, path))
}

// ReportInvalidTextureNineSlice is ReportInvalidSpriteNineSlice for a
// texture-sourced sprite.
func ReportInvalidTextureNineSlice(k kernel.Kernel, texture gfx.TextureDescr, width, height int, frame, insets SpriteFrame) {
	k.ReportErrorOnce(invalidTextureFrame{texture: texture.ID(), path: texture.Path(), frame: frame, insets: insets},
		fmt.Errorf("canvas: nine-slice insets %s do not fit the %dx%d source %s selects", frameText(insets), width, height, textureText(texture)))
}

// frameText spells a SpriteFrame the way it is written at a call site, so a
// report can be compared against the code that caused it without arithmetic.
func frameText(frame SpriteFrame) string {
	return fmt.Sprintf("{Left:%d Top:%d Right:%d Bottom:%d}", frame.Left, frame.Top, frame.Right, frame.Bottom)
}
