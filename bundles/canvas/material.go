package canvas

import "github.com/dvoyni/cog/bundles/canvas/internal"

// MaterialSet is the shading a scope - the queue, a layer, a ui.Frame or a ui
// element subtree - supplies to the draws beneath it that name none of their
// own: one material per family (Sprite, Triangles, Texture), plus one parameter
// list shared by all three.
//
// A scope names a set rather than a material because a layer is never one
// family: every interesting layer carries sprites and triangles, and the two can
// never be one shader. A nil slot keeps its built-in, so an entirely zero set is
// the built-ins. One parameter list serves all three slots because gfx drops a
// name the bound shader never declared. docs/specs/materials.md carries the
// whole reasoning.
type MaterialSet = internal.MaterialSet
