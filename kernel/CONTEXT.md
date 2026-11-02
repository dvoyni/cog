# Kernel

The kernel is Cog's plugin microkernel: it runs plugins' work as jobs over shared resources, ordering jobs by their dependencies and keeping their locks from conflicting. This glossary records its domain language.

## Composition

**Supervisor**:
The one owner of every kernel in an application, and of the main thread and the log handler they share.

**Kernel**:
One isolated runtime under a supervisor: its resources, tasks, handlers and jobs. Kernels share nothing and reach each other only by publishing events.
_Avoid_: Engine, world, instance

**Plugin**:
A statically linked unit of functionality installed into a kernel at spawn, identified within it by its name.
_Avoid_: Module, extension, system

**Config part**:
The value under a plugin's name in the config a kernel is spawned with: all of the config that plugin sees.

**Composition error**:
A mistake in how plugins register (an uninitialized resource, a pointer in an arg, a cycle in handler order), caught at spawn rather than at run time.

## Work

**Resource**:
A value of one type that a kernel holds once, and that only tasks touch.
_Avoid_: Component, service, port

**Task**:
Registered code that touches resources. Its lock set is fixed when it is registered.
_Avoid_: System, command, handler

**Lock set**:
The resources a task reads and the resources it writes. Requesting a resource handle is what adds to it.

**Job**:
One scheduled run of a task, with one arg and one result.
_Avoid_: Command, invocation

**Dependency**:
A job another job waits for before it may start.

**Feeding**:
A job taking another job's result as its arg.

**Job graph**:
A kernel's jobs that have not finished, with the dependencies and feedings between them.

**Pinned task**:
A task whose jobs run on the main thread rather than on the kernel's workers.

## Events

**Event**:
A pointer-free value published into a kernel, and the only way work gets scheduled.
_Avoid_: Message, command

**Publication**:
One delivery of an event to its handlers, in their settled order.

**Handler**:
Code subscribed to an event type. It holds no locks; it decides what jobs to schedule and may change the event for the handlers after it.
_Avoid_: Subscriber, listener

**Batch**:
The jobs one publication's handlers schedule. None of them starts until the last handler returns.

**Out dependency**:
The job a handler hands to the next handler, and a publication to its publisher, for later work to wait on.

## Outside the kernel

**Main thread**:
The OS thread `main` runs on, lent to pinned jobs between waits.

**Signal**:
A reusable notice to code outside the kernel that a job has finished.
