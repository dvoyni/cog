package types

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"io/fs"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/assets"
	"github.com/dvoyni/cog/libs/m"

	"github.com/dvoyni/cog/slots/gfx"
)

// whiteTexel is the one sprite canvas makes rather than reads: a single opaque
// white RGBA pixel, which every solid fill, line and stroke samples at its
// centre.
//
// It is a const rather than a []byte literal because the bytes are the asset's
// identity. A const resolves to one address every time it is evaluated, so
// NewBlobFromString of it names one cache entry; a fresh []byte{255,255,255,255}
// at a call site would be a new asset every frame.
const whiteTexel = "\xff\xff\xff\xff"

// spritePadding is the border packed around every sprite read from storage, in
// texels, with the edge texels extruded into it so bilinear filtering at a page
// boundary samples the sprite rather than its neighbour. The generated texel
// takes none: it is sampled at one point.
const spritePadding = 2

// spriteDescrParams is the sprite tier's bake parameters, and it has exactly one
// case to carry: whether the descriptor names the texel canvas generates rather
// than a file it reads. A generated sprite's bytes are already RGBA, are packed
// bare, and are sampled at the centre of their one texel.
//
// The field is unexported, so the only descriptor that can be spelled by hand
// carries the zero params, which name a file.
type spriteDescrParams struct{ generated bool }

// tiledDescrParams and sizeDescrParams are empty: neither tier bakes anything,
// and there is nothing about a request on either that its path does not say.
//
// They are two named types rather than two uses of struct{} because a descriptor
// is also the report-once key and the kernel's table is keyed by type. Two tiers
// sharing Descr[struct{}] would share one namespace, so one tier's failure would
// silence the other's, and one tier's FreeAll - which forgets every key of its
// own type - would forget the other's reports while its entries were still
// cached and still failed. A tier owning its own type cannot do either.
type tiledDescrParams struct{}

type sizeDescrParams struct{}

// spriteUser is what the sprite loader needs that it may not hold itself: the
// packer, which outlives every handler and lives on the Lookup, and the resource
// queue of the handler doing the loading.
//
// It travels by value rather than behind a pointer, because a Get on the sprite
// tier happens per sprite per frame and building one on the heap each time would
// put an allocation on the frame path.
type spriteUser struct {
	packer    *packer
	resources *gfx.ResourceQueue
}

// spriteLoader decodes a sprite and packs it into a texture array. Its value is
// the atlas entry, by value: nothing mutates one after the load, and freeing it
// only reads it.
type spriteLoader struct{}

// Load decodes the bytes and hands them to the packer. Everything it finds wrong
// is its own to say: the Library owns the read, so what is left is an image that
// will not decode and the packer's two refusals. Each of them runs once by
// construction, because whatever this returns is the entry until a Free.
func (spriteLoader) Load(k kernel.Kernel, data assets.Blob, params spriteDescrParams, _ fs.FS, user spriteUser) AtlasEntry {
	source := insertion{pixels: data.Data(), width: 1, height: 1, centreUV: true}
	if !params.generated {
		width, height, pixels, err := decodeImage(data)
		if err != nil {
			k.ReportError(fmt.Errorf("canvas: sprite image could not be decoded: %w", err))
			return AtlasEntry{}
		}
		source = insertion{pixels: pixels, width: width, height: height, padding: spritePadding, extrude: true}
	}
	entry, refusal := user.packer.insert(source, user.resources)
	switch refusal {
	case packTooLarge:
		k.ReportError(fmt.Errorf("canvas: sprite %dx%d padded to %dx%d does not fit a %d-texel atlas page",
			source.width, source.height,
			source.width+2*source.padding, source.height+2*source.padding,
			user.packer.config.AtlasSize))
		return AtlasEntry{}
	case packOverBudget:
		k.ReportError(fmt.Errorf("canvas: sprite %dx%d does not fit the atlas: %d bytes of texture arrays already allocated against a %d-byte budget",
			source.width, source.height, user.packer.bytes, user.packer.config.MaxAtlasBytes))
		return AtlasEntry{}
	}
	return entry
}

// Default skips rather than substitutes. A sprite's on-screen size comes from
// the transform or from the sprite's own pixels, so a placeholder would be
// visible exactly when the draw named a size and invisible when the draw trusted
// the file - and in ui the element was already laid out at zero by the
// measurement tier, so the placeholder would paint outside a box of size zero.
// The loudness is the report, not the pixels.
func (spriteLoader) Default(assets.Descr[spriteDescrParams], spriteUser) AtlasEntry {
	return AtlasEntry{}
}

// Free returns the entry's slot to the packer, which releases the array behind
// it once nothing is left in it.
func (spriteLoader) Free(value AtlasEntry, user spriteUser) {
	user.packer.freeEntry(value, user.resources)
}

// standaloneLoader decodes a sprite into a full-image texture of its own, kept
// outside the atlas so tiled sprites can sample it with repeat addressing.
type standaloneLoader struct{}

func (standaloneLoader) Load(k kernel.Kernel, data assets.Blob, _ tiledDescrParams, _ fs.FS, resources *gfx.ResourceQueue) StandaloneEntry {
	width, height, pixels, err := decodeImage(data)
	if err != nil {
		k.ReportError(fmt.Errorf("canvas: tiled sprite image could not be decoded: %w", err))
		return StandaloneEntry{}
	}
	return StandaloneEntry{
		// A decoded image is sRGB by definition, and there is no caller to say
		// otherwise: canvas draws pictures, never data maps.
		Texture: resources.BakeTexture(width, height, gfx.FormatRGBA8Srgb, pixels, true, false),
		Width:   width,
		Height:  height,
	}
}

// Default is the zero entry, on the sprite tier's terms: a tiled draw with no
// texture draws nothing.
func (standaloneLoader) Default(assets.Descr[tiledDescrParams], *gfx.ResourceQueue) StandaloneEntry {
	return StandaloneEntry{}
}

func (standaloneLoader) Free(value StandaloneEntry, resources *gfx.ResourceQueue) {
	if value.Width <= 0 || value.Height <= 0 {
		return
	}
	resources.ReleaseTexture(value.Texture)
}

// spriteSizeLoader reads a sprite's intrinsic pixel size out of its header. It
// is the measurement tier, and its user value is struct{} - no packer, no
// resource queue, no device of any kind. That is the property that keeps a
// handler which only lays a page out off the GPU's lock set, and it is a
// requirement of this tier rather than an accident of what the loader needed.
type spriteSizeLoader struct{}

func (spriteSizeLoader) Load(k kernel.Kernel, data assets.Blob, _ sizeDescrParams, _ fs.FS, _ struct{}) m.Vec2i {
	config, _, err := image.DecodeConfig(bytes.NewReader(data.Data()))
	if err != nil {
		k.ReportError(fmt.Errorf("canvas: sprite header could not be decoded: %w", err))
		return m.Vec2i{}
	}
	return m.Vec2i{X: config.Width, Y: config.Height}
}

// Default is a zero size, which is what an element sized from a sprite that did
// not load lays out at.
func (spriteSizeLoader) Default(assets.Descr[sizeDescrParams], struct{}) m.Vec2i { return m.Vec2i{} }

// Free has nothing to release: a size is two ints and holds no handle.
func (spriteSizeLoader) Free(m.Vec2i, struct{}) {}

// decodeImage decodes an image's bytes into straight-alpha RGBA rows.
func decodeImage(data assets.Blob) (width, height int, pixels []byte, err error) {
	decoded, _, err := image.Decode(bytes.NewReader(data.Data()))
	if err != nil {
		return 0, 0, nil, err
	}
	bounds := decoded.Bounds()
	rgba := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(rgba, rgba.Bounds(), decoded, bounds.Min, draw.Src)
	return bounds.Dx(), bounds.Dy(), rgba.Pix, nil
}
