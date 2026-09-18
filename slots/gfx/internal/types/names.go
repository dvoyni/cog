package types

import "strconv"

// The name tables of the recorder-side enums. They are plain functions rather
// than methods on purpose: naming an enum for a debug document is not the same
// promise as giving every gfx enum a String. The views and gfx's frame snapshot
// both read them; the GPU vocabulary's enums name themselves through their Name
// methods. An unknown value names its ordinal rather than
// falling back to a legal-looking name, so a member added without touching
// this file is visible instead of mislabelled.

// TextureSourceName spells how a descriptor resolves. There is no enum behind
// it any more: the three cases are disjoint fields, so the name is read off
// whichever one carries the answer. A descriptor carrying none of them names no
// texture at all, and says so rather than reading as a path that is empty.
func TextureSourceName(texture TextureDescr) string {
	switch {
	case texture.Params.id != 0:
		return "baked"
	case texture.Name != "":
		return "resource"
	case texture.Blob.Len() != 0:
		return "bytes"
	}
	return "none"
}

func BufferSourceName(source bufferSource) string {
	switch source {
	case BufferSourceBytes:
		return "bytes"
	case BufferSourceBaked:
		return "baked"
	}
	return UnknownName(int(source))
}

// UnknownName spells a value no table names, so an enum that grew reads as
// unknown rather than as whichever name happened to be last.
func UnknownName(value int) string {
	return "unknown(" + strconv.Itoa(value) + ")"
}
