# Gfx

Gfx is the GPU as a kernel sees it: a thin mirror of wgpu, implemented by a backend plugin, that draws one frame per beat of the render track. It knows nothing of materials, scenes or the ECS. This glossary records its domain language.

## Language

### The device

**Backend**:
A plugin that implements gfx over a real or recorded GPU: gogpu, or the headless recording adapter.
_Avoid_: Driver, renderer, implementation

**Device**:
The GPU as every job shares it: creating and releasing resources, writing them, recording and submitting.
_Avoid_: Context, GPU, renderer

**Exclusive device**:
The device as only one job at a time may hold it, for what no other job may overlap: configuring surfaces, recovering a lost device, waiting for the GPU to go idle.
_Avoid_: Device lock, writable device

**Handle**:
A pointer-free name for one GPU resource, which names nothing once the resource is released.
_Avoid_: ID, reference, resource pointer

### Frames

**Frame**:
What one beat of the render track draws: a framebuffer for each open window, and the encoders recorded into it.
_Avoid_: Frame context, render pass, swapchain image

**Framebuffer**:
The image a window shows next, acquired at the start of a frame and presented at its end.
_Avoid_: Back buffer, swapchain texture, render target

**Surface**:
What a window is drawn through on the GPU; gfx keeps one per open window, named by that window.
_Avoid_: Swapchain, canvas

**Draw event**:
The event gfx publishes inside each frame, once its framebuffers are acquired, for the application's handlers to record the frame.
_Avoid_: Render event, draw callback

**Submit order**:
The number an encoder is handed under, by which a frame's encoders are submitted, shared by every plugin that records.
_Avoid_: Priority, layer, pass index
