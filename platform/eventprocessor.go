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
	// changes is the batch of one run as the sockets report it, and events
	// what the run publishes, folded from it; their capacity is kept between
	// runs.
	changes []InputChange
	events  []InputChange
	// modifiers is what the last key change reported, whose lock states the
	// releases on focus loss carry.
	modifiers Modifiers
	// labelled is whether the input state's labels were filled, on the first
	// run with an input source.
	labelled bool
}

// appliedWindow is one window as last applied through the WindowsSocket.
type appliedWindow struct {
	window Window
	entry  WindowEntry
}

// processEvents is ProcessEvents, the pump task: it applies the request
// through the bound WindowsSocket, lets it pump, collects the input changes,
// folds them into the window and input state, then publishes them in order.
// Without a WindowsSocket it applies nothing, and without an
// InputSourceSocket it collects no input.
func processEvents(k *kernel.Kernel, _ kernel.NoArg, processorHandle kernel.Write[*eventProcessor], managerHandle kernel.Write[*WindowManager], inputHandle kernel.Write[*InputState]) kernel.NoResult {
	processor := processorHandle.Get()
	manager := managerHandle.Get()
	input := inputHandle.Get()
	processor.changes = processor.changes[:0]
	processor.events = processor.events[:0]
	if windows := processor.windows.Get(); windows != nil {
		processor.apply(k, windows, manager, input)
		processor.changes = windows.Pump(processor.changes)
	}
	if inputSource := processor.inputSource.Get(); inputSource != nil {
		processor.changes = inputSource.AppendChanges(processor.changes)
		if !processor.labelled {
			input.rebuildLabels(inputSource)
			processor.labelled = true
		}
	}
	for _, change := range processor.changes {
		if change.Kind != InputChangeLayout && manager.findState(change.Window) < 0 {
			panic(fmt.Sprintf("platform: an input change names %v, which the window implementation was not asked to open or reported closed", change.Window))
		}
		if change.Kind == InputChangeKey && !change.Key.isValid() {
			panic(fmt.Sprintf("platform: an input change names %v, which is no key", change.Key))
		}
	}
	input.clearEdges()
	for _, change := range processor.changes {
		processor.fold(manager, input, change)
	}
	for _, event := range processor.events {
		switch event.Kind {
		case InputChangeWindowOpened:
			k.Publish(WindowOpenedEvent{Window: event.Window})
		case InputChangeWindowClosed:
			k.Publish(WindowClosedEvent{Window: event.Window})
		case InputChangeCloseRequested:
			k.Publish(CloseRequestedEvent{Window: event.Window})
		case InputChangeKey:
			k.Publish(KeyEvent{Window: event.Window, Key: event.Key, Pressed: event.Pressed, Repeat: event.Repeat, Modifiers: event.Modifiers, Position: event.Position, Label: event.label})
		case InputChangeText:
			k.Publish(TextEvent{Window: event.Window, Char: event.Char})
		case InputChangeLayout:
			k.Publish(LayoutChangedEvent{})
		case InputChangeFocus:
			k.Publish(FocusEvent{Window: event.Window, Focused: event.Focused})
		case InputChangePointerMove:
			k.Publish(PointerMoveEvent{Window: event.Window, Position: event.Position, Delta: event.motion})
		case InputChangeScroll:
			k.Publish(ScrollEvent{Window: event.Window, Delta: event.ScrollDelta, Lines: event.ScrollLines})
		case InputChangePointerEnter:
			k.Publish(PointerEnterEvent{Window: event.Window})
		case InputChangePointerLeave:
			k.Publish(PointerLeaveEvent{Window: event.Window})
		case InputChangeResize:
			k.Publish(ResizeEvent{Window: event.Window, Size: event.Size, FramebufferSize: event.FramebufferSize, Scale: event.Scale})
		}
	}
	return kernel.NoResult{}
}

// apply applies what changed in manager's request since the last run
// through windows: it closes every window whose entry is gone, then opens
// every new one and changes every edited one, in the order they were opened.
// It keeps input's pointers to the windows the state knows, and adds a closed
// event for each window it closed to the run's events.
func (processor *eventProcessor) apply(k *kernel.Kernel, windows WindowsSocket, manager *WindowManager, input *InputState) {
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
		input.forgetPointer(applied.window)
		processor.events = append(processor.events, InputChange{Kind: InputChangeWindowClosed, Window: applied.window})
	}
	clear(processor.applied[len(kept):])
	processor.applied = kept
	for _, requested := range manager.entries {
		index := processor.find(requested.window)
		if index < 0 {
			processor.applied = append(processor.applied, appliedWindow{window: requested.window, entry: requested.entry})
			manager.addState(requested.window)
			input.addPointer(requested.window)
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
}

// fold applies one change from the WindowsSocket or the InputSourceSocket to
// manager and input, and adds the events it makes to the run's: the change
// itself, but for the release of a key not held, which it drops, a pointer
// move, which replaces the window's earlier move of the run, its motion
// added, and focus loss, which it follows with the release of every held key.
// A key change carries its label as of its place in the run, and a layout
// change rebuilds the labels.
func (processor *eventProcessor) fold(manager *WindowManager, input *InputState, change InputChange) {
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
		input.forgetPointer(change.Window)
		if index := processor.find(change.Window); index >= 0 {
			processor.applied = slices.Delete(processor.applied, index, index+1)
		}
	case InputChangeKey:
		processor.modifiers = change.Modifiers
		change.label = input.Label(change.Key)
		if change.Pressed {
			input.press(change.Key)
		} else if input.IsHeld(change.Key) {
			input.release(change.Key)
		} else {
			return
		}
	case InputChangeFocus:
		if index := manager.findState(change.Window); index >= 0 {
			manager.states[index].focused = change.Focused
		}
		if !change.Focused {
			processor.modifiers &= ModifierCapsLock | ModifierNumLock
			for key := range keyCount {
				if !input.IsHeld(key) {
					continue
				}
				input.release(key)
				release := InputChange{Kind: InputChangeKey, Window: change.Window, Key: key, Modifiers: processor.modifiers, label: input.Label(key)}
				if key.IsMouseButton() {
					release.Position = input.PointerPosition(change.Window)
				}
				processor.events = append(processor.events, release)
			}
		}
	case InputChangePointerMove:
		change.motion = input.movePointer(change.Window, change.Position)
		for index := len(processor.events) - 1; index >= 0; index-- {
			earlier := processor.events[index]
			if earlier.Kind == InputChangePointerMove && earlier.Window == change.Window {
				change.motion.X += earlier.motion.X
				change.motion.Y += earlier.motion.Y
				processor.events = slices.Delete(processor.events, index, index+1)
				break
			}
		}
	case InputChangeLayout:
		if inputSource := processor.inputSource.Get(); inputSource != nil {
			input.rebuildLabels(inputSource)
		}
	case InputChangePointerEnter:
		input.enterPointer(change.Window, change.Position)
	case InputChangePointerLeave:
		input.leavePointer(change.Window)
	case InputChangeResize:
		index := manager.findState(change.Window)
		if index < 0 {
			return
		}
		resized := &manager.states[index]
		resized.size = change.Size
		resized.framebufferSize = change.FramebufferSize
		resized.scale = change.Scale
	}
	processor.events = append(processor.events, change)
}

// find answers the index of window in applied, or -1 if it is not there.
func (processor *eventProcessor) find(window Window) int {
	return slices.IndexFunc(processor.applied, func(a appliedWindow) bool { return a.window == window })
}
