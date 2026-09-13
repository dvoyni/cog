package gfx

import (
	"strconv"
	"strings"
)

// ShaderLocation names one line of one shader source. Line is 1-based; a zero
// value means the supply, which has no source line to point at.
type ShaderLocation struct {
	Source string // storage name of the .wgsl file
	Line   int    // 1-based
}

func (l ShaderLocation) String() string {
	if l.Source == "" && l.Line == 0 {
		return "the supply"
	}
	return l.Source + ":" + strconv.Itoa(l.Line)
}

// ShaderSourceMap says where every line of a flattened shader module came from.
//
// It is a table rather than a per-output-line array because the flatten is
// line-preserving by construction: comments, consumed directives and skipped
// branches blank rather than being deleted, splicing only reorders whole runs,
// and a #const injects its const on the directive's own line. So the output is a
// concatenation of contiguous segments with 1:1 line correspondence inside each,
// which a table describes as precisely as an array would at roughly one entry
// per source. That property is the thing to preserve, not the data structure.
type ShaderSourceMap struct {
	Segments []ShaderSegment // in output order, contiguous, covering every line
}

// ShaderSegment is one run of flattened output lines coming from one source.
//
// The hoisted WGSL §4 prologue is the one segment with no source
// correspondence: its Source is empty and its lines map to nothing.
type ShaderSegment struct {
	OutputStart  int            // 1-based line in the flattened text
	Length       int            // lines
	Source       string         // storage name; empty for the hoisted prologue
	SourceStart  int            // 1-based line in Source
	IncludedFrom ShaderLocation // the #include line that pulled it in; zero for the root
}

// locate maps a 1-based flattened line back to the source line it came from,
// reporting false for a line the map does not cover and for the hoisted
// prologue, which came from no source.
func (m ShaderSourceMap) locate(line int) (ShaderLocation, bool) {
	for _, segment := range m.Segments {
		if line < segment.OutputStart || line >= segment.OutputStart+segment.Length {
			continue
		}
		if segment.Source == "" {
			return ShaderLocation{}, false
		}
		return ShaderLocation{Source: segment.Source, Line: segment.SourceStart + line - segment.OutputStart}, true
	}
	return ShaderLocation{}, false
}

// render writes the segment table on one line, which is what a backend's error
// is appended to. It is one line rather than one entry per segment because the
// default kernel error handler is log.Printf, so an error must read as a single
// printed block.
func (m ShaderSourceMap) render() string {
	var b strings.Builder
	for i, segment := range m.Segments {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(strconv.Itoa(segment.OutputStart))
		b.WriteByte('-')
		b.WriteString(strconv.Itoa(segment.OutputStart + segment.Length - 1))
		b.WriteByte(' ')
		if segment.Source == "" {
			b.WriteString("(hoisted prologue)")
			continue
		}
		b.WriteString(segment.Source)
		if segment.SourceStart != 1 {
			b.WriteString("@" + strconv.Itoa(segment.SourceStart))
		}
		if segment.IncludedFrom != (ShaderLocation{}) {
			b.WriteString(" (" + segment.IncludedFrom.String() + ")")
		}
	}
	return b.String()
}
