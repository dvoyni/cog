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
	"github.com/dvoyni/cog/slots/gfx"
)

// textureDescrParams is everything about one of a model's images that its path
// does not already say: which image of that path it is, and the colour space
// the slot binding it reads it in.
//
// Its fields are unexported, so the only descriptor anything outside this
// package could spell by hand carries a zero P - the no-options constructor,
// which names image 0 in linear space and is nobody's asset by accident.
//
// It is a named type of its own rather than a second use of ModelDescrParams,
// and that is load-bearing rather than tidy: two caches over one params type
// share one kernel report-once namespace, so the second of them to fail a path
// is silenced and either one's FreeAll forgets the other's reports while its own
// entries stay cached and failed. Canvas reproduced exactly that before pinning
// it, and an empty params type is where it happens.
type textureDescrParams struct {
	// image is the index into the file's images array for a picture the model
	// embeds, and externalImage for one with a storage path of its own, where
	// the path is the whole identity.
	image int
	// srgb is part of the key because the colour space is the slot's property
	// and not the image's: base colour and emissive are gamma-encoded pictures
	// and the other three are data. One image used as both is two GPU textures,
	// and it has to be - sampling a normal map through an sRGB view is a wrong
	// picture with nothing in the frame to explain it.
	//
	// It does not separate within the data group, and must not be made to. The
	// material slot stays out of the key, because putting it in would turn one
	// ORM image - occlusion, roughness and metalness packed into one picture,
	// which is glTF's standard packing - into three GPU textures.
	srgb bool
}

// textureDescr names one image of one model: the path it is read from, the
// index within that path, and the colour space it is uploaded in.
//
// Textures live in a scene-owned cache rather than in canvas's atlas because
// wrap modes, mip chains and per-texture samplers rule the atlas out: an atlas
// page is one sampler and one set of neighbours, and a tiling ground beside a
// clamped decal needs two of each.
//
// An external image names its own resolved storage path, so every model that
// binds it binds the same entry - which is what makes the double decode go
// away. A GLB-embedded image has no path of its own, so it is named by its
// model's path and its index, and its bytes ride in Blob beside that name. The
// Library takes a Blob supplied beside a Name as a payload rather than as part
// of the identity, so the container is never opened again to find them and the
// key does not move when the model is parsed afresh.
type textureDescr = assets.Descr[textureDescrParams]

// externalImage is the image index a descriptor carries for a picture that has
// a storage path of its own, where the path is the whole identity.
const externalImage = -1

// textureUserData is what the texture loader needs that only a handler holds: the
// resource queue it bakes and releases through.
//
// model and name ride with it because Load is not handed the descriptor, and a
// decode failure names both the file that asked for the picture and the picture
// that broke. Free and Default read neither, so a free passes them empty - the
// same shape the model loader carries its path in.
type textureUserData struct {
	resources *gfx.ResourceQueue
	model     string
	name      string
}

// textureLoader decodes one image and uploads it. It is stateless: the kernel,
// the queue and the two names all arrive per call, and none of them is retained.
type textureLoader struct{}

// Load decodes PNG or JPEG bytes into the straight-alpha RGBA8 gfx bakes and
// mips, in the colour space the slot reads it in. For an external image the
// Library has already read the file; for an embedded one data is the payload
// the parse supplied beside the model's name.
//
// A picture that opens and does not decode is the loader's own fault rather
// than the Library's, so it keeps scene's own report type and its own
// string-keyed namespace - which is the dedup no cache can express, because one
// broken image bound in two colour spaces is two entries and one fact.
// Whatever this returns is cached, the placeholder included, so a broken
// picture is decoded one time per entry until an UnloadTexture.
func (textureLoader) Load(
	k kernel.Kernel, data assets.Blob, params textureDescrParams, _ fs.FS, userData textureUserData,
) gfx.TextureDescr {
	decoded, _, err := image.Decode(bytes.NewReader(data.Data()))
	if err != nil {
		named := textureReportPath(userData.name, params.image)
		k.ReportErrorOnce(textureReportKey(named),
			ErrModelTextureUnavailable{Model: userData.model, Texture: named, Err: err})
		return placeholderTexture(params, userData.resources)
	}
	bounds := decoded.Bounds()
	rgba := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(rgba, rgba.Bounds(), decoded, bounds.Min, draw.Src)
	format := gfx.FormatRGBA8
	if params.srgb {
		format = gfx.FormatRGBA8Srgb
	}
	// Mips are generated at bake by the backend's CPU box filter, which is the
	// whole of the no-mipmap-generation-API gap: a minified model texture
	// without them aliases into noise the moment the camera moves. The pixels
	// are handed over rather than copied - they are this decode's private
	// buffer and nothing reads them again.
	return userData.resources.BakeTexture(bounds.Dx(), bounds.Dy(), format, rgba.Pix, false, true)
}

// Default supplies the value for a picture whose file could not be read. The
// Library has already reported that, and it never says what went wrong here:
// what gets drawn is a separate question from what gets said.
func (textureLoader) Default(d textureDescr, userData textureUserData) gfx.TextureDescr {
	return placeholderTexture(d.Params, userData.resources)
}

// Free hands one baked texture back through the resource queue, which is the
// same release the hand-written unload scan emitted before. A placeholder for a
// data slot is the zero descriptor and had nothing baked for it, so there is
// nothing to hand back.
func (textureLoader) Free(value gfx.TextureDescr, userData textureUserData) {
	if value.ID() != 0 {
		userData.resources.ReleaseTexture(value)
	}
}

// placeholderTexture is what a slot binds when its image did not arrive:
// magenta for a picture, and nothing at all for data.
//
// A missing base colour rendering white looks deliberate, which is why the
// picture case is loud. But magenta is only right for a picture: as a normal map
// it is a surface lit from nowhere, and as metallic-roughness it is
// metallic = 1, roughness = 0, which is a mirror. srgb separates the two groups
// exactly, because the sRGB slots are the pictures - so the data half returns
// the zero descriptor and bindModelMaterial keeps the per-slot white texel and
// flat normal it already had for those, unchanged.
//
// It is baked per failed entry rather than shared, so a Free releases exactly
// what this minted and nothing has to count references to a placeholder. The
// entries that take one are the broken ones, which a level has few of by
// construction.
func placeholderTexture(params textureDescrParams, resources *gfx.ResourceQueue) gfx.TextureDescr {
	if !params.srgb {
		return gfx.TextureDescr{}
	}
	return resources.BakeTexture(1, 1, gfx.FormatRGBA8Srgb, magentaTexel[:], true, false)
}

// magentaTexel is the placeholder's one opaque pixel. 1x1 rather than larger
// because for a constant texel every mip level is identical, so there is
// nothing to generate.
var magentaTexel = [4]byte{0xff, 0x00, 0xff, 0xff}

// textureReportPath names a picture in a report. An external image is named by
// its storage path, which is what a caller can act on; an embedded one is named
// by the file and the index, because that is the whole of its identity.
func textureReportPath(name string, image int) string {
	if image == externalImage {
		return name
	}
	return fmt.Sprintf("%s#image%d", name, image)
}
