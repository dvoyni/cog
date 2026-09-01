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
