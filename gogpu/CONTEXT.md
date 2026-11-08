# gogpu

The gogpu plugin implements gfx and platform over our fork of gogpu, driven from the kernel's own main-thread tick. This glossary records its domain language.

## Language

### The loop

**Host loop**:
gogpu's loop driven by its host, one call at a time from the kernel's main thread: start, pump the events, begin a frame, end it, stop.
_Avoid_: Run loop, render loop, game loop

**Pass-through**:
How the plugin serves gfx's seam: each call goes to the window, surface or device gogpu already keeps, nothing of gfx's reimplemented.
_Avoid_: Proxy, wrapper

**Pin**:
Running one of gfx's tasks on the main thread because the host loop or the OS requires it there; the plugin pins, gfx never does.

### The fork

**Fork change**:
A change the plugin needs in our fork of gogpu, goffi or wgpu, each proposed upstream as its own pull request.
_Avoid_: Patch, hack

**Documented limit**:
A behaviour a system lacks that the design records instead of fixing, such as Wayland's one window.
_Avoid_: Known bug, gap

### Frames

**Captured frame**:
A frame drawn into an offscreen texture of the plugin's own, copied out for a capture and then drawn onto the window, so every backend can capture.
_Avoid_: Screenshot frame

**Device replacement**:
Releasing a lost device and requesting a new one while every window and its surface survives.
_Avoid_: Device reset
