package ui

import (
	"iter"
	"slices"
	"strings"

	"github.com/dvoyni/cog/m"
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

// HoverTracker remembers what the pointer is resting on and for how long, so
// callers can decide whether to show a tooltip without pairing up InteractionIn
// and InteractionOut themselves.
//
// Interactions describe the frame the plugin last processed, so the tracker
// trails the pointer by one frame. The zero value reports hover immediately.
type HoverTracker struct {
	// Dwell is how long the pointer must rest before Hovered reports an element.
	// Zero reports immediately. Set it once, when the tracker is created: it is
	// read every frame but is not meant to vary between them.
	Dwell float32

	current    hoverTarget
	fallback   string
	fallbackAt m.Vec2
	previousAt m.Vec2
	elapsed    float32
	shown      bool
}

// hoverTarget is whatever the dwell is currently timing. The two fields are
// compared as a pair, so a fallback whose text happens to read like an ID is
// still not that element.
type hoverTarget struct {
	id       ID
	fallback string
}

func (target hoverTarget) empty() bool {
	return target.id == "" && target.fallback == ""
}

func (target hoverTarget) isFallback() bool {
	return target.fallback != ""
}

// Update records this frame's hover target and advances the dwell. Call it once
// per frame, before building that frame's UI.
func (tracker *HoverTracker) Update(interactions *Interactions, delta float32) {
	target := hoverTarget{fallback: tracker.fallback}
	if target.fallback == "" && interactions != nil {
		for interaction := range interactions.All() {
			if interaction.Kind == InteractionHover {
				target.id = interaction.ID
			}
		}
	}

	switch {
	case target.empty():
		// The pointer left everything, so the next target waits out the dwell
		// again. Gaps between neighbouring elements count as leaving.
		tracker.shown = false
		tracker.elapsed = 0
	case target.isFallback() != tracker.current.isFallback():
		// An element and a fallback are not neighbours to skim between. What a
		// fallback stands in for is usually the scene's screen-filling click
		// catcher, which has no tooltip of its own and which the pointer is over
		// the whole time it crosses the scene, so letting that count as a shown
		// target would spare every sprite its dwell.
		tracker.shown = false
		tracker.elapsed = 0
	case target != tracker.current:
		// Moving straight from one target to another while something is already
		// shown shows the next one at once. The dwell is there to stop tooltips
		// strobing as the pointer crosses a toolbar, not to slow down reading.
		if !tracker.shown {
			tracker.elapsed = 0
		}
	case target.isFallback() && tracker.fallbackAt != tracker.previousAt:
		// A sprite has no edges to hold the pointer inside, and a fallback's
		// identity is as coarse as the caller made it — every unit with the same
		// ability is one target here — so crossing the scene would otherwise run
		// the dwell out mid-sweep. The pointer counts as resting only while it
		// holds still.
		tracker.elapsed = 0
	default:
		tracker.elapsed += delta
	}
	if !target.empty() && tracker.elapsed >= tracker.Dwell {
		tracker.shown = true
	}
	tracker.current = target
	tracker.previousAt = tracker.fallbackAt
}

// Hovered reports whether id is the element under the pointer, and has been for
// at least Dwell. It is false for every id while a fallback is set.
func (tracker *HoverTracker) Hovered(id ID) bool {
	return id != "" && tracker.shown && tracker.current.id == id
}

// SetFallbackTooltip names what the pointer is over when what it is over is not
// an element: a sprite the caller draws and hit-tests itself. text is the
// tooltip to show, and doubles as the target's identity, so replacing it
// restarts the dwell exactly as moving between elements does. Pass "" the moment
// the pointer leaves — and while a window covers what it was over, since a
// fallback never went through hit testing and so nothing else can take it away.
//
// at is where the tooltip goes, in the coordinates layout runs in. It is handed
// straight back by FallbackTooltip, ungated by the dwell, so a tooltip already up
// follows the pointer. It is also how the tracker tells resting from crossing: a
// fallback's dwell advances only while at holds still from one frame to the next.
//
// A fallback outranks any element, and Hovered reports false for every id while
// one is set. What lies under a hand-drawn sprite is usually a screen-filling
// element catching clicks for the scene behind it, which layout cannot tell
// apart from a button, so deferring to it would mean never showing a fallback at
// all. For the same reason, crossing between an element and a fallback restarts
// the dwell rather than counting as moving between two tooltips.
//
// Call it once per frame from your own hit test. Before or after Update is
// equally fine: the tracker trails the pointer by a frame either way.
func (tracker *HoverTracker) SetFallbackTooltip(text string, at m.Vec2) {
	tracker.fallback = text
	tracker.fallbackAt = at
}

// FallbackTooltip returns the text last given to SetFallbackTooltip once the
// pointer has rested on it for Dwell, along with where to put it. The text is ""
// while no fallback is set or the dwell has not run out.
func (tracker *HoverTracker) FallbackTooltip() (string, m.Vec2) {
	if !tracker.shown || tracker.current.fallback == "" {
		return "", m.Vec2{}
	}
	return tracker.current.fallback, tracker.fallbackAt
}
