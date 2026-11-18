# Context Map

## Contexts

- [Kernel](./kernel/CONTEXT.md): runs plugins' work as jobs over shared resources, ordering jobs and keeping their locks apart
- [ECS](./plugins/ecs/CONTEXT.md): worlds of entities with pointer-free components, worked on by systems that run as kernel tasks
- [Metronome](./plugins/metronome/CONTEXT.md): paces the tracks an application declares, publishing each beat as a kernel event
- [Gfx](./plugins/gfx/CONTEXT.md): the GPU as a kernel sees it, a thin mirror of wgpu that draws one frame per beat of the render track
- [Platform](./plugins/platform/CONTEXT.md): what the operating system provides, windows, input and filesystems, declared as interfaces an implementing plugin fills
- [Gogpu](./plugins/gogpu/CONTEXT.md): the plugin that implements gfx and platform over our gogpu fork, driven from the kernel's main-thread tick
- [Mux](./plugins/mux/CONTEXT.md): a game's true state as plain data, shown through timed overrides that catch what is drawn up to it
- [Render](./plugins/render/CONTEXT.md): the immediate-mode rendering API over gfx, drawing meshes with materials into passes, and loading and sharing the shaders, textures and buffers they use
- [Canvas](./plugins/canvas/CONTEXT.md): 2D on render, the sprites, shapes, text and meshes a frame records as entries, sorted and drawn through 2D materials

## Relationships

- **Kernel → ECS**: a world, its stores and its command buffers are kernel resources; a system is a kernel task, and its parameters declare its lock set through the kernel's `Access`.
- **Kernel → Metronome**: a beat is a kernel event, and a beat stays in flight until its publication's out dependency finishes.
- **Kernel → Platform**: windows, the FileSystem and values are kernel resources an implementing plugin installs; the pump is a task pinned to the main thread.
- **Metronome → Gfx**: gfx's draw chain hangs off a beat of the render track, and the beat stays in flight until the frame is presented.
- **Platform → Gfx**: gfx keeps one surface per platform window, created from its native handle when the window opens.
- **Gfx → Gogpu**: gogpu binds gfx's backend seam and pins the gfx tasks its OS needs on the main thread; each call passes through to gogpu's own surfaces and device.
- **Platform → Gogpu**: gogpu binds platform's window and input seams; platform's pump is gogpu's host loop pumping its events.
- **Gfx → Render**: render draws through gfx alone, and none of render's users reaches gfx; render's frame event is published inside gfx's draw event, and its recovery, capture and readback events inside gfx's own.
- **Platform → Render**: render reads every loaded asset through the FileSystem; bytes that are not files reach it through a memory mount.
- **Render → Canvas**: canvas draws through render alone and reaches no gfx; its frame event is published inside render's, its entries are sorted and drawn as one command list per camera order among render's, and what canvas needs that render lacks is a delta to render.
