package ui

import (
	"iter"
	"slices"
	"strings"
)

type interactions struct {
	values []Interaction
}

// All yields interaction values from the most recently processed UI frame.
// Interactions of the same kind are ordered back-to-front, with the topmost last.
func (interactions *interactions) All() iter.Seq[Interaction] {
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
func (interactions *interactions) Has(id ID, kind InteractionKind, button int, consume bool) (bool, any) {
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

func (interactions *interactions) Clicked(id ID) bool {
	result, _ := interactions.Has(id, InteractionClick, 0, true)
	return result
}

// HoverTracker remembers which element the pointer is over and how long it has
// rested there, so callers can decide whether to show a tooltip without pairing
// up InteractionIn and InteractionOut themselves.
//
// Interactions describe the frame the plugin last processed, so the tracker
// trails the pointer by one frame. The zero value reports hover immediately.
type HoverTracker struct {
	// Dwell is how long the pointer must rest before Hovered reports an element.
	// Zero reports immediately. Set it once, when the tracker is created: it is
	// read every frame but is not meant to vary between them.
	Dwell float32

	hovered ID
	elapsed float32
	shown   bool
}

// Update records this frame's hover target and advances the dwell. Call it once
// per frame, before building that frame's UI.
func (tracker *HoverTracker) Update(interactions *Interactions, delta float32) {
	target := ID("")
	if interactions != nil {
		for interaction := range interactions.All() {
			if interaction.Kind == InteractionHover {
				target = interaction.ID
			}
		}
	}

	switch {
	case target == "":
		// The pointer left every element, so the next one waits out the dwell
		// again. Gaps between neighbouring elements count as leaving.
		tracker.shown = false
		tracker.elapsed = 0
	case target != tracker.hovered:
		// Moving straight from one element to another while something is already
		// shown shows the next one at once. The dwell is there to stop tooltips
		// strobing as the pointer crosses a toolbar, not to slow down reading.
		if !tracker.shown {
			tracker.elapsed = 0
		}
	default:
		tracker.elapsed += delta
	}
	if target != "" && tracker.elapsed >= tracker.Dwell {
		tracker.shown = true
	}
	tracker.hovered = target
}

// Hovered reports whether id is the element under the pointer, and has been for
// at least Dwell.
func (tracker *HoverTracker) Hovered(id ID) bool {
	return id != "" && tracker.shown && tracker.hovered == id
}
