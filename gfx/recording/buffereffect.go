package recording

// bufferEffect is what a write, a copy or a clear does to a buffer's bytes at
// the submit: source copied into destination, or destination cleared.
type bufferEffect struct {
	destination []byte
	source      []byte
	cleared     bool
}

// applyBufferEffects applies effects in order, and answers them emptied,
// cleared first so they hold no buffer's bytes while their array is kept for
// the next.
func applyBufferEffects(effects []bufferEffect) []bufferEffect {
	for _, effect := range effects {
		if effect.cleared {
			clear(effect.destination)
		} else {
			copy(effect.destination, effect.source)
		}
	}
	clear(effects)
	return effects[:0]
}
