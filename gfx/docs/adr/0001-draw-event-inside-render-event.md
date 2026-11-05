# The draw event is published inside gfx's render-event handler

gfx must acquire before the application records a frame and flush and present after it, but kernel handlers keep plugin order unless declared otherwise, so gfx's handlers on `RenderEvent` would sit side by side and every application handler would have to order itself before gfx's present, silently breaking the frame when one forgets. gfx therefore has a single `RenderEvent` handler: it schedules the acquire, publishes `DrawEvent` from inside that handler with `ex.Publish`, and schedules flush and present after the out dependency the nested publication answers. The application subscribes to `DrawEvent` with no ordering at all, and `RenderEvent` is the beat alone.

## Considered Options

- **Handlers on `RenderEvent`, ordered by declaration:** the decided shape before the kernel's `OrderSubscriptions` existed (`RenderEvent.Ready` set by gfx's first handler). Rejected because the bracket depended on every plugin remembering one call.
