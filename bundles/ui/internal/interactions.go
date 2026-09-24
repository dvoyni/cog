package internal

import (
	"iter"
	"slices"
	"strings"
)

// Interactions contains the results from the most recently processed UI frame.
type Interactions struct {
	values []Interaction
}

// All yields interaction values from the most recently processed UI frame.
// Interactions of the same kind are ordered back-to-front, with the topmost last.
func (interactions *Interactions) All() iter.Seq[Interaction] {
	values := interactions.values
	return func(yield func(Interaction) bool) {
		for index := range values {
			if !yield(values[index]) {
				return
			}
		}
	}
}

// Has reports whether the topmost interaction of kind matches id and button.
// It returns the matched element's user data. When consume is true, a match
// removes all interactions of that kind.
func (interactions *Interactions) Has(id ID, kind InteractionKind, button int, consume bool) (bool, any) {
	for index := len(interactions.values) - 1; index >= 0; index-- {
		interaction := interactions.values[index]
		if interaction.Kind != kind {
			continue
		}
		if interaction.Button != button {
			return false, nil
		}
		if !strings.HasPrefix(string(interaction.ID), string(id)) {
			return false, nil
		}
		if consume {
			interactions.values = slices.DeleteFunc(interactions.values, func(interaction Interaction) bool {
				return interaction.Kind == kind
			})
		}
		return true, interaction.userData
	}
	return false, nil
}

func (interactions *Interactions) Clicked(id ID) bool {
	result, _ := interactions.Has(id, InteractionClick, 0, true)
	return result
}

// Clear discards every interaction from the last processed frame, so whoever
// reads them next sees a frame nobody touched.
//
// It exists for the tick whose input belongs to nobody. An app that holds a
// view change in flight - a screen fading out while the next one fades in - has
// a stretch of frames where a click would land on a control that is halfway
// gone; clearing here, once, after the plugin has processed the frame, swallows
// that stretch for every consumer at once, instead of each of them asking
// whether it should be listening.
//
// Hover interactions go with the rest, so a caller's own tooltip or highlight
// stops too - but the VisualHovered state the processor stamps on an element is
// hit-tested during layout, not read back from here, so a button still lights
// up under the pointer. Clearing is about what the app acts on, not about what
// the UI looks like.
//
// The buffer is kept, only emptied: the plugin swaps it back in next tick.
func (interactions *Interactions) Clear() {
	interactions.values = interactions.values[:0]
}
