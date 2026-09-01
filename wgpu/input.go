package wgpu

import (
	"github.com/dvoyni/cog/input"
	"github.com/dvoyni/cog/kernel"
	"github.com/gogpu/gpucontext"
)

// wireInput connects gogpu's EventSource to the input contract. Each callback
// appends an input.Change to a main-thread-local batch that flushInput hands to
// the input plugin once per frame. The callbacks and flushInput both run on the
// gogpu main thread, so the batch needs no lock.
func (p *Plugin) wireInput() {
	es := p.gpu.EventSource()

	es.OnKeyPress(func(k gpucontext.Key, m gpucontext.Modifiers) {
		p.pending = append(p.pending, input.KeyChange(mapKey(k), mapMods(m), true))
	})
	es.OnKeyRelease(func(k gpucontext.Key, m gpucontext.Modifiers) {
		p.pending = append(p.pending, input.KeyChange(mapKey(k), mapMods(m), false))
	})
	es.OnTextInput(func(text string) {
		for _, r := range text {
			p.pending = append(p.pending, input.TextChange(r))
		}
	})
	if pointerEvents, ok := es.(gpucontext.PointerEventSource); ok {
		pointerEvents.OnPointer(func(event gpucontext.PointerEvent) {
			p.pending = append(p.pending, pointerChanges(event)...)
		})
	} else {
		es.OnMouseMove(func(x, y float64) {
			p.pending = append(p.pending, input.PointerChange(input.Pos{X: x, Y: y}))
		})
		es.OnMousePress(func(button gpucontext.MouseButton, x, y float64) {
			p.pending = append(p.pending,
				input.PointerChange(input.Pos{X: x, Y: y}),
				input.KeyChange(mapMouseButton(button), 0, true))
		})
		es.OnMouseRelease(func(button gpucontext.MouseButton, x, y float64) {
			p.pending = append(p.pending,
				input.PointerChange(input.Pos{X: x, Y: y}),
				input.KeyChange(mapMouseButton(button), 0, false))
		})
	}
	es.OnScroll(func(dx, dy float64) {
		p.pending = append(p.pending, input.ScrollChange(dx, dy))
	})
}

func pointerChanges(event gpucontext.PointerEvent) []input.Change {
	// gogpu's browser translator currently leaves both primary fields at zero.
	if !event.IsPrimary && event.PointerID != 0 {
		return nil
	}

	position := input.PointerChange(input.Pos{X: event.X, Y: event.Y})
	switch event.Type {
	case gpucontext.PointerMove:
		return []input.Change{position}
	case gpucontext.PointerDown, gpucontext.PointerUp, gpucontext.PointerCancel:
		key, ok := mapPointerButton(event.PointerType, event.Button)
		if !ok {
			return []input.Change{position}
		}
		down := event.Type == gpucontext.PointerDown
		return []input.Change{position, input.KeyChange(key, mapMods(event.Modifiers), down)}
	default:
		return nil
	}
}

func mapPointerButton(pointerType gpucontext.PointerType, button gpucontext.Button) (input.Key, bool) {
	if pointerType != gpucontext.PointerTypeMouse {
		return input.KeyMouseLeft, true
	}

	switch button {
	case gpucontext.ButtonLeft:
		return input.KeyMouseLeft, true
	case gpucontext.ButtonRight:
		return input.KeyMouseRight, true
	case gpucontext.ButtonMiddle:
		return input.KeyMouseMiddle, true
	case gpucontext.ButtonX1:
		return input.KeyMouse4, true
	case gpucontext.ButtonX2:
		return input.KeyMouse5, true
	default:
		return 0, false
	}
}

// flushInput hands the frame's accumulated input changes to the input plugin via
// its Apply command. Called at the start of onUpdate (main thread). If the input
// plugin is not registered, the kernel's error handler is called and the changes
// are dropped.
func (p *Plugin) flushInput(k kernel.Executioner) {
	if len(p.pending) == 0 {
		return
	}
	changes := p.pending
	p.pending = nil
	k.ExecuteCommand[input.ApplyCmd](input.ApplyRequest{Changes: changes})
}

// mapKey maps a gogpu/gpucontext key code onto the input contract's Key space.
// input.Key mirrors gpucontext.Key for keyboard keys, so this is a plain cast.
func mapKey(k gpucontext.Key) input.Key {
	return input.Key(k)
}

// mapMouseButton maps a gogpu mouse button onto the negative input.Key range:
// button b -> Key(-1 - b) (Left=-1, Right=-2, Middle=-3, ...).
func mapMouseButton(b gpucontext.MouseButton) input.Key {
	return -1 - input.Key(b)
}

// mapMods maps gogpu modifier flags onto input.Mods. The bit positions match, so
// this is a plain cast.
func mapMods(m gpucontext.Modifiers) input.Mods {
	return input.Mods(m)
}
