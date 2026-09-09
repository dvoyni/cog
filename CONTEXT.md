# Cog Engine

Cog Engine provides typed runtime systems and the Feuds game. This glossary records the shared domain language used by its subsystems.

## Runtime Architecture

**Engine**:
The composition root. It exists once, owns the plugin set, registry, scheduler, and lifetime, and is built before startup.

**Kernel**:
The runtime handle a plugin uses during one dispatch. It is a value carrying its engine and invocation context, and it is scoped to the handler that received it.

**Executioner**:
A superset of the kernel that can also dispatch a command synchronously without declaring it. Only the engine mints one, for plugin lifecycle methods and host callbacks, which run outside any handler and so hold no locks.

**Command**:
A synchronous request-response contract handled by exactly one plugin. Its identity is a distinct defined type, declared together with the request and response types that carry its payload.
_Avoid_: Message, RPC, service call

**Handler**:
The factory a plugin registers for one command or subscription. It runs once, during registration, and returns the Lock that binds its resource handles plus the body the engine runs per invocation.

**Command handler**:
The Handler implementing one Command. It is always private: the command type is the name callers dispatch, and the handler behind it is the package's own business. Code abbreviates it to `Impl`, as in a `CmdImpl` suffix.

**Declared dispatch**:
A handler's statement, made in its `Lock`, that it dispatches a given command. Composition folds that command's lock closure into the handler's own set, so the handler never names the resources behind it.

**Plugin**:
A statically linked unit of engine functionality selected before startup and fixed for the engine lifetime.

**Plugin dependency**:
A requirement that another plugin complete registration and any optional startup first.

**Registrar**:
A plugin-scoped capability used only during Registration to declare owned contracts and initial resources.

**Registration**:
The lifecycle phase in which a plugin declares the contracts and initial resources it provides.

**Startup**:
The optional lifecycle phase in which a plugin begins operating after all registrations have been finalized.

**Host**:
The single plugin that owns the application's blocking runtime loop.
_Avoid_: System plugin

**Event publication**:
One delivery of an event value to its subscribers. Separate publications may execute concurrently.

**Event**:
An immutable value delivered to subscribers. Mutable shared payload belongs in a Resource.

**Subscription dependency**:
A completion-order constraint within one event publication. A dependent subscriber cannot begin until its prerequisites complete successfully.

**Publication handle**:
The completion result of an event publication. Callers may discard it or wait for every runnable subscriber to finish.

**First phase**:
The initial subscriber group in an event publication. Members may execute concurrently subject to dependencies between members; later phases wait for the group to complete.

**Last phase**:
The final subscriber group in an event publication. Members may execute concurrently subject to dependencies between members and begin only after earlier phases complete.

**Resource**:
Shared state whose declared access is coordinated by the engine. Plugins may also own state and coordinate its concurrent access themselves.

**Resource handle**:
A binding to a resource cell, obtained during registration and valid for the engine lifetime. The value it exposes is valid only while the owning handler holds its lock.

**Lock**:
The closure that binds a handler's resource handles. Requesting a handle is what declares the corresponding lock, so declaration and use cannot drift apart. It runs once, during registration.

**Required resource**:
A resource that must have an owner and initial value when registration is finalized.

**System fault**:
An unexpected engine or plugin failure that cannot be represented as an expected command response.

**Invocation context**:
The cancellation and deadline scope of one command or subscriber execution, bounded by the engine lifetime.

**Conflict-aware FIFO**:
Scheduler fairness in which later work may pass an earlier request only when it does not access any resource requested by the earlier work.

**Nested command**:
A command executed by another handler using only resource access already held by that handler.

**Architecture description**:
A read-only account of finalized plugin order, contract ownership, and subscription dependency graphs.

**Headless engine**:
An engine without a Host. It remains running until its context is canceled.

**Shutdown**:
The optional lifecycle phase that stops active plugins in reverse dependency order before the scheduler stops.

## UI Declarations

**Element**:
A frame-local declaration of layout, interaction, and visual intent.
_Avoid_: Widget, control

**Modifier**:
A value transformation that derives one Element declaration from another.
_Avoid_: Setter, constructor

**Pixel value**:
An absolute distance in screen pixels.
_Avoid_: Absolute value

**Relative value**:
A ratio of the containing axis, except for pivots, where it is a ratio of the Element's own axis.
_Avoid_: Percentage

**Borrowed children**:
An Element sequence the UI may hold by reference instead of copying. Its storage remains caller-owned and must stay stable while the UI frame is processed; whether any given sequence is borrowed or copied is unspecified.
_Avoid_: Copied children, owned children

**UI Frame**:
The complete set of root Element declarations produced for one update tick.
_Avoid_: Retained UI, scene

## Scene Declarations

**Camera**:
A declaration of a viewpoint and of the passes drawn from it. Its identity is also its place in the frame's ordering space, so declaring one twice is an error.

**Pass**:
One render pass a Camera emits, carrying the tag that selects materials for it, its target, and its clears.
_Avoid_: Render step, stage

**Pass tag**:
The name of what a Pass is for, and the key that selects which of a Scene material's entries serves it.
_Avoid_: Queue, light mode

**Scene material**:
The set of graphics materials one recorded thing offers, one per Pass tag. A Pass whose tag it has no entry for does not draw that thing.
_Avoid_: Shader

**Layer mask**:
A selection of which Cameras see a recorded item. Both a Camera and an item carry one, and an empty mask on either side means every layer.
_Avoid_: Render layer, culling group

**Selector**:
The scene and node names that address part of a model file. A Selector that matches nothing addresses nothing and never widens to the whole file.
_Avoid_: Path, query

**Re-rooting**:
The discarding of a selected node's authored world transform, so that the node's subtree is placed by the recording call's own transform instead.

**Residency**:
One model path's position in the load cycle: never asked for, loading, resident, or terminally failed.
_Avoid_: Cache state, load status

**Frame boundary**:
The point between two frames at which every deferred change to persistent scene state becomes visible — residency, unloads, and geometry bakes alike.

**Rest pose**:
A model's placement with no animation playing: its authored hierarchy resolved once. It is what a bounds query answers about, whatever the frame is playing.
_Avoid_: Bind pose, default pose

**Debug shape**:
A recorded primitive drawn from a plugin-owned unit mesh rather than from an asset on disk.
_Avoid_: Gizmo, primitive

**Instance**:
One placement of one recorded thing. A recording call carrying many transforms declares many Instances that share everything else it said.

**Batch**:
One run of recorded work merged into a single draw call, whatever supplies its per-item data. In Scene that is Instances sharing a mesh and a Scene material; in Canvas it is sprites sharing an Instance record buffer, or triangles sharing concatenated vertices.
_Avoid_: Cluster, group

## Shader Sources

**Shader source**:
One `.wgsl` file. Sources are what an include composes; a source is not a unit the GPU ever sees.

**Shader module**:
The compiled WGSL unit behind one shader identity. Many Shader sources flatten into one Shader module, so the two are never interchangeable.
_Avoid_: Calling an included source a module

**Root source**:
The Shader source a shader descriptor names, by path or by text. It is the entry point of one flatten, and the only source addressed from Go.

**Define**:
A valueless flag readable only by a preprocessor conditional. It never reaches WGSL.
_Avoid_: Macro, symbol

**Const**:
A named value that becomes a WGSL `const`. It is never readable by a conditional, and its value is text the preprocessor does not interpret.
_Avoid_: Macro, override

**Supply**:
The set of Defines and Const values Go provides to one shader, fixed when its descriptor is constructed.
_Avoid_: Options, flags

**Variant**:
The Shader module one Root source plus one Supply produces. One path with two Supplies is two shaders, not one shader with two states.

## Canvas Materials

**Canvas material**:
A shader, pipeline state, and the parameters that belong to the material rather than to a draw, bound to a Canvas draw. Canvas has three built-ins and an app may supply its own.
_Avoid_: Shader, effect

**Family**:
Sprite or triangles: the two Canvas draw shapes, which sample differently and can never be one shader. A Canvas material belongs to exactly one.
_Avoid_: Kind, mode

**Material set**:
One Canvas material per Family plus one shared parameter list. A Scope names a Material set because it covers draws of more than one Family; a draw names a single Canvas material because at a draw the Family is known.
_Avoid_: Using set and material interchangeably

**Scope**:
Something that covers many draws and supplies a Material set to those that name none: the whole Canvas op queue, a layer, a UI frame, or a UI element subtree. The nearer scope wins, and a scope that names an empty set stops the wider one rather than reading as saying nothing.
_Avoid_: Context, group

**Instance record**:
The fixed record one sprite contributes to its Batch's storage buffer. It is not a parameter, not a uniform, and not extensible.
_Avoid_: Instance buffer, which is the array of them

**Reserved parameter name**:
A parameter name Canvas consumes itself and never forwards to the Canvas material.
_Avoid_: Built-in parameter
