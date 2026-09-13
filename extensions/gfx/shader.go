package gfx

import "github.com/dvoyni/cog/extensions/gfx/internal"

// ShaderDescr describes a shader by inline source text (ShaderWithText) or a
// resource path (ShaderWithResource), resolved to bytes by the renderer.
//
// A descriptor also carries its supply - the defines and const values the
// preprocessor resolves the source against. A root source plus one supply is
// one variant, and two supplies over one path are two shaders, so the supply is
// part of the descriptor's identity everywhere identity is decided.
type ShaderDescr = internal.ShaderDescr

const (
	ShaderSourceText     = internal.ShaderSourceText
	ShaderSourceResource = internal.ShaderSourceResource
)

// ShaderOption is one entry of a shader's supply: a define or a const. Build it
// with ShaderDefine or ShaderConst.
type ShaderOption = internal.ShaderOption

// ShaderDefine supplies a valueless flag, readable by the source's #if
// conditionals and never reaching WGSL. Supplying a define a source does not
// mention is harmless; nothing can unset one.
func ShaderDefine(name string) ShaderOption {
	return internal.ShaderDefine(name)
}

// ShaderConst supplies a value for a #const the source declares, overriding
// that declaration's default. The value is WGSL text the preprocessor never
// interprets - the type rides in the value, so "16" and "16u" differ - and a
// name no source declares is silently ignored, which keeps one const map usable
// across a family of shaders.
func ShaderConst(name, value string) ShaderOption {
	return internal.ShaderConst(name, value)
}

// ShaderWithText describes a shader from inline source bytes (e.g. WGSL).
func ShaderWithText(text string, opts ...ShaderOption) ShaderDescr {
	return internal.ShaderWithText(text, opts...)
}

// ShaderWithResource describes a shader loaded from storage.FileSystem.
func ShaderWithResource(path string, opts ...ShaderOption) ShaderDescr {
	return internal.ShaderWithResource(path, opts...)
}
