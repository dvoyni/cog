package canvas

import "github.com/dvoyni/cog/bundles/canvas/internal"

// ArmDrawsCmd arms one draw snapshot and hands back the wait. It is ordinary
// canvas API: anything holding a kernel handle may ask what a tick recorded,
// and the agent-facing capability is one caller among them.
//
// The response's channel is the only delivery path, and refusals travel it
// too, because DrawsSnapshot carries Err - the same shape gfx's arms use, for
// the same reason: a channel passed in with the request would leave canvas
// unable to refuse a second arm synchronously.
type ArmDrawsCmd = internal.ArmDrawsCmd

// ArmDrawsRequest carries the filter, because the filter is what bounds the
// work done inside the tick. Nothing about the output is decided before the
// request is known: the serialization runs in the tick, and by then it already
// knows what was asked for.
type ArmDrawsRequest = internal.ArmDrawsRequest

// ArmDrawsResponse hands back the wait and the viewport.
type ArmDrawsResponse = internal.ArmDrawsResponse

// SetFallbackLanguageCmd makes one of Config.FallbackFonts, named by its
// language, the fallback tried first for text outside a $[language]{...} span.
// Characters already drawn keep their font until ClearGlyphsCmd.
type SetFallbackLanguageCmd = internal.SetFallbackLanguageCmd

// SetFallbackLanguageRequest names the fallback language; empty tries the
// fallbacks in configured order.
type SetFallbackLanguageRequest = internal.SetFallbackLanguageRequest

// SetFallbackLanguageResponse carries ErrUnknownFallbackLanguage for a language
// no fallback is configured for.
type SetFallbackLanguageResponse = internal.SetFallbackLanguageResponse

// ClearGlyphsCmd drops the glyph atlas and every baked face at the next flush,
// so every character is rasterized again along the current fallback chain.
type ClearGlyphsCmd = internal.ClearGlyphsCmd

// ClearGlyphsRequest is the empty request to ClearGlyphsCmd.
type ClearGlyphsRequest = internal.ClearGlyphsRequest

// ClearGlyphsResponse is the empty response from ClearGlyphsCmd.
type ClearGlyphsResponse = internal.ClearGlyphsResponse
