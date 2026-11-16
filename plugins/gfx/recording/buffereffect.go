package recording

import "encoding/binary"

// bufferEffect is what a write, a copy, a clear, a resolve or a timestamp
// write does to a buffer's or a query set's bytes at the submit, as its kind
// says: source copied into destination, destination cleared, or the tick
// ticks after the command buffer's first written into destination.
type bufferEffect struct {
	kind        bufferEffectKind
	destination []byte
	source      []byte
	ticks       uint64
}

// bufferEffectKind is what a buffer effect does.
type bufferEffectKind uint8

const (
	// bufferEffectKindCopy copies an effect's source into its destination.
	bufferEffectKindCopy bufferEffectKind = iota
	// bufferEffectKindClear zeroes its destination.
	bufferEffectKindClear
	// bufferEffectKindTimestamp writes its tick into its destination.
	bufferEffectKindTimestamp
)

// applyBufferEffects applies effects in order, their timestamps counted from
// firstTick, and answers them emptied, cleared first so they hold no
// buffer's bytes while their array is kept for the next.
func applyBufferEffects(effects []bufferEffect, firstTick uint64) []bufferEffect {
	for _, effect := range effects {
		switch effect.kind {
		case bufferEffectKindCopy:
			copy(effect.destination, effect.source)
		case bufferEffectKindClear:
			clear(effect.destination)
		case bufferEffectKindTimestamp:
			binary.LittleEndian.PutUint64(effect.destination, firstTick+effect.ticks)
		}
	}
	clear(effects)
	return effects[:0]
}
