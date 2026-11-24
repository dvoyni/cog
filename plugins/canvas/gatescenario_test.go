package canvas_test

import "fmt"

// gateScenario is a steady frame of spriteCount sprites, 8 units square,
// split among taskCount recording tasks, each drawing its share in bulk,
// drawn through cameraCount cameras the first task sets. Its sprites
// switch to a second material and back every switchPeriod, zero meaning
// never, and spread over layerCount layers, zero meaning one. Unless
// spritesOnly, each task also draws the same few entries of every other
// kind, half its sprites lie far right of every camera's view, culled, and
// a sprite and a fill are given an argument of the dissolve material.
type gateScenario struct {
	spriteCount  int
	taskCount    int
	cameraCount  int
	switchPeriod int
	layerCount   int
	spritesOnly  bool
}

func (s gateScenario) String() string {
	return fmt.Sprintf("sprites=%d/tasks=%d/cameras=%d/switch=%d/layers=%d", s.spriteCount, s.taskCount, s.cameraCount, s.switchPeriod, s.layerCount)
}
