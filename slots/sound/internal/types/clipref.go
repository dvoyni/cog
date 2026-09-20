package types

import (
	"strconv"

	"github.com/dvoyni/cog/libs/assets"
)

// clipParams is what composes into a Clip's cache key beside its source, and it
// is empty. gfx composes a colour space and canvas a pixel size; a Clip has no
// variant dimension, because rate is a Voice parameter applied at playback,
// stereo is never downmixed, and there is no bake. That holds only because an
// Adapter may not bake the Device rate into what it prepares, so a Device
// change costs the cache nothing.
type clipParams struct{}

// ClipRef names a Clip: a path read through storage, or encoded Ogg bytes the
// caller already holds. It is a request and never a handle - there is no
// LoadSound and nothing a game must release - so nothing here learns anything
// from the load it names.
//
// Compare two with Equal rather than ==. Equal is Clip identity and == is field
// identity, and they differ: a Clip with a path is named by that path, so two
// refs spelling one path are one Clip whatever bytes either carries beside it.
// That is the Library's own key rule, and Equal is where a caller meets it.
type ClipRef struct {
	name string
	blob assets.Blob
}

// ClipWithResource names a Clip by a storage path. The read happens inside
// sound's flush, on the tick the Play or Preload that needs it was recorded.
func ClipWithResource(path string) ClipRef { return ClipRef{name: path} }

// ClipWithBytes names a Clip by encoded Ogg the caller holds. A Blob is a
// pointer and a length, so an Adapter streaming from those bytes references the
// same run and copies nothing.
//
// The bytes are the identity, with everything assets.Blob's own doc says about
// what that costs a call site building them fresh: build them once and keep
// them, or every call names an asset nothing can ask for twice.
func ClipWithBytes(ogg assets.Blob) ClipRef { return ClipRef{blob: ogg} }

// Equal reports whether r and other name one Clip. A ref with a path is
// identified by that path alone; a ref with no path is identified by its bytes.
func (r ClipRef) Equal(other ClipRef) bool {
	if r.name != "" || other.name != "" {
		return r.name == other.name
	}
	return r.blob == other.blob
}

// descr is the Library descriptor r names. A Name wins when it is set, so bytes
// supplied beside a path are a payload rather than part of the identity.
func (r ClipRef) descr() assets.Descr[clipParams] {
	return assets.Descr[clipParams]{Name: r.name, Blob: r.blob}
}

// key is r reduced to what identifies it, which is what sound's own clip table
// is spelled with so that it keys a Clip exactly as the Library does.
func (r ClipRef) key() ClipRef {
	if r.name != "" {
		r.blob = assets.Blob{}
	}
	return r
}

// empty reports whether r names nothing at all, which a Play on a zero ClipRef
// is and which no read can turn into a Clip.
func (r ClipRef) empty() bool { return r.name == "" && r.blob.Len() == 0 }

// describe renders r for a message that has to say which Clip failed. It is
// unexported because rendering a ClipRef is sound's own need: the type is
// opaque to a game, and an exported String would be a second way to read the
// path back out of a request.
func (r ClipRef) describe() string {
	if r.name != "" {
		return r.name
	}
	if r.blob.Len() == 0 {
		return "no clip"
	}
	return "clip of " + strconv.Itoa(r.blob.Len()) + " bytes"
}
