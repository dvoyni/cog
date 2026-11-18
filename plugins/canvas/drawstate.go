package canvas

import (
	"fmt"
	"sync/atomic"

	"github.com/dvoyni/cog/kernel"
)

// drawState is canvas's private state: the lane of each task declaring a
// DrawList, in registration order, whether a frame is open for entries, and
// the entries of the frame last merged from the lanes.
type drawState struct {
	// logger is the kernel's, where reports go.
	logger *kernel.Logger
	// recording is set from the job opening a frame for entries until the
	// job merging its lanes.
	recording atomic.Bool
	// lanes are added at registration, while the kernel is composed, and
	// only read after.
	lanes []*lane
	// shapes are the frame's shapes, merged from the lanes in their order,
	// and replaced by the next frame's when it merges.
	shapes []Shape
}

// addLane adds a lane after every lane added before it, and answers it.
func (s *drawState) addLane() *lane {
	added := &lane{outsideFrame: Report{
		Kind: ReportKindEntryOutsideFrame,
		Err:  fmt.Errorf("canvas: draw list %d recorded an entry while no frame is recording", len(s.lanes)),
	}}
	s.lanes = append(s.lanes, added)
	return added
}

// closeAndMerge closes the frame to entries, and replaces the shapes of the
// frame merged before with each lane's, lane after lane, emptying the lanes.
func (s *drawState) closeAndMerge() {
	s.recording.Store(false)
	s.shapes = s.shapes[:0]
	for _, taskLane := range s.lanes {
		s.shapes = taskLane.moveShapes(s.shapes)
	}
}
