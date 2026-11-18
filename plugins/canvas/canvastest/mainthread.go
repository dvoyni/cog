package canvastest

import (
	"testing"

	"github.com/dvoyni/cog/plugins/render/rendertest"
)

// RunMain is rendertest.RunMain: a test package with pixel tests calls it
// from TestMain, so the harness keeps the process's main thread for gogpu.
//
//	func TestMain(m *testing.M) { canvastest.RunMain(m) }
func RunMain(m *testing.M) { rendertest.RunMain(m) }
