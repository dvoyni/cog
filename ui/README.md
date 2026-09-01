# ui

`github.com/cog-engine/ui` is an immediate-mode layout and interaction plugin.
Consumers submit a complete element tree every update tick. The plugin measures
and arranges that tree, hit-tests current input, records visuals into canvas,
publishes interaction results, and consumes the declaration.

## Package Model

An `Element` is a frame-local value describing layout, interaction, and visual
intent. Modifiers return a changed copy, so declarations compose fluently:

```go
root := ui.Horizontal(
    ui.Image(ui.SpriteParams{Path: "icon.png", Fit: ui.SpriteContain}),
    ui.Label(ui.TextParams{Font: font, Text: "Ready"}),
).ChildrenAlignment(ui.AlignCenter).Gap(8)
```

The zero value `ui.Element{}` is a valid empty declaration. It is useful for
conditional composition and as a grid placeholder. `ui.NewElement()` returns
the same empty declaration when a fluent chain is clearer.

The root passed to `Frame.Add` is copied. Descendant slices remain borrowed and
must stay unchanged until UI processing completes. Rebuild declarations on the
next tick instead of retaining and mutating a submitted tree.

## Plugin

- Name: `ui.Name` (`"ui"`)
- Constructor: `ui.New() *ui.Plugin`
- Dependencies: `input`, `gfx`, and `canvas`
- Configuration: none

Register dependencies before UI, typically in this order: `storage`, `input`,
`gfx`, `canvas`, then `ui`.

UI processing subscribes to `app.UpdateEvent` after
`input.UpdateEventHandler` and before `canvas.UpdateEventHandler`. It runs on
every update tick, including intermediate fixed-step catch-up ticks.

## Declaring A Frame

`*ui.Frame` is the writable one-tick submission resource. A producer binds a
write handle to it and runs before UI:

```go
type buildUIHandler kernel.Subscription[app.UpdateEvent]

func buildUI() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
    var frame kernel.Write[*ui.Frame]
    return func(access kernel.ResourceAccess) {
            frame = access.GetWrite[*ui.Frame]()
        }, func(k kernel.Kernel, _ app.UpdateEvent) error {
            frame.Get().Add(canvas.Layer(20), root)
            return nil
        }
}

registry.Subscribe[buildUIHandler](buildUI).
    Before[ui.UpdateEventHandler]()
```

Submit each root with its base canvas layer:

```go
frame.Get().Add(canvas.Layer(20), root)
```

Processing consumes all roots and clears the frame while retaining its internal
capacity. An empty frame is still processed so stale hover and pointer-capture
state can be released.

An unset child layer inherits its parent's effective layer. An explicit
`Element.Layer(offset)` is relative to the base layer passed to `Frame.Add`, not
to the parent's explicit offset. Elements are ordered first by effective layer,
then by declaration order on the same layer. Drawing and hit testing share this
order.

## Composition

The container helpers select how children are arranged:

- `Horizontal` and `Vertical` flow children on one main axis.
- `Grid` places children in two-dimensional tracks.
- `Overlay` gives each child the same content area.
- `Spacer` is an empty element with stretch weight `1`.

Flow containers support intrinsic sizing, weighted stretch and shrink, wrapping,
main-axis arrangement, cross-axis alignment, padding, and gaps.
`ChildrenArrangement` controls the main axis and `ChildrenAlignment` controls
the cross axis. `AlignStretch` fills an unset cross-axis dimension when the
parent's cross axis is definite.

`Padding` and `PaddingRel` accept CSS-style one, two, three, or four values in
top/right/bottom/left order. A visual draws in the outer `State.Rect`; padding
produces `State.ContentRect`, which contains normal child layout.

## Grid

`Columns(n)` produces row-major placement. `Rows(n)` by itself produces
column-major placement. Setting both creates a fixed row-major capacity; extra
children are inactive and are neither drawn nor hit-tested.

```go
status := ui.Grid(
    ui.Element{}, top, ui.Element{},
    left, center, right,
    ui.Element{}, bottom, ui.Element{},
).Columns(3).Rows(3).ChildrenAlignment(ui.AlignCenter)
```

Each natural column width is the largest child width in that column, and each
natural row height is the largest child height in that row. A definite grid
width or height distributes extra space evenly across tracks on that axis.
Alignment positions children inside the resulting cells; `AlignStretch` fills
unset dimensions. When neither rows nor columns are specified, layout chooses a
shape from the child sizes and available dimensions.

## Sizing

An element's natural size is the larger of its visual's intrinsic size and its
content size, plus padding. Leaving an axis unset preserves that natural or
parent-arranged size. `Width`, `Height`, minimums, and maximums use logical
pixels; their `Rel` variants use the corresponding containing axis.

`Stretch` and `Shrink` provide weighted main-axis distribution in flow layouts.
The parent can also determine an unset cross axis through `AlignStretch`. This
allows reusable elements to remain unconstrained while each parent chooses the
size appropriate to its context.

`PreserveAspectRatio` derives an unset axis when layout fixes the other axis.
`Image` and `InteractiveImage` enable it automatically. In a flow container, a
definite stretched cross axis can therefore determine an image's main axis from
its intrinsic ratio. Setting both axes makes both dimensions definite; use
`SpriteContain` or `SpriteCover` when the art should preserve its ratio inside
those bounds instead of using the default stretch fit.

`SpriteParams.Scale` affects intrinsic measurement. The arranged element rect
still determines the final drawing bounds.

`ui.Measure(element, available)` resolves structural, pixel, relative, and
constraint-based layout without drawing or processing interactions. It has no
canvas lookup, so intrinsic sprite and text sizes contribute zero; use it when
the relevant geometry comes from explicit dimensions and containers.

## Positioning And Clipping

`Left`, `Right`, `Top`, and `Bottom`, together with their relative variants,
position an element in its containing rectangle. Setting both opposing edges
makes that axis definite when its explicit dimension is unset. Pivot modifiers
offset placement using the element's own dimensions. `Fill()` sets all four
relative edges to zero.

`IgnoreLayout()` removes a child from flow or grid measurement and arrangement,
allowing edge modifiers to position it independently. It does not disable
clipping. `Overlay` is the usual container when several normal children should
occupy the same area.

Each element inherits its parent's child clip, which is the intersection of the
parent's rect and inherited clip. `IgnoreClip()` resets the element's inherited
clip to the screen; its descendants are clipped by its own rect again unless
they also opt out.

## Built-In Visuals

Built-in element wrappers cover the common visual vocabulary:

- `Image`, `Image9Sliced`, and `Image9SliceTiled`
- `Label`
- `ColorPanel`
- Interactive variants of sprite, text, color, and nine-slice visuals

The corresponding `Sprite`, `Text`, `Color`, and nine-slice factories return a
`(Visual, payload)` pair for use with `Element.Visual`.

Sprite intrinsic dimensions come from `canvas.LookupAccess`. `SpriteParams`
controls scale, tint, filtering, fit, source frame, rotation, and origin.
Single-image nine-slices use one framed texture; tiled nine-slices use separate
images for their corners, edges, and center.

`ui.Font` is a `{Path, Size}` descriptor. Text measurement and drawing parse
`${path}` inline-image tokens; a backslash escapes a literal `${` or `\`.
`TextParams.WordWrapping` wraps drawing at the arranged element width.
`WrapWidth` optionally provides the width used for intrinsic measurement.

## Custom Visuals

Implement `ui.ParamVisual[T]` when built-in elements cannot express the output,
such as procedural geometry or specialized batched rendering. Params keep their
type end to end; there is no payload to assert:

```go
type ParamVisual[T any] interface {
    DefaultSize(lookup canvas.LookupAccess, params T) m.Vec2
    Draw(lookup canvas.LookupAccess, queue *canvas.OpQueue, state ui.State, params T)
}
```

Implementations are stateless and shared between elements.
`Element.Visual(visual, params)` binds one to a single element's params and
stores the result as the untyped `ui.Visual` the layout pass measures and draws.

Both methods receive a handler-scoped `canvas.LookupAccess` for sprite sizing
and text measurement. `Draw` also receives final geometry, content geometry,
effective layer, clip, and visual state through `ui.State`.

The plugin configures the canvas layer transform and clip before each draw and
removes the clip afterward. A custom visual appends operations to the supplied
queue; it does not reset the queue or change clipping.

## Visual State

`State.VisualState` is a `uint16` mask inherited through the element tree. The
system states occupy bits `0..3`: Disabled, Active, Hovered, and Pressed.
User-defined presentation states start at `VisualUserDefinedBase` in bit 4.

Use `Element.State(add, remove)` to transform the inherited mask for an element
and its descendants. A disabled element still absorbs the pointer but is never a
target, and it does not hand the input to its ancestors; a descendant can remove
`VisualDisabled` to opt back in.

Interactive color, text, sprite, and nine-slice factories accept
`VisualStates[T]` maps. A map key containing several bits assigns the same value
to each bit. Drawing selects the configured value for the highest active bit.
User-defined bits therefore take precedence over system bits; system precedence
from highest to lowest is Pressed, Hovered, Active, then Disabled.

## Buttons And Interactions

`ui.Button` supplies interaction and container policy only. It returns an
`Overlay` with the requested ID and optional disabled state. Callers compose its
background, padding, and content.

Hit testing is topmost-wins. Every element absorbs the pointer over its visible
rect, so only the frontmost element under the pointer is considered and at most
one target reacts per event. That element is the target when it has an ID;
otherwise the input passes up to its nearest ancestor that has one, which is why
an anonymous background, label, or padding wrapper inside a button still
activates the button. If neither it nor any ancestor is an enabled target, the
input is swallowed instead of falling through to whatever is drawn below. A
full-screen anonymous element is therefore all a modal needs to stop the UI
underneath from responding.

This makes any element a click blocker for whatever it overlaps, including a
purely decorative one. Overlapping a caption across live controls will disable
them, so keep decoration out of interactive bounds or place it on a lower layer.

`IgnoreHitTest()` opts an element out of blocking: reaching it while walking up
from a hit without having found an ID lets the pointer fall through to whatever
is drawn beneath, instead of stopping the search there. A descendant with its
own ID is unaffected, since it is matched directly. Use this for a decorative
overlay that must render above interactive content without disabling it, such
as a highlight ring or tutorial callout drawn on a layer above the map.

`*ui.Interactions` contains results from the latest completed UI update. A
handler before UI reads the previous tick's results. A handler that needs the
current tick registers after UI and before canvas:

```go
registry.Subscribe[reactUIHandler](reactUI).
    Reads[*ui.Interactions]().
    After[ui.UpdateEventHandler]().
    Before[canvas.UpdateEventHandler]()
```

Use `Has` for lookup, `HasF` to decode an ID while matching, or range over
`All()`. At most one element reacts to a given event, so a kind appears at most
once per pointer event. Passing `true` for `consume` removes all interactions of
that kind after a match. The sequence returned by `All` is borrowed and must be
consumed before the next UI update.

Mouse buttons are numbered left `0`, right `1`, middle `2`, button 4 as `3`, and
button 5 as `4`. Hover interactions use button `-1`. Input edge positions use
the pointer's current tick position; when down and up are both present, down is
processed first. Press captures an ID for that button, release emits `Up`, and a
release over the same ID also emits `Click`.