package canvas

import (
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
)

// drawnTargets is the Targets canvas installs: each target a camera has
// drawn into, as it was last drawn, and the main window of the frame last
// drawn. Only canvas writes it, under Targets' write lock,
// and a reader cannot assert it into writing, its type being private.
type drawnTargets struct {
	mainWindow platform.Window
	targets    []drawnTarget
}

// Window answers window's size in points as it was last drawn, its pixels
// to a point, and whether a camera has drawn into it, the zero window
// answering the main window's.
func (t *drawnTargets) Window(window platform.Window) (m.Vec2, float32, bool) {
	if window == (platform.Window{}) {
		window = t.mainWindow
	}
	if window == (platform.Window{}) {
		return m.Vec2{}, 0, false
	}
	target := t.find(window, render.Texture{})
	if target == nil {
		return m.Vec2{}, 0, false
	}
	return m.Vec2{X: float32(target.width), Y: float32(target.height)}.DivS(target.pixelsPerPoint), target.pixelsPerPoint, true
}

// Texture answers texture's size in pixels as it was last drawn, and
// whether a camera has drawn into it.
func (t *drawnTargets) Texture(texture render.Texture) (m.Vec2, bool) {
	if texture == (render.Texture{}) {
		return m.Vec2{}, false
	}
	target := t.find(platform.Window{}, texture)
	if target == nil {
		return m.Vec2{}, false
	}
	return m.Vec2{X: float32(target.width), Y: float32(target.height)}, true
}

// record keeps the target of each of cameraDraws, a frame's cameras that
// drew, as it drew, in place of its account before, and mainWindow, the
// main window they drew against.
func (t *drawnTargets) record(mainWindow platform.Window, cameraDraws []cameraDraw) {
	t.mainWindow = mainWindow
	for index := range cameraDraws {
		drawn := &cameraDraws[index].target
		if target := t.find(drawn.window, drawn.texture); target != nil {
			*target = *drawn
		} else {
			t.targets = append(t.targets, *drawn)
		}
	}
}

// find answers the account of the target of window and texture, one of them
// zero, or nil when it has none.
func (t *drawnTargets) find(window platform.Window, texture render.Texture) *drawnTarget {
	for index := range t.targets {
		if target := &t.targets[index]; target.window == window && target.texture == texture {
			return target
		}
	}
	return nil
}

// drawnTarget is one target as a camera drew into it: its window or its
// texture, the other zero; its size in pixels; and its pixels to a point, 1
// for a texture.
type drawnTarget struct {
	window         platform.Window
	texture        render.Texture
	width, height  int
	pixelsPerPoint float32
}
