package scene

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"io/fs"
	"net/url"
	"path"
	"strings"

	"github.com/dvoyni/cog/gfx"
	"github.com/qmuntal/gltf"
)

// textureKey identifies one entry of the scene-owned texture cache.
//
// Textures live here rather than in canvas's atlas because wrap modes, mip
// chains and per-texture samplers rule the atlas out: an atlas page is one
// sampler and one set of neighbours, and a tiling ground beside a clamped decal
// needs two of each.
//
// An external image keys on its resolved storage path and is therefore shared
// across every model that names it. A GLB-embedded image has no path of its
// own, so it keys on the model's path and its index in the file's images array
// - which is why two models embedding the same picture pay for it twice, and
// why the vendored set, every asset of which is a single-buffer GLB, exercises
// the cache within a model rather than across models.
//
// srgb is part of the key because the colour space is the slot's property, not
// the image's: base colour and emissive are gamma-encoded pictures and the
// other three are data. One image used as both is two GPU textures, and it has
// to be - sampling a normal map through an sRGB view is a wrong picture with
// nothing to explain it.
type textureKey struct {
	path  string
	image int
	srgb  bool
}

// externalImage is the image index a key carries for an image that has a
// storage path of its own, where the path is the whole identity.
const externalImage = -1

// loadedTexture is one decoded image waiting for the upload half of a load.
// The pixels are straight-alpha RGBA8 whatever the file encoded, because that
// is the one format gfx bakes and mips.
type loadedTexture struct {
	key           textureKey
	width, height int
	format        gfx.TextureFormat
	pixels        []byte
}

// missingTexture is the slot value for a texture that failed to decode. The
// model still becomes resident and the slot binds the 1x1 white texel, because
// a model missing one texture is a model you can see and fix; a model that
// failed wholesale over a missing picture is a level with a hole in it.
const missingTexture = -1

// textureLoader decodes the images one model's materials name, once per key,
// and collects the reports for slots that could not be filled.
//
// It is a per-load value rather than a lookup query because the load holds no
// Lookup lock: the cache is consulted at install time, where a key that is
// already resident drops the pixels this decoded. Decoding an image a resident
// model already uploaded is the cost of parsing without the lock, and it is
// paid only when two models share an external image path.
type textureLoader struct {
	doc       *gltf.Document
	fsys      fs.FS
	modelPath string
	// keys maps a decoded key to its index in textures, so nine textures over
	// three images decode three times.
	keys     map[textureKey]int
	textures []loadedTexture
	reports  []error
}

func newTextureLoader(doc *gltf.Document, fsys fs.FS, modelPath string) *textureLoader {
	return &textureLoader{doc: doc, fsys: fsys, modelPath: modelPath, keys: map[textureKey]int{}}
}

// texture resolves one glTF texture reference into a decoded image and the
// sampler that reads it, or reports missingTexture. srgb selects the colour
// space the slot binds it in.
func (l *textureLoader) texture(index int, srgb bool) (int, gfx.SamplerDesc) {
	sampler := defaultModelSampler
	if index < 0 || index >= len(l.doc.Textures) || l.doc.Textures[index] == nil {
		l.reports = append(l.reports, ErrModelTextureUnavailable{
			Model: l.modelPath, Texture: fmt.Sprintf("%d", index),
			Err: errors.New("the file has no texture there"),
		})
		return missingTexture, sampler
	}
	texture := l.doc.Textures[index]
	if texture.Sampler != nil {
		sampler = modelSampler(l.doc, *texture.Sampler)
	}
	if texture.Source == nil {
		l.reports = append(l.reports, ErrModelTextureUnavailable{
			Model: l.modelPath, Texture: texture.Name,
			Err: errors.New("it names no image source"),
		})
		return missingTexture, sampler
	}
	return l.image(*texture.Source, srgb), sampler
}

// image decodes one of the file's images in one colour space, once.
func (l *textureLoader) image(source int, srgb bool) int {
	if source < 0 || source >= len(l.doc.Images) || l.doc.Images[source] == nil {
		l.reports = append(l.reports, ErrModelTextureUnavailable{
			Model: l.modelPath, Texture: fmt.Sprintf("image %d", source),
			Err: errors.New("the file has no image there"),
		})
		return missingTexture
	}
	entry := l.doc.Images[source]
	key := textureKey{path: l.modelPath, image: source, srgb: srgb}
	if resolved, external := l.resolvePath(entry); external {
		key = textureKey{path: resolved, image: externalImage, srgb: srgb}
	}
	if index, decoded := l.keys[key]; decoded {
		return index
	}
	encoded, err := l.imageBytes(entry, key)
	if err == nil {
		var decoded loadedTexture
		decoded, err = decodeTexture(key, encoded)
		if err == nil {
			l.keys[key] = len(l.textures)
			l.textures = append(l.textures, decoded)
			return len(l.textures) - 1
		}
	}
	l.reports = append(l.reports, ErrModelTextureUnavailable{
		Model: l.modelPath, Texture: textureReportPath(key), Err: err,
	})
	// The failure is cached as the same miss, so a nine-texture material over
	// one broken image is one report rather than nine.
	l.keys[key] = missingTexture
	return missingTexture
}

// resolvePath reports the storage path an image's URI names, and whether it has
// one at all. A buffer view and a base64 data URI are both internal to the file
// and key on it instead.
func (l *textureLoader) resolvePath(entry *gltf.Image) (string, bool) {
	if entry.BufferView != nil || entry.URI == "" || entry.IsEmbeddedResource() {
		return "", false
	}
	if strings.Contains(entry.URI, "://") {
		// An absolute URI is a network fetch, which storage does not do.
		return "", false
	}
	unescaped, err := url.PathUnescape(entry.URI)
	if err != nil {
		unescaped = entry.URI
	}
	return path.Join(path.Dir(l.modelPath), unescaped), true
}

// imageBytes reads one image's encoded bytes from wherever the file put them.
func (l *textureLoader) imageBytes(entry *gltf.Image, key textureKey) ([]byte, error) {
	if entry.BufferView != nil {
		if *entry.BufferView < 0 || *entry.BufferView >= len(l.doc.BufferViews) {
			return nil, errors.New("its bufferView index is past the end of the file")
		}
		return readBufferViewBytes(l.doc, l.doc.BufferViews[*entry.BufferView])
	}
	if entry.IsEmbeddedResource() {
		return entry.MarshalData()
	}
	if key.image != externalImage {
		return nil, errors.New("it has neither a bufferView nor a URI")
	}
	if l.fsys == nil {
		return nil, errors.New("the model was loaded with no filesystem to resolve it against")
	}
	file, err := l.fsys.Open(key.path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(file)
}

// readBufferViewBytes slices one buffer view out of the document's decoded
// buffers. It is the modeler's ReadBufferView without the accessor validation,
// because an image is opaque bytes rather than typed elements.
func readBufferViewBytes(doc *gltf.Document, view *gltf.BufferView) ([]byte, error) {
	if view == nil || view.Buffer < 0 || view.Buffer >= len(doc.Buffers) {
		return nil, errors.New("its bufferView names no buffer")
	}
	data := doc.Buffers[view.Buffer].Data
	if view.ByteOffset < 0 || view.ByteLength < 0 || view.ByteOffset+view.ByteLength > len(data) {
		return nil, errors.New("its bufferView runs past the end of its buffer")
	}
	return data[view.ByteOffset : view.ByteOffset+view.ByteLength], nil
}

// decodeTexture turns encoded PNG or JPEG bytes into the straight-alpha RGBA8
// gfx bakes, in the colour space its slot reads it in.
func decodeTexture(key textureKey, encoded []byte) (loadedTexture, error) {
	decoded, _, err := image.Decode(bytes.NewReader(encoded))
	if err != nil {
		return loadedTexture{}, err
	}
	bounds := decoded.Bounds()
	rgba := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(rgba, rgba.Bounds(), decoded, bounds.Min, draw.Src)
	format := gfx.FormatRGBA8
	if key.srgb {
		format = gfx.FormatRGBA8Srgb
	}
	return loadedTexture{
		key: key, width: bounds.Dx(), height: bounds.Dy(), format: format, pixels: rgba.Pix,
	}, nil
}

// textureReportPath names a texture in a report. An external image is named by
// its storage path, which is what a caller can act on; an embedded one is named
// by the file and the index, because that is the whole of its identity.
func textureReportPath(key textureKey) string {
	if key.image == externalImage {
		return key.path
	}
	return fmt.Sprintf("%s#image%d", key.path, key.image)
}

// defaultModelSampler is glTF's own default for a texture that names no
// sampler: repeat on both axes, filtered linearly at every step.
var defaultModelSampler = gfx.SamplerDesc{AddressU: gfx.AddressRepeat, AddressV: gfx.AddressRepeat}

// modelSampler maps one glTF sampler onto gfx's. glTF specifies magnification,
// minification and mip selection independently, which is exactly the shape
// SamplerDesc took to hold them, so the mapping is three lookups and no
// interpretation.
//
// Anisotropy is left off: glTF has no field for it, and turning it on for every
// model texture would be scene deciding a quality/bandwidth trade on the app's
// behalf with nothing in the file to justify it.
func modelSampler(doc *gltf.Document, index int) gfx.SamplerDesc {
	if index < 0 || index >= len(doc.Samplers) || doc.Samplers[index] == nil {
		return defaultModelSampler
	}
	sampler := doc.Samplers[index]
	desc := gfx.SamplerDesc{
		AddressU: addressMode(sampler.WrapS),
		AddressV: addressMode(sampler.WrapT),
	}
	if sampler.MagFilter == gltf.MagNearest {
		desc.Mag = gfx.FilterNearest
	}
	switch sampler.MinFilter {
	case gltf.MinNearest:
		desc.Min = gfx.FilterNearest
	case gltf.MinNearestMipMapNearest:
		desc.Min, desc.Mip = gfx.FilterNearest, gfx.FilterNearest
	case gltf.MinNearestMipMapLinear:
		desc.Min = gfx.FilterNearest
	case gltf.MinLinearMipMapNearest:
		desc.Mip = gfx.FilterNearest
	}
	return desc
}

func addressMode(wrap gltf.WrappingMode) gfx.AddressMode {
	switch wrap {
	case gltf.WrapClampToEdge:
		return gfx.AddressClamp
	case gltf.WrapMirroredRepeat:
		return gfx.AddressMirror
	}
	return gfx.AddressRepeat
}
