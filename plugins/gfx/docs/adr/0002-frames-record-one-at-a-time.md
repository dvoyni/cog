# Frames record one at a time

wgpu lets a surface hold one acquired framebuffer at a time: acquiring again before the present fails with `ErrSurfaceAlreadyAcquired` (wgpu v0.34.2, `core/surface.go`). gfx therefore keeps the handle of the last frame's present and schedules each frame's acquire after it, and `DrawEvent.Ready` stays that acquire: a frame's recording starts only once the previous frame is presented. Frames still overlap on the GPU, through wgpu's swapchain, but not on the CPU, so the vsync preset needs only one render beat in flight.

## Considered Options

- **Acquire late:** hand a frame's encoders out at `DrawEvent` and its framebuffers under a second job handle, so frame N+1's offscreen passes record while frame N waits on the display. Rejected for now: it takes about a frame of latency off, but it adds a second readiness point to the API, and a `WriteBuffer` made while recording frame N+1 would reach the queue before frame N's submit, so frame N would read frame N+1's data. It can return under one rule, from [Per-frame transient memory](https://github.com/dvoyni/cog/issues/664): the writes made while recording a frame go only to regions that frame owns, such as a ring partitioned by the frames in flight, never to data rewritten in place like a camera uniform at a fixed offset. metronome's vsync preset then goes back to two beats in flight.
