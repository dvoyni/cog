package internal

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// ArmDrawsCmd arms one draw snapshot and hands back the wait. It is ordinary
// canvas API: anything holding a kernel handle may ask what a tick recorded,
// and the agent-facing capability is one caller among them.
//
// The response's channel is the only delivery path, and refusals travel it
// too, because DrawsSnapshot carries Err - the same shape gfx's arms use, for
// the same reason: a channel passed in with the request would leave canvas
// unable to refuse a second arm synchronously.
type ArmDrawsCmd kernel.Command[ArmDrawsRequest, ArmDrawsResponse]

// ArmDrawsRequest carries the filter, because the filter is what bounds the
// work done inside the tick. Nothing about the output is decided before the
// request is known: the serialization runs in the tick, and by then it already
// knows what was asked for.
type ArmDrawsRequest struct {
	// FromLayer and ToLayer bound the layers kept, inclusive, and an absent one is
	// unbounded on that side. Layers they drop are counted rather than silently
	// missing, and every op keeps its own record index, so an index read off a
	// filtered snapshot still addresses the same op in an unfiltered one.
	FromLayer, ToLayer m.Maybe[int]
	// Kinds keeps only the recording calls named. An empty list keeps them all.
	Kinds []OpKind
	// Vertices are the record indices of the triangle ops whose vertices are
	// returned in full. Every other triangle op reports a count and a bounding
	// box, which is the only shape a frame of tens of thousands of vertices can
	// come back in.
	Vertices []int
}

// ArmDrawsResponse hands back the wait and the viewport.
type ArmDrawsResponse struct {
	// Done receives exactly one DrawsSnapshot and is buffered, so the game's
	// own goroutine never blocks on a caller that walked away.
	Done <-chan DrawsSnapshot
	// Viewport is the window as of the arm, which a capability body cannot read
	// for itself. A resize between the arm and the tick it binds to is a stated
	// non-guarantee, exactly as it is for a capture.
	Viewport gfx.Viewport
	// Err is the outcome an arm was refused with, exactly as it is for a gfx
	// capture.
	Err error
}

// SetFallbackLanguageCmd makes one of Config.FallbackFonts, named by its
// language, the fallback tried first for text outside a $[language]{...} span.
// A game sets it from the language the player reads in, so that a Han
// character draws the way that reader writes it.
//
// It changes what characters not met yet are drawn with. A face remembers which
// font drew each character it has drawn, so characters already on screen keep
// their font until ClearGlyphsCmd drops them.
type SetFallbackLanguageCmd kernel.Command[SetFallbackLanguageRequest, SetFallbackLanguageResponse]

// SetFallbackLanguageRequest names the fallback language. Empty tries the
// fallbacks in the order Config.FallbackFonts lists them.
type SetFallbackLanguageRequest struct{ Language string }

// SetFallbackLanguageResponse carries ErrUnknownFallbackLanguage when no
// fallback font is configured for the language, which leaves the current one
// in place.
type SetFallbackLanguageResponse struct{ Err error }

// ClearGlyphsCmd drops the glyph atlas and every baked face at the next flush,
// as a framebuffer scale change does, so every character is looked up along the
// fallback chain and rasterized again. It is what makes a SetFallbackLanguageCmd
// reach text already drawn.
type ClearGlyphsCmd kernel.Command[ClearGlyphsRequest, ClearGlyphsResponse]

// ClearGlyphsRequest is the empty request to ClearGlyphsCmd.
type ClearGlyphsRequest struct{}

// ClearGlyphsResponse is the empty response from ClearGlyphsCmd.
type ClearGlyphsResponse struct{}
