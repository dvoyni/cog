package gogpu

import (
	"github.com/dvoyni/cog/platform"
	"github.com/gogpu/gpucontext"
)

// inputSource is the plugin's platform.InputSourceSocket: it reports the
// input the window system's windows report to it. platform calls it on the
// main thread alone.
type inputSource struct {
	// plugin holds what the next AppendChanges reports.
	plugin *Plugin
}

// AppendChanges appends to changes the input the windows reported since its
// last call, in the order it happened, and answers it.
func (s inputSource) AppendChanges(changes []platform.InputChange) []platform.InputChange {
	changes = append(changes, s.plugin.inputChanges...)
	s.plugin.inputChanges = s.plugin.inputChanges[:0]
	return changes
}

// Label answers 0: key labels are not built yet.
func (inputSource) Label(platform.Key) rune { return 0 }

// reportPointer reports event in window from the next AppendChanges. A
// cancel, and a button no Key names, report nothing.
func (s inputSource) reportPointer(window platform.Window, event gpucontext.PointerEvent) {
	change := platform.InputChange{Window: window, Position: platform.Point{X: float32(event.X), Y: float32(event.Y)}}
	switch event.Type {
	case gpucontext.PointerMove:
		change.Kind = platform.InputChangePointerMove
		change.PointerDelta = platform.Point{X: float32(event.DeltaX), Y: float32(event.DeltaY)}
	case gpucontext.PointerEnter:
		change.Kind = platform.InputChangePointerEnter
	case gpucontext.PointerLeave:
		change.Kind = platform.InputChangePointerLeave
	case gpucontext.PointerDown, gpucontext.PointerUp:
		key, ok := mouseButtonKeys[event.Button]
		if !ok {
			return
		}
		change.Kind = platform.InputChangeKey
		change.Key = key
		change.Pressed = event.Type == gpucontext.PointerDown
		change.Modifiers = platform.Modifiers(event.Modifiers)
	default:
		return
	}
	s.plugin.inputChanges = append(s.plugin.inputChanges, change)
}

// mouseButtonKeys is the Key of each gogpu button.
var mouseButtonKeys = map[gpucontext.Button]platform.Key{
	gpucontext.ButtonLeft:   platform.KeyMouseLeft,
	gpucontext.ButtonMiddle: platform.KeyMouseMiddle,
	gpucontext.ButtonRight:  platform.KeyMouseRight,
	gpucontext.ButtonX1:     platform.KeyMouse4,
	gpucontext.ButtonX2:     platform.KeyMouse5,
}
