package platform

import (
	"fmt"
	"slices"

	"github.com/dvoyni/cog/kernel"
)

// eventProcessor is the private resource of ProcessEvents' own state, write
// locked by its runs alone, which so never overlap.
type eventProcessor struct {
	windows     kernel.ExclusiveSocket[WindowsSocket]
	inputSource kernel.ExclusiveSocket[InputSourceSocket]
	// applied is every window as last applied through the WindowsSocket, in
	// the order they were opened.
	applied []appliedWindow
	// changes is the batch of one run, its capacity kept between runs.
	changes []InputChange
}

// appliedWindow is one window as last applied through the WindowsSocket.
type appliedWindow struct {
	window Window
	entry  WindowEntry
}

// processEvents is ProcessEvents, the pump task: it applies the request
// through the bound WindowsSocket, lets it pump, collects the input changes,
// folds them into the window state, then publishes them in order. Without a
// WindowsSocket it applies nothing, and without an InputSourceSocket it
// collects no input.
func processEvents(k *kernel.Kernel, _ kernel.NoArg, processorHandle kernel.Write[*eventProcessor], managerHandle kernel.Write[*WindowManager]) kernel.NoResult {
	processor := processorHandle.Get()
	manager := managerHandle.Get()
	processor.changes = processor.changes[:0]
	sourced := 0
	if windows := processor.windows.Get(); windows != nil {
		processor.changes = processor.apply(k, windows, manager, processor.changes)
		sourced = len(processor.changes)
		processor.changes = windows.Pump(processor.changes)
	}
	if inputSource := processor.inputSource.Get(); inputSource != nil {
		processor.changes = inputSource.AppendChanges(processor.changes)
	}
	for _, change := range processor.changes[sourced:] {
		if manager.findState(change.Window) < 0 {
			panic(fmt.Sprintf("platform: an input change names %v, which the window implementation was not asked to open or reported closed", change.Window))
		}
	}
	for _, change := range processor.changes[sourced:] {
		processor.fold(manager, change)
	}
	for _, change := range processor.changes {
		switch change.Kind {
		case InputChangeWindowOpened:
			k.Publish(WindowOpenedEvent{Window: change.Window})
		case InputChangeWindowClosed:
			k.Publish(WindowClosedEvent{Window: change.Window})
		case InputChangeCloseRequested:
			k.Publish(CloseRequestedEvent{Window: change.Window})
		}
	}
	return kernel.NoResult{}
}

// apply applies what changed in manager's request since the last run
// through windows: it closes every window whose entry is gone, then opens
// every new one and changes every edited one, in the order they were opened.
// It appends a closed change for each window it closed to changes, and
// answers it.
func (processor *eventProcessor) apply(k *kernel.Kernel, windows WindowsSocket, manager *WindowManager, changes []InputChange) []InputChange {
	kept := processor.applied[:0]
	for _, applied := range processor.applied {
		if manager.findEntry(applied.window) >= 0 {
			kept = append(kept, applied)
			continue
		}
		if err := windows.Close(applied.window); err != nil {
			k.Logger().LogErr(fmt.Errorf("platform: closing a window: %w", err))
		}
		manager.removeState(applied.window)
		changes = append(changes, InputChange{Kind: InputChangeWindowClosed, Window: applied.window})
	}
	clear(processor.applied[len(kept):])
	processor.applied = kept
	for _, requested := range manager.entries {
		index := processor.find(requested.window)
		if index < 0 {
			processor.applied = append(processor.applied, appliedWindow{window: requested.window, entry: requested.entry})
			manager.addState(requested.window)
			if err := windows.Open(requested.window, requested.entry); err != nil {
				k.Logger().LogErr(fmt.Errorf("platform: opening a window: %w", err))
			}
			continue
		}
		applied := &processor.applied[index]
		if applied.entry != requested.entry {
			if err := windows.Change(requested.window, applied.entry, requested.entry); err != nil {
				k.Logger().LogErr(fmt.Errorf("platform: changing a window: %w", err))
			}
			applied.entry = requested.entry
		}
	}
	return changes
}

// fold applies one change from the WindowsSocket or the InputSourceSocket to
// manager.
func (processor *eventProcessor) fold(manager *WindowManager, change InputChange) {
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
		if index := processor.find(change.Window); index >= 0 {
			processor.applied = slices.Delete(processor.applied, index, index+1)
		}
	}
}

// find answers the index of window in applied, or -1 if it is not there.
func (processor *eventProcessor) find(window Window) int {
	return slices.IndexFunc(processor.applied, func(a appliedWindow) bool { return a.window == window })
}
