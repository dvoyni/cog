# Handles survive device loss

When the device is lost, every object made on it stops working at once, and wgpu recreates nothing. gfx owns the handle tables and keeps every object's descriptor, slices deep-copied, so it recovers in place: on the first beat after the loss, instead of a frame, it releases the device, requests a new one, and recreates every object under the same handle, in dependency order (buffers, textures, samplers and shader modules; views and bind group layouts; pipeline layouts; pipelines with their "auto" layouts; bind groups), then publishes `DeviceRecoveredEvent`. No holder's handle goes stale, so an ECS component or a scene structure holding one needs nothing. What gfx cannot recreate is contents: buffers and textures come back empty, and their owners fill them again from `DeviceRecoveredEvent`, as render targets already are every frame.

## Considered Options

- **Every handle dies:** gfx publishes the loss and every holder recreates its own objects. Rejected: every plugin and every component holding a handle would carry its own recovery, for objects gfx can rebuild from what it already keeps.
- **Only the objects defined by their descriptors survive** (shader modules, layouts, pipelines, samplers, bind groups), while buffers and textures die. Rejected: bind groups name buffers and textures, so they would die with them, and owners would still have to recreate what gfx could have, only to refill it.
