package platform

import (
	"fmt"
	"slices"

	"github.com/dvoyni/cog/kernel"
)

// pump is ProcessEvents' own state, touched only by its runs, which write
// lock the same resources and so never overlap.
type pump struct {
	windows     Windows
	inputSource InputSource
	// applied is every window as last applied through Windows, in the order
	// they were opened.
	applied []appliedWindow
	// changes is the batch of one run, its capacity kept between runs.
	changes []InputChange
}

// appliedWindow is one window as last applied through Windows.
type appliedWindow struct {
	window Window
	entry  WindowEntry
}

// run is one ProcessEvents run: it applies the request through Windows, lets
// Windows pump, collects the input changes, folds them into the window state,
// then publishes them in order.
func (p *pump) run(k *kernel.Kernel, manager *WindowManager) {
	p.changes = p.apply(k, manager, p.changes[:0])
	sourced := len(p.changes)
	p.changes = p.windows.Pump(p.changes)
	p.changes = p.inputSource.AppendChanges(p.changes)
	for _, change := range p.changes[sourced:] {
		if manager.findState(change.Window) < 0 {
			panic(fmt.Sprintf("platform: an input change names %v, which Windows was not asked to open or reported closed", change.Window))
		}
	}
	for _, change := range p.changes[sourced:] {
		p.fold(manager, change)
	}
	for _, change := range p.changes {
		switch change.Kind {
		case InputChangeWindowOpened:
			k.Publish(WindowOpenedEvent{Window: change.Window})
		case InputChangeWindowClosed:
			k.Publish(WindowClosedEvent{Window: change.Window})
		case InputChangeCloseRequested:
			k.Publish(CloseRequestedEvent{Window: change.Window})
		}
	}
}

// apply applies what changed in manager's request since the last run through Windows:
// it closes every window whose entry is gone, then opens every new one and
// changes every edited one, in the order they were opened. It appends a
// closed change for each window it closed to changes, and answers it.
func (p *pump) apply(k *kernel.Kernel, manager *WindowManager, changes []InputChange) []InputChange {
	kept := p.applied[:0]
	for _, applied := range p.applied {
		if manager.findEntry(applied.window) >= 0 {
			kept = append(kept, applied)
			continue
		}
		if err := p.windows.Close(applied.window); err != nil {
			k.Logger().LogErr(fmt.Errorf("platform: closing a window: %w", err))
		}
		manager.removeState(applied.window)
		changes = append(changes, InputChange{Kind: InputChangeWindowClosed, Window: applied.window})
	}
	clear(p.applied[len(kept):])
	p.applied = kept
	for _, requested := range manager.entries {
		index := p.find(requested.window)
		if index < 0 {
			p.applied = append(p.applied, appliedWindow{window: requested.window, entry: requested.entry})
			manager.addState(requested.window)
			if err := p.windows.Open(requested.window, requested.entry); err != nil {
				k.Logger().LogErr(fmt.Errorf("platform: opening a window: %w", err))
			}
			continue
		}
		applied := &p.applied[index]
		if applied.entry != requested.entry {
			if err := p.windows.Change(requested.window, applied.entry, requested.entry); err != nil {
				k.Logger().LogErr(fmt.Errorf("platform: changing a window: %w", err))
			}
			applied.entry = requested.entry
		}
	}
	return changes
}

// fold applies one change from Windows or the InputSource to manager.
func (p *pump) fold(manager *WindowManager, change InputChange) {
	switch change.Kind {
	case InputChangeWindowOpened:
		index := manager.findState(change.Window)
		if index < 0 {
			return
		}
		opened := &manager.states[index]
		opened.open = true
		opened.size = change.Size
		opened.framebufferSize = change.FramebufferSize
		opened.scale = change.Scale
		opened.native = change.Native
	case InputChangeWindowClosed:
		manager.removeState(change.Window)
		manager.Close(change.Window)
		if index := p.find(change.Window); index >= 0 {
			p.applied = slices.Delete(p.applied, index, index+1)
		}
	}
}

// find answers the index of window in applied, or -1 if it is not there.
func (p *pump) find(window Window) int {
	return slices.IndexFunc(p.applied, func(a appliedWindow) bool { return a.window == window })
}
