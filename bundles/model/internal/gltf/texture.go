package gltf

import (
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"

	"github.com/dvoyni/cog/slots/gfx"
	"github.com/qmuntal/gltf"
)

// NoImage is the image index of a material slot the file itself could not
// name - no texture at that index, no image source, an image the container
// holds no bytes for. The slot still binds its own per-slot default, because a
// model missing one texture is a model you can see and fix.
const NoImage = -1

// Image is one picture a model's materials name, in one colour space: a plain
// reference the decoder hands over and model keys its texture cache by. The
// decoder names pictures and never decodes one.
//
// An external image is its resolved storage path, which is the whole of its
// identity. An embedded one is the model's path, the index of the image in the
// file, and the encoded bytes the file carries for it, because it has no path
// of its own.
//
// There is one Image per distinct picture and colour space: nine slots over
// three images are three Images. The colour space is part of the identity
// because it is the slot's property and not the image's - one image used as a
// base colour and as a normal map is two textures.
type Image struct {
	Path     string
	Embedded bool
	// Index is the image's index in the file's images array, meaningful only
	// for an embedded image.
	Index int
	SRGB  bool
	Bytes []byte
}

// imageKey interns an Image without its bytes.
type imageKey struct {
	path     string
	embedded bool
	index    int
	srgb     bool
}

// imageRequests resolves the images one model's materials name, once per
// picture and colour space.
type imageRequests struct {
	doc       *gltf.Document
	modelPath string
	// asked maps a request to its index in images, or to NoImage for one that
	// already failed, so a nine-texture material over one unreadable image is
	// one report rather than nine.
	asked   map[imageKey]int
	images  []Image
	reports []error
}

func newImageRequests(doc *gltf.Document, modelPath string) *imageRequests {
	return &imageRequests{doc: doc, modelPath: modelPath, asked: map[imageKey]int{}}
}

// texture resolves one glTF texture reference into the image it names and the
// sampler that reads it, or reports NoImage. srgb selects the colour space the
// slot binds it in.
func (r *imageRequests) texture(index int, srgb bool) (int, gfx.SamplerDesc) {
	sampler := DefaultSampler
	if index < 0 || index >= len(r.doc.Textures) || r.doc.Textures[index] == nil {
		r.reports = append(r.reports, ErrModelTextureUnavailable{
			Model: r.modelPath, Texture: fmt.Sprintf("%d", index),
			Err: errors.New("the file has no texture there"),
		})
		return NoImage, sampler
	}
	texture := r.doc.Textures[index]
	if texture.Sampler != nil {
		sampler = modelSampler(r.doc, *texture.Sampler)
	}
	if texture.Source == nil {
		r.reports = append(r.reports, ErrModelTextureUnavailable{
			Model: r.modelPath, Texture: texture.Name,
			Err: errors.New("it names no image source"),
		})
		return NoImage, sampler
	}
	return r.image(*texture.Source, srgb), sampler
}

// image names one of the file's images in one colour space, once.
func (r *imageRequests) image(source int, srgb bool) int {
	if source < 0 || source >= len(r.doc.Images) || r.doc.Images[source] == nil {
		r.reports = append(r.reports, ErrModelTextureUnavailable{
			Model: r.modelPath, Texture: fmt.Sprintf("image %d", source),
			Err: errors.New("the file has no image there"),
		})
		return NoImage
	}
	entry := r.doc.Images[source]
	request := imageKey{path: r.modelPath, embedded: true, index: source, srgb: srgb}
	if resolved, external := r.resolvePath(entry); external {
		request = imageKey{path: resolved, index: NoImage, srgb: srgb}
	}
	if index, seen := r.asked[request]; seen {
		return index
	}
	image := Image{Path: request.path, Embedded: request.embedded, Index: request.index, SRGB: srgb}
	// An embedded picture has no path of its own, so its bytes are handed over
	// beside the model's name and nothing ever opens the container again to
	// look for them. Reading them here costs a slice of a buffer the parse has
	// already decoded, except for a base64 data URI, which is the one case that
	// allocates.
	if request.embedded {
		encoded, err := r.imageBytes(entry)
		if err != nil {
			r.reports = append(r.reports, ErrModelTextureUnavailable{
				Model:   r.modelPath,
				Texture: fmt.Sprintf("%s#image%d", request.path, request.index),
				Err:     err,
			})
			r.asked[request] = NoImage
			return NoImage
		}
		image.Bytes = encoded
	}
	r.asked[request] = len(r.images)
	r.images = append(r.images, image)
	return len(r.images) - 1
}

// resolvePath reports the storage path an image's URI names, and whether it has
// one at all. A buffer view and a base64 data URI are both internal to the file
// and are named by it instead.
func (r *imageRequests) resolvePath(entry *gltf.Image) (string, bool) {
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
	return path.Join(path.Dir(r.modelPath), unescaped), true
}

// imageBytes reads one embedded image's encoded bytes out of the file it rides
// in. A picture with a storage path of its own never reaches here: whoever
// loads it opens that one itself, which is also why this needs no filesystem.
func (r *imageRequests) imageBytes(entry *gltf.Image) ([]byte, error) {
	if entry.BufferView != nil {
		if *entry.BufferView < 0 || *entry.BufferView >= len(r.doc.BufferViews) {
			return nil, errors.New("its bufferView index is past the end of the file")
		}
		return readBufferViewBytes(r.doc, r.doc.BufferViews[*entry.BufferView])
	}
	if entry.IsEmbeddedResource() {
		return entry.MarshalData()
	}
	return nil, errors.New("it has neither a bufferView nor a URI")
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

// DefaultSampler is what a texture that names no sampler reads with: repeat on
// both axes, which glTF specifies, and linear filtering at every step, which it
// does not. glTF asks for "auto filtering" there and leaves the choice to the
// runtime; linear is cog's. The same choice fills any filter a declared sampler
// omits, since FilterLinear is the zero value.
//
// It is deliberately not a report: the file is valid glTF, and whether linear
// is wrong for it - a pixel-art atlas - is the consumer's judgement.
var DefaultSampler = gfx.SamplerDesc{AddressU: gfx.AddressRepeat, AddressV: gfx.AddressRepeat}

// modelSampler maps one glTF sampler onto gfx's. glTF specifies magnification,
// minification and mip selection independently, which is exactly the shape
// SamplerDesc took to hold them, so the mapping is three lookups and no
// interpretation.
//
// Anisotropy is left off: glTF has no field for it, and turning it on for every
// model texture would be deciding a quality/bandwidth trade on the app's behalf
// with nothing in the file to justify it.
func modelSampler(doc *gltf.Document, index int) gfx.SamplerDesc {
	if index < 0 || index >= len(doc.Samplers) || doc.Samplers[index] == nil {
		return DefaultSampler
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
