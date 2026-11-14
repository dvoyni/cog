package render

import (
	"errors"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/kernel"
)

// reporter is what a resource of render's that reports holds: the kernel's
// logger, where reports and errors go.
type reporter struct {
	logger *kernel.Logger
}

// report logs a Report of kind, saying err.
func (r reporter) report(kind ReportKind, err error) {
	r.logger.LogErr(Report{Kind: kind, Err: err})
}

// logUnlessLost logs err, unless the device is lost, which gfx has logged.
func (r reporter) logUnlessLost(err error) {
	if !errors.Is(err, gfx.ErrDeviceLost) {
		r.logger.LogErr(err)
	}
}
