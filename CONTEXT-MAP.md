# Context Map

## Contexts

- [Kernel](./kernel/CONTEXT.md): runs plugins' work as jobs over shared resources, ordering jobs and keeping their locks apart
- [ECS](./ecs/CONTEXT.md): worlds of entities with pointer-free components, worked on by systems that run as kernel tasks
- [Metronome](./metronome/CONTEXT.md): paces the tracks an application declares, publishing each beat as a kernel event
- [Gfx](./gfx/CONTEXT.md): the GPU as a kernel sees it, a thin mirror of wgpu that draws one frame per beat of the render track
- [Platform](./platform/CONTEXT.md): what the operating system provides, windows, input and filesystems, declared as interfaces an implementing plugin fills

## Relationships

- **Kernel → ECS**: a world, its stores and its command buffers are kernel resources; a system is a kernel task, and its parameters declare its lock set through the kernel's `Access`.
- **Kernel → Metronome**: a beat is a kernel event, and a beat stays in flight until its publication's out dependency finishes.
- **Kernel → Platform**: windows, the FileSystem and values are kernel resources an implementing plugin installs; the pump is a task pinned to the main thread.
- **Metronome → Gfx**: gfx's draw chain hangs off a beat of the render track, and the beat stays in flight until the frame is presented.
- **Platform → Gfx**: gfx keeps one surface per platform window, created from its native handle when the window opens.
