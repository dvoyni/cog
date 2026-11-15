package render

import (
	"cmp"
	"fmt"
	"slices"
)

// clearChecker finds, after a frame's recording, each pass that clears a
// target an earlier pass of the frame drew into, in submit order, and
// reports it once for the two passes: a window or a texture, as a colour,
// resolve or depth-stencil target, a read-only one never drawn into.
type clearChecker struct {
	// sortedStates are the frame's list states in submit order, and
	// lastDrawers, for each target drawn into since it was last cleared, the
	// last pass that drew into it, both kept between frames so checking
	// allocates nothing once warm; reportedClears the pairs of passes
	// reported.
	sortedStates   []*commandListState
	lastDrawers    map[passTargetKey]passIdentity
	reportedClears map[clearReportKey]struct{}
}

// check reports, with reporter, each pass of states, the command lists of a
// frame, that clears a target a pass before it in gfx's submit
// order, by their lists' orders, drew into, unless its pair was reported
// before.
func (c *clearChecker) check(states []*commandListState, reporter reporter) {
	c.sortedStates = append(c.sortedStates[:0], states...)
	slices.SortFunc(c.sortedStates, func(first, second *commandListState) int { return cmp.Compare(first.order, second.order) })
	if c.lastDrawers == nil {
		c.lastDrawers = map[passTargetKey]passIdentity{}
	}
	clear(c.lastDrawers)
	for _, state := range c.sortedStates {
		for _, pass := range state.passes {
			identity := passIdentity{label: pass.label, order: state.order}
			for _, target := range state.passTargets[pass.targetStart:pass.targetEnd] {
				drawer, drawn := c.lastDrawers[target.key]
				if drawn && target.cleared {
					if claimReport(&c.reportedClears, clearReportKey{drawer: drawer, clearer: identity}) {
						reporter.report(ReportKindClearAfterDraw, fmt.Errorf("render: pass %q at order %d clears a target that pass %q at order %d drew into earlier in the frame", identity.label, identity.order, drawer.label, drawer.order))
					}
					delete(c.lastDrawers, target.key)
				}
				if pass.drew {
					c.lastDrawers[target.key] = identity
				}
			}
		}
	}
}

// passIdentity names a pass in a clear-after-draw report: its label, and the
// order of its command list.
type passIdentity struct {
	label string
	order Order
}

// clearReportKey is a pair of passes the clear-after-draw check reported:
// the one that drew, and the one that cleared after it.
type clearReportKey struct {
	drawer, clearer passIdentity
}
