package canvas

import (
	"encoding/binary"
	"iter"
)

// encodedSpan is a span of a run of text as a layout's key holds it: an
// icon's name, or the span's text.
type encodedSpan struct {
	icon    bool
	content string
}

// appendEncodedSpans appends spans to encoded, as a layout's key holds
// them, and answers it: each span whether it is an icon, then the length of
// its icon's name or its text as a uvarint, then the name or the text, and
// an icon's serial in icons as a uvarint, zero for a name none is
// registered under, so a name registered again lays out anew. Their tints
// are left out, since they shape nothing.
func appendEncodedSpans(encoded []byte, spans []Span, icons map[string]registeredIcon) []byte {
	for _, span := range spans {
		kind := byte(0)
		content := span.Text
		if span.Icon != "" {
			kind = 1
			content = span.Icon
		}
		encoded = append(encoded, kind)
		encoded = binary.AppendUvarint(encoded, uint64(len(content)))
		encoded = append(encoded, content...)
		if span.Icon != "" {
			encoded = binary.AppendUvarint(encoded, icons[span.Icon].serial)
		}
	}
	return encoded
}

// decodeSpans answers the spans encoded holds, as appendEncodedSpans
// encodes them, in order, their icons' serials skipped.
func decodeSpans(encoded string) iter.Seq[encodedSpan] {
	return func(yield func(encodedSpan) bool) {
		for len(encoded) > 0 {
			icon := encoded[0] == 1
			length, lengthSize := binary.Uvarint([]byte(encoded[1:min(len(encoded), 1+binary.MaxVarintLen64)]))
			start := 1 + lengthSize
			end := start + int(length)
			if !yield(encodedSpan{icon: icon, content: encoded[start:end]}) {
				return
			}
			encoded = encoded[end:]
			if icon {
				_, serialSize := binary.Uvarint([]byte(encoded[:min(len(encoded), binary.MaxVarintLen64)]))
				encoded = encoded[serialSize:]
			}
		}
	}
}
