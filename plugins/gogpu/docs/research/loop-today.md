# How gogpu's loop runs today, thread by thread, on each platform

Answers [#613](https://github.com/dvoyni/cog/issues/613) on the map [#612](https://github.com/dvoyni/cog/issues/612). Facts only, no proposal.

Source: the gogpu fork at `submodules/gogpu`, commit `dffe7d7`. Paths below are relative to `submodules/gogpu`. Line numbers refer to that commit.

## 1. What `Run` does before the loop

`App.Run` (`app.go:368-404`), in order:

1. `runtime.LockOSThread()` with a deferred unlock (`app.go:371-372`). This pins whichever goroutine calls `Run`. On macOS, `internal/platform/darwin/init.go:28-32` also locks the main goroutine to the main OS thread in a package `init`, so `Run` must be called from the main goroutine there.
2. `initPlatform()` (`app.go:374`, body `app.go:547-619`):
   - sets the platform logger and makes the `PlatformManager` through `newPlatformManagerFn` (default `platform.NewManager`, `app.go:543`, `app.go:550`), then calls `manager.Init()` (`app.go:551`);
   - applies the app name and pending menus (`app.go:556-560`, `applyPendingMenus` `app.go:624-647`);
   - creates the primary `PlatformWindow` with `manager.CreateWindow(platform.Config{...})` (`app.go:563-578`) and stores it in `a.platWindow` (`app.go:584`);
   - applies the hit-test callback and header alignment (`app.go:586-590`);
   - makes sure `Input()` and `EventSource()` exist, without replacing ones made before `Run` (`app.go:596-597`);
   - registers `modalFrameTick` as the window's modal frame callback (`app.go:613`);
   - installs the macOS live-resize hook and phase hooks when the window implements them (`app.go:615-616`, `setupLiveResizeHook` `app.go:667-686`, `setupLiveResizePhaseHooks` `app.go:692-701`).
3. `a.renderLoop = thread.NewRenderLoop()` (`app.go:378`), which starts the render thread (section 2).
4. `initRenderer(platWindow)` (`app.go:380`, body `app.go:718-732`) runs `newRenderer(...)` on the render thread through `RunOnRenderThreadVoid` (`app.go:721-726`). `newRenderer` (`renderer.go:264-300`) makes the `Renderer` and its primary `RenderTarget` (`renderer.go:265-274`), then the wgpu instance (`renderer.go:277`), the surface (`renderer.go:285`), the adapter and device (`renderer.go:290`), and configures the surface (`renderer.go:295`). Present mode is chosen in `configure` by `resolvePresentMode` (`renderer.go:446-457`, `renderer.go:465-496`): VSync on picks FifoRelaxed then Fifo; VSync off picks Immediate, then Mailbox, then Fifo. On failure `Run` stops the render loop and destroys the window and manager (`app.go:380-385`).
5. `platWindow.Show()` (`app.go:387`) and `syncInitialSurfaceSize()` (`app.go:388`, `app.go:410-419`), which resizes the surface on the render thread once the window has a non-zero physical size.
6. `defer a.shutdown(platWindow)` (`app.go:390`). Shutdown order is in `app.go:421-456`: GPU wait, deferred destroys, tracked resources and `onClose` on the render thread; `Renderer.Destroy` on the render thread; platform window and manager destroyed on the main thread; `ReleaseInstance` on the render thread; then `renderLoop.Stop()`.
7. `registerPrimaryWindow` (`app.go:392`, `app.go:1002-1018`) makes the `WindowManager` and the primary `*Window` that holds `onDraw`, `onResize` and the primary `RenderTarget`.
8. `startRunLoop` (`app.go:393`, `app.go:460-476`): sets `running`, sets lifecycle to `AppRunning`, calls `onSurfaceAvailable`, sets `lastFrame`, makes the `Invalidator` wired to `manager.WakeUp` (`app.go:469`), stores `WakeUp` for `Quit` (`app.go:470`), makes the `AnimationController`, replays a redraw requested before `Run`, and invalidates once for the first frame (`app.go:472-475`).
9. `runMainLoop()` (`app.go:400`). On native builds it is `for running && !platWindow.ShouldClose() { runFrame() }` (`app_run_native.go:7-11`). The browser build replaces it with a `requestAnimationFrame` loop (`app_run_browser.go:25-39`).

`Run` returns only after the loop ends and `shutdown` has run (`app.go:402-403`).

## 2. Goroutines and OS threads

| Thread / goroutine | Made at | Owns | Runs |
|---|---|---|---|
| Main thread (caller of `Run`) | locked at `app.go:371`; on macOS also `darwin/init.go:31` | `PlatformManager`, every `PlatformWindow`, the OS message pump, `Invalidator`, `AnimationController`, input state, `inputEvents`, `WindowManager` | `runFrame`, `processEventsMultiThread`, all event callbacks, `onUpdate`, `onResize`, `modalFrameTick`, the macOS live-resize hook |
| Render thread | `thread.New` (`internal/thread/thread.go:30-59`), started by `thread.NewRenderLoop` (`internal/thread/renderloop.go:28-32`); its goroutine calls `runtime.LockOSThread` (`thread.go:42`) | wgpu instance, adapter, device, surfaces, swapchain, pipelines (`Renderer`, `renderer.go:192-261`) | every closure sent through `RunOnRenderThreadVoid`: renderer init, resize, `renderFrameGPU` (so `OnDraw`), shutdown steps |
| X11 reader goroutine (X11 only) | `go c.readerLoop()` (`internal/platform/x11/connection.go:170`) | the X11 socket read side | demuxes replies, errors and events (`connection.go:438-`) |
| Linux D-Bus menu goroutines | `go m.serve()` (`internal/platform/menu_linux.go:150`) | D-Bus AppMenu server | menu actions run on fresh goroutines (`menu_linux.go:551`, `menu_linux.go:576`), not on the main thread |
| Other platform goroutines | clipboard (`wayland/libwayland_clipboard.go:612`), DnD (`wayland/libwayland_dnd.go:324`), D-Bus (`dbus_linux.go:132`, `:302`), print jobs (`print_job.go:109`, `print_linux.go:218`, `print_windows.go:252`) | their own operation | not part of the frame loop |

Hand-over between main and render thread:

- **Synchronous calls.** `RunOnRenderThreadVoid` is `Thread.CallVoid` (`renderloop.go:73-75`, `thread.go:92-110`): it sends a closure on the `funcs` channel (buffered 16, `thread.go:32`) and blocks on a per-call `done` channel until the render thread has run it; a panic is carried back and re-raised on the caller (`thread.go:96-109`). The main thread is therefore blocked for the whole of every render-thread call, including the full frame. `CallAsync` exists (`thread.go:114-125`, `renderloop.go:79-81`) but the app does not use it; `appRenderLoop` only declares `RunOnRenderThreadVoid`, `Stop`, `RequestResize` and `ConsumePendingResize` (`app.go:16-21`).
- **Deferred resize.** `RequestResize` stores width, height and a pending flag in atomics (`renderloop.go:44-48`); `ConsumePendingResize` swaps the flag on the render thread (`renderloop.go:53-58`). Repeated requests coalesce to the last one.
- **Redraw requests.** `Invalidator` is a channel of capacity 1 plus a wake-up function (`invalidator.go:8-18`). `Invalidate` does a non-blocking send and always calls `WakeUp` (`invalidator.go:24-32`); `Consume` is a non-blocking receive (`invalidator.go:37-44`). `RequestRedraw` goes through it, or sets `pendingRedraw` before `startRunLoop` (`app.go:1351-1357`).
- **Animations.** `AnimationController` is an atomic counter (`animation.go:11-28`); `StartAnimation` increments it and requests a redraw (`app.go:1362-1366`).
- **Quit.** `Quit` clears the atomic `running` and calls the stored `WakeUp` (`app.go:1303-1308`).
- **Frame gating.** `frameCallbackReady` (`app.go:1315-1345`) asks the primary window, and secondary windows once one of them has prepared a gate (`renderer.secondaryFrameGatePending`, `renderer.go:249-252`, set in `markSecondaryFrameGatePending` `renderer.go:843-848`), whether it implements `platform.FrameGater` and is ready (`internal/platform/platform.go:378-384`). Only Wayland implements it (section 5). The window list is read under `windowManager.mu.RLock` (`app.go:1332-1333`).
- **Wayland display lock.** The render thread takes the platform display lock around swapchain acquire and present (`renderer.go:654-656`, `renderer.go:875-877`, `lockDisplay` `renderer.go:174-185`); only Wayland implements `DisplayLocker` (`platform.go:359-366`, `platform_linux.go:925-936`).
- **macOS autorelease pool.** The frame closure is wrapped in `runInFramePool` (`app.go:1063`), a fresh `NSAutoreleasePool` on darwin (`framepool_darwin.go:7-9`, `internal/platform/darwin/framepool.go:10-`), a plain call elsewhere (`framepool_other.go`).

The doc comment on `darwin/init.go:26` says "User callbacks (OnDraw, OnUpdate) will also execute on the main thread"; in the current code `OnDraw` runs on the render thread (`app.go:1062-1066`, `app.go:1112`), while the main thread waits for it.

## 3. The path of one frame

`runFrame` (`app.go:481-540`), on the main thread:

1. Reads `ContinuousRender`, `animations.IsAnimating()` and `invalidator.Consume()` (`app.go:482-484`). If none is true it blocks in `manager.WaitEvents()` (`app.go:486-489`).
2. `processEventsMultiThread()` (`app.go:493`, body `app.go:740-791`):
   - truncates `inputEvents` (`app.go:742`);
   - drains `manager.PollEvents()` until `EventNone` into a local slice (`app.go:749-755`);
   - `classifyEvent` on each (`app.go:759-761`, body `app.go:816-882`): appends to `inputEvents`, calls per-window callbacks, `EventSource` callbacks and `input.State` synchronously; `RequestRedraw` on expose, focus, scale change and file drop; handles close (`windowCloseEvent` `app.go:963-990`, which clears `running` when the last window closes and `quitOnLastWindowClosed` is set);
   - for the last primary resize, unless `InSizeMove()`: `renderLoop.RequestResize(physical)`, `onResize(logical)`, `RequestRedraw()` (`app.go:766-780`);
   - secondary resizes go to the render thread one by one (`app.go:783-785`, `handleSecondaryResize` `app.go:794-`);
   - `eventSource.dispatchEndFrame()` for gestures (`app.go:788-790`).
3. A second `Consume` picks up redraws requested by events (`app.go:497-499`).
4. Delta time from `lastFrame`, clamped to 0.066 s (`app.go:502-511`).
5. `onUpdate(dt)` (`app.go:516-518`), then `inputState.Update()` to clear edges (`app.go:525-527`).
6. A third `Consume` picks up redraws requested by `onUpdate` (`app.go:530-532`).
7. If `ContinuousRender` or invalidated, and `frameCallbackReady()`: `renderFrameMultiThread()` (`app.go:537-539`).

`renderFrameMultiThread` (`app.go:1024-1067`), on the main thread: under `windowManager.mu.RLock` it collects each visible window's `onDraw` (a black clear when nil, `app.go:1035-1042`), scale factor and physical size (`app.go:1026-1055`); returns if there are none; then calls `RunOnRenderThreadVoid(runInFramePool(renderFrameGPU(frames)))` and blocks until it returns (`app.go:1062-1066`).

`renderFrameGPU` (`app.go:1071-1169`), on the render thread:

1. Applies `ConsumePendingResize` to the primary surface (`app.go:1076-1080`), then re-syncs the primary surface to `platWindow.PhysicalSize()` (`app.go:1085-1090`).
2. `DrainDeferredDestroys` once (`app.go:1093`).
3. Per window: `syncFrameSurfaceSize` (`app.go:1100`, `app.go:1171-1189`); `prepareLazyAcquire` (`app.go:1105`, `renderer.go:924-939`); sets `renderer.currentSurface` (`app.go:1108`); builds a new `Context` and calls **`OnDraw`** (`app.go:1111-1112`).
   - **Acquire** is lazy: the first draw call reaches `ensureFrameStarted` (`renderer.go:943-964`), e.g. through `clear` (`renderer.go:1244-1251`) or `ensureFrameEncoder` (`renderer.go:992-1007`). `beginFrame` (`renderer.go:629-680`) calls `platWin.PrepareFrame()` and reconfigures on a size change (`renderer.go:640-648`), then `surface.GetCurrentTexture()` under the display lock (`renderer.go:654-656`). If `OnDraw` draws nothing, nothing is acquired or presented (`app.go:1134-1142`), and a redraw is requested only when an acquire was attempted and failed (`app.go:1135-1137`).
   - **Submit and present** happen in `endFrameForSurface` (`app.go:1148`, `renderer.go:724-771`): flush pending clear (`renderer.go:745-749`), composition blit (`renderer.go:754-756`), debug overlays (`renderer.go:758`), `submitFrameEncoder` → `Queue().Submit` (`renderer.go:760`, `renderer.go:1010-1022`, `renderer.go:1319-1326`), `syncFrameForPresent` (`renderer.go:765`, `renderer.go:781-806`), `present` → `surface.PresentWithDamage` under the display lock (`renderer.go:766`, `renderer.go:870-905`).
   - If present reports an outdated surface it re-runs `OnDraw` once at the live scale, and requests a redraw if still outdated (`app.go:1148-1154`). Overlays that want another frame request a redraw (`app.go:1160-1162`).
4. `pollSubmissions` once: non-blocking `Queue().Poll()` and resource triage (`app.go:1168`, `renderer.go:852-858`).

## 4. `Context`

`Context` (`context.go:21-26`) holds the `*Renderer`, the target `*RenderTarget` (nil means the renderer's primary), the scale factor and a `cleared` flag. A new one is made per window per frame by `newContextForSurface` (`context.go:41-50`, called at `app.go:1111` and again at `app.go:1150` on the outdated retry). Its doc comment says it is "only valid during the OnDraw callback and should not be stored" (`context.go:15-16`). All frame state it reaches (swapchain texture, view, frame encoder, `frameStarted`) lives on the `RenderTarget` and is reset per frame by `prepareLazyAcquire` and `resetLazyState` (`renderer.go:924-939`, `renderer.go:971-976`). `Context` exposes the `Renderer` (`context.go:262`), the surface view (`context.go:274`), the shared command encoder (`context.go:290`) and `RenderTarget()` (`context.go:340`).

## 5. Move and resize per platform

**Windows.** `DefWindowProc` runs a modal loop during title-bar drag and border resize. `WM_NCLBUTTONDOWN` starts a 1 ms timer before calling `DefWindowProc` and kills it if no modal loop began (`internal/platform/platform_windows.go:2896-2914`); `WM_ENTERSIZEMOVE` sets `inSizeMove` and (re)starts the timer (`:2916-2925`); `WM_EXITSIZEMOVE` kills the timer, clears `inSizeMove` and queues one final `EventResize` (`:2927-2947`). `WM_SIZE` during the modal loop updates the stored size but queues no event (`:2851-2894`). Each `WM_TIMER` calls the modal frame callback (`:2949-2962`); the timer is 1 ms so VSync paces it (`:110-120`). Secondary windows inherit the primary's callback (`:893-900`). These messages are dispatched from `PeekMessage`/`DispatchMessage` inside `PollEvents` (`:1225-1242`), so `modalFrameTick` runs nested inside `processEventsMultiThread` → `manager.PollEvents()` on the main thread.

`modalFrameTick` (`app.go:1201-1246`): delta time clamped (`:1203-1210`); `inputState.Update()` **before** `onUpdate` (`:1213-1220`, the reverse of `runFrame`); `RequestResize` with the current physical size (`:1228-1231`); `resizeSecondaryWindowsDuringModal` on the render thread (`:1237`, `:1252-1287`); `renderFrameMultiThread()` unconditionally, with no invalidation or `frameCallbackReady` check (`:1240`); then `platWindow.SyncFrame()` (`:1245`). It does not process events, does not call `onResize`, and does not touch `inputEvents`. `SyncFrame` on Windows calls `DwmFlush` only while `InSizeMove` (`platform_windows.go:1164-1168`, `:1813-1820`); it is also called from `syncFrameForPresent` before present (`renderer.go:801`), so a modal tick can flush twice.

**macOS.** `SetModalFrameCallback` is a no-op (`internal/platform/platform_darwin.go:592`); `InSizeMove` is `InLiveResize` (`:391-393`); `SyncFrame` is a no-op (`:474`). During live resize the window delegate's `windowDidResize:` calls the live-resize hook while `InLiveResize` is true (`internal/platform/darwin/windowdelegate.go:77-92`). The hook (`app.go:672-685`) does `RequestResize` with the physical size and `renderFrameMultiThread()`: no `onUpdate`, no event processing, no invalidation check. `windowWillStartLiveResize:` / `windowDidEndLiveResize:` toggle Metal transaction present on the render thread (`app.go:697-714`, `darwin/window.go:584-610`); the end also wakes the event loop (`darwin/window.go:609`). Resize events are not queued during live resize (`platform_darwin.go:660`, `:1063-1067`). No hook exists for window move.

**Linux X11.** `InSizeMove` is false, `SyncFrame` and `SetModalFrameCallback` are no-ops (`internal/platform/platform_linux.go:536`, `:544-545`). No modal loop is handled; resize arrives as ordinary events.

**Linux Wayland.** `InSizeMove` is false and `SetModalFrameCallback` a no-op (`platform_linux.go:722`, `:920`, `:3218-3224`). Resize arrives as ordinary events. The window implements `FrameGater` (`:942-951`), `PresentationSyncer` (`:849-863`) and `DisplayLocker` (`:925-936`). `SyncFrame` requests a `wl_surface.frame` callback (`:841-843`); it is requested before present (`renderer.go:762-766`), and `frameCallbackReady` holds rendering until the compositor's done event. `GOGPU_WAYLAND_FRAME_CALLBACK=0` disables the per-frame callback (`wayland/libwayland_frame.go:254-257`). `PollEvents` consumes the ready state after dispatch; the done event wakes `WaitEvents` but is a gate, not a redraw trigger (`platform_linux.go:3046-3055`).

The Linux backend is picked by environment: Wayland if `WAYLAND_DISPLAY` is set, else X11 if `DISPLAY` is set, else Wayland (`platform_linux.go:194-226`).

## 6. Where the loop sleeps or blocks

- **Idle wait** (main thread), only when not continuous, not animating and not invalidated (`app.go:486-489`):
  - Windows: `MsgWaitForMultipleObjectsEx(INFINITE, QS_ALLINPUT, MWMO_INPUTAVAILABLE)`; it does not remove messages (`platform_windows.go:1347-1355`). `WakeUp` posts `wmWakeUp` (`:1359-1361`, handled as a no-op at `:2751-2753`).
  - macOS: `nextEventMatchingMask` with `distantFuture`, then it dispatches that event and drains the rest through the routing handler, so on macOS `WaitEvents` also processes events (`platform_darwin.go:247-287`, `darwin/application.go:394-430`). `WakeUp` posts an empty application-defined event (`platform_darwin.go:292-296`).
  - X11: loops `PollEventTimeout` over primary and secondary connections in a 100 ms total budget, returning on an event or a pending `wakeCh` signal (`platform_linux.go:374-430`); `WakeUp` is a non-blocking send on `wakeCh` (`:434-439`). The wake channel is checked between slices, so a wake can wait up to one budget.
  - Wayland: `unix.Poll(..., -1)` on the display fd, a wake pipe, secondary display fds and the key-repeat timerfd, then drains the pipe (`platform_linux.go:3230-3278`); `WakeUp` writes one byte to the pipe (`:3283-3285`).
- **Wake sources:** OS input, `RequestRedraw`/`Invalidate` (always wakes, `invalidator.go:29-31`), `StartAnimation` (`app.go:1364`), `Quit` (`app.go:1305-1307`).
- **Main thread blocked on render thread:** every `RunOnRenderThreadVoid`, in particular the whole of `renderFrameGPU` (`app.go:1062-1066`, `thread.go:92-110`).
- **Swapchain acquire and present** (render thread): `GetCurrentTexture` and `PresentWithDamage` (`renderer.go:655`, `renderer.go:876`) block inside wgpu according to the present mode (Fifo/FifoRelaxed with VSync on, `renderer.go:465-496`). gogpu adds no frame-in-flight wait of its own; `submitTracked` is non-blocking (`renderer.go:1316-1326`) and `pollSubmissions` is non-blocking (`renderer.go:852-858`). `WaitForGPU` is used at shutdown (`app.go:438`, `renderer.go:1917-`).
- **Frame gating:** on Wayland a frame is skipped, not waited for, while the callback is pending (`app.go:537`, `app.go:1315-1320`); the loop then returns to `WaitEvents` unless continuous or animating.
- **Continuous and animating modes** do not wait for events at all; their pace comes from the blocking acquire/present when a frame is drawn. In animating mode without `RequestRedraw`, `onUpdate` runs every turn and nothing is drawn (`app.go:513-539`), so nothing in gogpu paces that turn.
- **Windows modal loop:** paced by the 1 ms `WM_TIMER` plus the blocking present and `DwmFlush` (`platform_windows.go:110-120`, `app.go:1240-1245`).

## 7. What is reachable from outside the package

Exported on `App` and used by the loop: `NewApp`, `Run`, `Quit`, `RequestRedraw`, `StartAnimation` (returns `*AnimationToken` with `Stop`), `OnDraw`, `OnUpdate`, `OnResize`, `OnClose`, lifecycle callbacks, `InSizeMove`, `PollInputEvent`, `EventSource`, `Input`, `NewWindow`, `PrimaryWindow`, `DeviceProvider`, `GPUContextProvider` (`app.go:115-1571`, `event_source.go:182`, `:216`, `window_manager.go:331`, `:450`, `gpucontext_adapter.go:229`). `Context` and its methods are exported (`context.go`), as are `Renderer.BeginFrame`, `EndFrame`, `Resize`, `WaitForGPU` (`renderer.go:608`, `:708`, `:551`, `:1920`) and `NewHeadlessRenderer` / `RenderToImage` (`renderer.go:311`, `:1764`).

Unexported: `runMainLoop`, `runFrame`, `startRunLoop`, `initPlatform`, `initRenderer`, `processEventsMultiThread`, `classifyEvent`, `renderFrameMultiThread`, `renderFrameGPU`, `modalFrameTick`, `resizeSecondaryWindowsDuringModal`, `frameCallbackReady`, `shutdown`, the `appRenderLoop` interface and the `renderLoop`, `invalidator`, `animations`, `manager`, `platWindow` fields (`app.go`). `newPlatformManagerFn` is an unexported package variable (`app.go:543`).

Internal packages, not importable from outside the module: `internal/platform` (every `PlatformManager`/`PlatformWindow` implementation, the event queue, `FrameGater`, `DisplayLocker`) and `internal/thread` (`Thread`, `RenderLoop`). A few platform types leak as aliases: `FileDialogOptions`, `FileTypeFilter` (`app.go:1427`, `:1430`).

So from outside there is no way to run one loop turn, pump events, or reach the render thread; only `Run` drives the loop.

## 8. What gogpu's tests cover

- `app_run_coverage_test.go`: `startRunLoop` (`:39`, `:70`), `runMainLoop` stopping (`:85`), `runFrame` idle-then-resize and continuous-with-update-and-input (`:94`, `:118`), `modalFrameTick` nil-window and full cycle (`:173`, `:187`), `resizeSecondaryWindowsDuringModal` guards (`:204`, `:212`), `frameCallbackReady` with and without gaters, secondary gates and fast path (`:231-313`).
- `app_coverage_test.go`: `processEventsMultiThread` with `InSizeMove` blocking or allowing resize (`:93`, `:120`), live-resize phase hooks (`:154`), `renderFrameGPU` with nil, unconfigured and zero-size surfaces and the no-draw no-reschedule case (`:201-294`), `renderFrameMultiThread` using the frame pool (`:309`).
- `app_test.go`: `Quit` and its wake-up (`:96-130`), `RequestRedraw` with and without invalidator (`:138`, `:145`), `StartAnimation` (`:157`), secondary resize in `processEventsMultiThread` (`:869-947`), `PollInputEvent` reset per frame (`:1360`).
- `invalidator_test.go` (coalescing, wake-up, concurrency) and `animation_test.go` (counter, double stop, concurrency).
- `renderer_skip_present_test.go` (lazy acquire, present only when started) and `context_presentation_sync_test.go` (presentation sync and Wayland-style gating paths).
- `internal/platform/darwin/app_run_test.go` (darwin build only): real `gogpu.App` runs, including panics in `OnUpdate`/`OnDraw` and `Quit` unblocking idle (`:46-173`).

These app tests use `mockRenderLoop`, whose `RunOnRenderThreadVoid` calls the closure inline (`app_test.go:583-585`), so the real render thread and main/render hand-over are not exercised by them. `internal/thread` has no test files. Only the darwin tests drive a real `Run`; no test drives the Windows `WM_TIMER` modal path or the Linux event waits end to end.
