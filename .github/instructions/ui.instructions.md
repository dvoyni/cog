---
name: "UI Construction"
description: "Use when creating or changing Go code that constructs UI elements, layouts, visuals, or interactions. Covers abstraction choice, composition, and sizing ownership."
applyTo: "**/*.go"
---

# UI Construction

Use the highest-level `ui` abstraction that fully expresses the behavior:

1. Use an existing element, visual, or modifier.
2. Compose `Horizontal`, `Vertical`, `Grid`, or `Overlay` with built-in elements such as `Image`, `Label`, and `ColorPanel`.
3. Extract repeated composition into a small element factory.
4. Implement `ui.Visual` only when built-in elements cannot express the visual, such as procedural geometry or specialized batched rendering.
5. Record direct canvas operations only inside a justified custom `Visual`.

Establish why one level cannot express the behavior before moving to the next. Existing low-level code is not, by itself, a reason to use a lower level. Follow these rules in new and changed code without expanding a focused task into unrelated cleanup.

## Compose Declaratively

- Represent spatial relationships with layout containers. Use `Grid` for two-dimensional placement, `Horizontal` or `Vertical` for one-dimensional flow, and `Overlay` for shared bounds.
- Use built-in visual parameters for image fitting, rotation, tint, frames, and interactive states.
- Use `ui.Element{}` for an empty declaration or grid placeholder.
- Express presentation state with interactive visuals and `VisualStates` rather than branching into separate draw paths.

Prefer ordinary elements when they are sufficient:

```go
func statusIcon(path string, rotation float32) ui.Element {
	return ui.Image(ui.SpriteParams{
		Path: path, Fit: ui.SpriteContain, Rotation: rotation,
	})
}

func statusGrid(portrait, top, left, right ui.Element) ui.Element {
	return ui.Grid(
		ui.Element{}, top, ui.Element{},
		left, portrait, right,
		ui.Element{}, ui.Element{}, ui.Element{},
	).Columns(3).Rows(3)
}
```

A custom `Visual` that manually calculates the same cells and records image operations is the wrong abstraction. Reserve that machinery for output that element composition cannot produce.

## Parent-Owned Sizing

Reusable and specialized element factories should normally leave width and height unset. Their parent owns contextual sizing through its layout, available bounds, alignment, stretch, and constraints.

Set dimensions inside a factory only when they are part of the element's actual contract, such as a fixed interaction target, authored frame geometry, or explicit design-system size. Repeated use at one size does not make that size contractual.

Keep the reusable element flexible:

```go
func abilityIcon(path string) ui.Element {
	return ui.Image(ui.SpriteParams{Path: path, Fit: ui.SpriteContain})
}

func toolbar(path string) ui.Element {
	return ui.Horizontal(
		abilityIcon(path).Width(24).Height(24),
		ui.Spacer(),
	)
}
```

Avoid embedding `.Width(24).Height(24)` in `abilityIcon`; another parent may need the same element at a different size. Images already have intrinsic size and preserve their aspect ratio when layout fixes one axis.

## Validation

After changing UI code, compile the narrowest package or production target that includes the change.