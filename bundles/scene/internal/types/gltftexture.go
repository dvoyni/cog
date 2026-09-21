package types

import (
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"

	"github.com/dvoyni/cog/libs/assets"
	"github.com/dvoyni/cog/slots/gfx"
	"github.com/qmuntal/gltf"
)

// missingTexture is the slot value for a texture the file itself could not
// name - no texture at that index, no image source, an image the container
// holds no bytes for. The model still becomes resident and the slot binds its
// own per-slot default, because a model missing one texture is a model you can
// see and fix; a model that failed wholesale over a missing picture is a level
// with a hole in it.
//
// A picture that was named and did not arrive is a different thing and does not
// come through here: it has a cache entry, and what it binds is that entry's
// placeholder.
const missingTexture = -1

// textureRequests resolves the images one model's materials name into the
// descriptors the texture cache is asked with, once per descriptor.
//
// It names rather than decodes, and that is the whole of what reopening the
// texture table as a cache changed here. The decode used to run in the parse
// and the scene-wide table was consulted afterwards, at install time - which is
// why two models sharing an external image both decoded it and only the first
// uploaded it. A cache is consulted before the read, so the second model now
// reads nothing and decodes nothing, and what is left here is turning a glTF
// texture reference into a name, a colour space and - for a picture the file
// embeds - the bytes to supply beside that name.
type textureRequests struct {
	doc       *gltf.Document
	modelPath string
	// asked maps a request - the descriptor as the cache keys it, without the
	// payload - to its index in descrs, so nine slots over three images are
	// three entries and three Gets.
	asked   map[textureDescr]int
	descrs  []textureDescr
	reports []error
}

func newTextureRequests(doc *gltf.Document, modelPath string) *textureRequests {
	return &textureRequests{doc: doc, modelPath: modelPath, asked: map[textureDescr]int{}}
}

// texture resolves one glTF texture reference into the image it names and the
// sampler that reads it, or reports missingTexture. srgb selects the colour
// space the slot binds it in.
func (r *textureRequests) texture(index int, srgb bool) (int, gfx.SamplerDesc) {
	sampler := defaultModelSampler
	if index < 0 || index >= len(r.doc.Textures) || r.doc.Textures[index] == nil {
		r.reports = append(r.reports, ErrModelTextureUnavailable{
			Model: r.modelPath, Texture: fmt.Sprintf("%d", index),
			Err: errors.New("the file has no texture there"),
		})
		return missingTexture, sampler
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
		return missingTexture, sampler
	}
	return r.image(*texture.Source, srgb), sampler
}

// image names one of the file's images in one colour space, once.
func (r *textureRequests) image(source int, srgb bool) int {
	if source < 0 || source >= len(r.doc.Images) || r.doc.Images[source] == nil {
		r.reports = append(r.reports, ErrModelTextureUnavailable{
			Model: r.modelPath, Texture: fmt.Sprintf("image %d", source),
			Err: errors.New("the file has no image there"),
		})
		return missingTexture
	}
	entry := r.doc.Images[source]
	request := textureDescr{
		Name: r.modelPath, Params: textureDescrParams{image: source, srgb: srgb},
	}
	if resolved, external := r.resolvePath(entry); external {
		request = textureDescr{
			Name: resolved, Params: textureDescrParams{image: externalImage, srgb: srgb},
		}
	}
	if index, seen := r.asked[request]; seen {
		return index
	}
	descr := request
	// An embedded picture has no path of its own, so its bytes are supplied
	// beside the model's name and the Library never opens the container to look
	// for them. Reading them here costs a slice of a buffer the parse has
	// already decoded, except for a base64 data URI, which is the one case that
	// allocates.
	if request.Params.image != externalImage {
		encoded, err := r.imageBytes(entry)
		if err != nil {
			r.reports = append(r.reports, ErrModelTextureUnavailable{
				Model:   r.modelPath,
				Texture: textureReportPath(request.Name, request.Params.image),
				Err:     err,
			})
			// The failure is remembered as the same miss, so a nine-texture
			// material over one unreadable image is one report rather than nine.
			r.asked[request] = missingTexture
			return missingTexture
		}
		descr.Blob = assets.NewBlob(encoded)
	}
	r.asked[request] = len(r.descrs)
	r.descrs = append(r.descrs, descr)
	return len(r.descrs) - 1
}

// resolvePath reports the storage path an image's URI names, and whether it has
// one at all. A buffer view and a base64 data URI are both internal to the file
// and are named by it instead.
func (r *textureRequests) resolvePath(entry *gltf.Image) (string, bool) {
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
// in. A picture with a storage path of its own never reaches here: the Library
// opens that one itself, which is also why this needs no filesystem.
func (r *textureRequests) imageBytes(entry *gltf.Image) ([]byte, error) {
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

// defaultModelSampler is what a texture that names no sampler reads with:
// repeat on both axes, which glTF specifies, and linear filtering at every
// step, which it does not. glTF asks for "auto filtering" there and leaves the
// choice to the runtime; linear is cog's. The same choice fills any filter a
// declared sampler omits, since FilterLinear is the zero value.
//
// It is deliberately not a report: the file is valid glTF, and whether linear
// is wrong for it - a pixel-art atlas - is the consumer's judgement, not scene's.
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
