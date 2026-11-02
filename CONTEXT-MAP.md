# Context Map

## Contexts

- [Kernel](./kernel/CONTEXT.md): runs plugins' work as jobs over shared resources, ordering jobs and keeping their locks apart
- [ECS](./ecs/CONTEXT.md): worlds of entities with pointer-free components, worked on by systems that run as kernel tasks

## Relationships

- **Kernel → ECS**: a world, its stores and its command buffers are kernel resources; a system is a kernel task, and its parameters declare its lock set through the kernel's `Access`.
