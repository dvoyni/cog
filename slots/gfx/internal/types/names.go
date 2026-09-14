package types

import "strconv"

// The name tables of the recorder-side enums. They are plain functions rather
// than methods on purpose: naming an enum for a debug document is not the same
// promise as giving every gfx enum a String. The views and gfx's frame snapshot
// both read them; the GPU vocabulary's enums name themselves through their Name
// methods. An unknown value names its ordinal rather than
// falling back to a legal-looking name, so a member added without touching
// this file is visible instead of mislabelled.

func TextureSourceName(source textureSource) string {
	switch source {
	case TextureSourceResource:
		return "resource"
	case TextureSourceBytes:
		return "bytes"
	case TextureSourceBaked:
		return "baked"
	}
	return UnknownName(int(source))
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
