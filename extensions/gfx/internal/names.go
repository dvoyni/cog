package internal

import "strconv"

// The name tables. They live here rather than in the contract root on purpose:
// naming an enum for a debug document is not the same promise as giving every
// gfx enum a String, which would put a rendering of every member into the
// package's public surface for the sake of one reader. The root's views and
// gfximpl's frame snapshot both read them. An unknown value names its ordinal rather than
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

func BufferKindName(kind BufferKind) string {
	switch kind {
	case BufferVertex:
		return "vertex"
	case BufferIndex:
		return "index"
	case BufferUniform:
		return "uniform"
	case BufferStorage:
		return "storage"
	}
	return UnknownName(int(kind))
}

func AddressModeName(mode AddressMode) string {
	switch mode {
	case AddressClamp:
		return "clamp"
	case AddressRepeat:
		return "repeat"
	case AddressMirror:
		return "mirror"
	}
	return UnknownName(int(mode))
}

func FilterModeName(mode FilterMode) string {
	switch mode {
	case FilterLinear:
		return "linear"
	case FilterNearest:
		return "nearest"
	}
	return UnknownName(int(mode))
}

func BlendModeName(mode BlendMode) string {
	switch mode {
	case BlendAlpha:
		return "alpha"
	case BlendOpaque:
		return "opaque"
	case BlendAdditive:
		return "additive"
	case BlendMultiply:
		return "multiply"
	}
	return UnknownName(int(mode))
}

func CompareFuncName(compare CompareFunc) string {
	switch compare {
	case CompareAlways:
		return "always"
	case CompareNever:
		return "never"
	case CompareLess:
		return "less"
	case CompareLessEqual:
		return "lessEqual"
	case CompareGreater:
		return "greater"
	case CompareGreaterEqual:
		return "greaterEqual"
	case CompareEqual:
		return "equal"
	case CompareNotEqual:
		return "notEqual"
	}
	return UnknownName(int(compare))
}

func CullModeName(mode CullMode) string {
	switch mode {
	case CullNone:
		return "none"
	case CullFront:
		return "front"
	case CullBack:
		return "back"
	}
	return UnknownName(int(mode))
}

func FrontFaceName(face FrontFace) string {
	switch face {
	case FrontCCW:
		return "ccw"
	case FrontCW:
		return "cw"
	}
	return UnknownName(int(face))
}

func LoadOpName(load LoadOp) string {
	switch load {
	case LoadPreserve:
		return "preserve"
	case LoadClear:
		return "clear"
	case LoadDiscard:
		return "discard"
	}
	return UnknownName(int(load))
}

func StoreOpName(store StoreOp) string {
	switch store {
	case StoreKeep:
		return "keep"
	case StoreDiscard:
		return "discard"
	}
	return UnknownName(int(store))
}

// UnknownName spells a value no table names, so an enum that grew reads as
// unknown rather than as whichever name happened to be last.
func UnknownName(value int) string {
	return "unknown(" + strconv.Itoa(value) + ")"
}
