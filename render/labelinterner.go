package render

import (
	"strings"
	"sync"
)

// labelInterner keeps a copy of each label render keeps past the call that
// names it, a pass's or a mesh's, so the descriptor handed with the label,
// its slices among it, stays where its caller made it: Go's escape analysis
// would move a whole descriptor to the heap for its label kept. A label is
// copied the first time it is named, and its copy forgotten at the end of
// the frame that leaves it unnamed for Config's TemporaryIdleFrames frames,
// so the same labels named frame after frame allocate nothing once warm.
type labelInterner struct {
	mutex  sync.Mutex
	labels map[string]*internedLabel
}

// internedLabel is a label's copy, and whether it was named this frame and
// for how many frames it has gone unnamed.
type internedLabel struct {
	label string
	hold  poolHold
}

// intern answers label's copy, made the first time label is named; the
// empty label is its own.
func (i *labelInterner) intern(label string) string {
	if label == "" {
		return ""
	}
	i.mutex.Lock()
	defer i.mutex.Unlock()
	interned, ok := i.labels[label]
	if !ok {
		if i.labels == nil {
			i.labels = map[string]*internedLabel{}
		}
		interned = &internedLabel{label: strings.Clone(label)}
		i.labels[interned.label] = interned
	}
	interned.hold.held = true
	return interned.label
}

// endFrame forgets the copies of the labels that have now gone unnamed for
// idleFrames frames.
func (i *labelInterner) endFrame(idleFrames int) {
	i.mutex.Lock()
	defer i.mutex.Unlock()
	for label, interned := range i.labels {
		if interned.hold.endFrame(idleFrames) {
			delete(i.labels, label)
		}
	}
}
