package gfx

import "github.com/dvoyni/cog/extensions/gfx/internal"

// ShaderLocation names one line of one shader source. Line is 1-based; a zero
// value means the supply, which has no source line to point at.
type ShaderLocation = internal.ShaderLocation

// ShaderSourceMap says where every line of a flattened shader module came from.
//
// It is a table rather than a per-output-line array because the flatten is
// line-preserving by construction: comments, consumed directives and skipped
// branches blank rather than being deleted, splicing only reorders whole runs,
// and a #const injects its const on the directive's own line. So the output is a
// concatenation of contiguous segments with 1:1 line correspondence inside each,
// which a table describes as precisely as an array would at roughly one entry
// per source. That property is the thing to preserve, not the data structure.
type ShaderSourceMap = internal.ShaderSourceMap

// ShaderSegment is one run of flattened output lines coming from one source.
//
// The hoisted WGSL §4 prologue is the one segment with no source
// correspondence: its Source is empty and its lines map to nothing.
type ShaderSegment = internal.ShaderSegment
