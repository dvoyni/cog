package canvas

import "sync"

// lane is one task's draw list for the frame being recorded: the entries its
// jobs record, in the order recorded. Two jobs of one task running together
// share it, their entries interleaving.
type lane struct {
	// outsideFrame is the lane's report of an entry recorded outside the
	// frame, made once; its message is the key it is logged once under.
	outsideFrame error
	// mutex guards shapes.
	mutex  sync.Mutex
	shapes []Shape
}

// recordShape records shape, unless state has no frame open for entries,
// which it reports once for the lane, outside the lane's lock.
func (l *lane) recordShape(state *drawState, shape Shape) {
	l.mutex.Lock()
	recording := state.recording.Load()
	if recording {
		l.shapes = append(l.shapes, shape)
	}
	l.mutex.Unlock()
	if !recording {
		state.logger.LogErrOnce(l.outsideFrame.Error(), l.outsideFrame)
	}
}

// moveShapes appends the lane's shapes to shapes, empties the lane, and
// answers the shapes appended to.
func (l *lane) moveShapes(shapes []Shape) []Shape {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	shapes = append(shapes, l.shapes...)
	l.shapes = l.shapes[:0]
	return shapes
}
