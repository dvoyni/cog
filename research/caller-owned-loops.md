# How caller-owned event loops handle modal loops, macOS and Wayland, and what gogpu upstream has discussed

Research for [cog#614](https://github.com/dvoyni/cog/issues/614), on the map [cog#612](https://github.com/dvoyni/cog/issues/612). Gathered 2026-11-03.

The question: winit (`pump_app_events`), SDL3 (`SDL_PollEvent`, main callbacks) and GLFW (`glfwPollEvents`) all let the caller own the loop. How does each one handle the hard cases (Windows modal loops, macOS, Wayland FIFO, and which thread may present)? And has gogpu upstream already discussed an external or non-blocking loop?

## Sources and versions

Every claim below links to a file at a pinned commit.

| Source | Commit |
|---|---|
| winit | [`7ed7c20`](https://github.com/rust-windowing/winit/tree/7ed7c2031c405e37aefd8635506933fdaa51b919) (2026-11-03) |
| SDL3 | [`3c9321b`](https://github.com/libsdl-org/SDL/tree/3c9321be39de4250e6406c273e1e86fb286997e4) (2026-11-03) |
| GLFW | [`92dcf4c`](https://github.com/glfw/glfw/tree/92dcf4ce74f2e2554a98fea09be7c705c17daa5a) (2026-08-03) |
| Mesa (Vulkan WSI) | [`eead8dc`](https://gitlab.freedesktop.org/mesa/mesa/-/blob/eead8dcf02fc055f22a993f1e21f4674459efea3/src/vulkan/wsi/wsi_common_wayland.c) (2026-11-03) |
| wayland / wayland-protocols | [`f2ecaa6`](https://gitlab.freedesktop.org/wayland/wayland/-/blob/f2ecaa61a3a82944ae069de5413104ab7ba2109f/protocol/wayland.xml) / [`3eab2dc`](https://gitlab.freedesktop.org/wayland/wayland-protocols/-/blob/3eab2dccf4bfdd85c16d312a6c8c8fbc2a6e73a8/staging/fifo/fifo-v1.xml) |
| wgpu (Rust) | [`6ccdfb3`](https://github.com/gfx-rs/wgpu/tree/6ccdfb3bfe355779a8b26aafdf69fdae5f357a8f) |
| gogpu fork | `dvoyni/gogpu` [`dffe7d7`](https://github.com/dvoyni/gogpu/tree/dffe7d7cc23e3e298c529cd83934c26a64331215) (0 behind upstream), with `gogpu/wgpu` v0.34.2 |
| Apple and Microsoft docs | linked inline |

## Answer in short

- **No library keeps the caller's own loop running through a Windows modal loop.** The caller's call (`pump_app_events`, `SDL_PollEvent`, `glfwPollEvents`) is stuck inside `DispatchMessage` → `DefWindowProc` until the drag or resize ends. Each library runs **user code from inside the window procedure** instead:
  - SDL3 uses a `WM_TIMER` that calls `SDL_AppIterate`, or sends `SDL_EVENT_WINDOW_EXPOSED` to event watchers.
  - winit uses `WM_PAINT`, re-armed with `RDW_INTERNALPAINT`, which calls the `RedrawRequested` handler.
  - GLFW calls the refresh callback, but only on `WM_PAINT` damage.
  - None of them uses a fibre or a separate message thread.
- **macOS can be pumped from the caller's main-thread loop.** SDL, GLFW and gogpu call `nextEventMatchingMask:… inMode:NSDefaultRunLoopMode` with `distantPast`. winit instead runs and stops `[NSApp run]` on every pump. Live resize (and menu tracking) still block inside `sendEvent:`, so the frame has to come from a callback: SDL uses an `NSTimer` in `NSRunLoopCommonModes`, winit and GLFW use `drawRect:`. For launch, GLFW and winit run `[NSApp run]` once and stop it in `applicationDidFinishLaunching:`. winit notes that the menu bar is unresponsive on 10.15 if activation is done before that notification.
- **On Wayland, FIFO present can hang forever on a hidden surface**, because compositors should not fire frame callbacks for invisible surfaces. Mesa's Vulkan WSI blocks in `vkQueuePresentKHR` on its legacy path. With the `wp_fifo_v1` protocol (Mesa 25.0+) it no longer blocks, but it may then not throttle at all on an occluded surface. Every library works around this with **frame-callback gating plus a bounded timeout**: SDL waits at most 50 ms, GLFW 20 ms, and SDL skips the swap entirely when the window is hidden. A non-blocking pump has to do the same, and never wait on present without a bound.
- **Present thread:**
  - SDL GPU requires acquire and submit on the thread that created the window.
  - GLFW allows `glfwSwapBuffers` from any thread, but events, window creation and every callback stay on the main thread.
  - winit leaves presentation to the renderer, but says to render inside `RedrawRequested` for macOS resize correctness.
  - Metal `presentsWithTransaction` (which the gogpu fork uses) ties `present` to the current Core Animation transaction.

  In practice, an acquire/present API reachable from the main-thread tick is the portable choice.
- **gogpu upstream has no issue, PR or discussion that proposes `PumpEvents`, a `Start`/`Stop` split, or a caller-owned OS loop.** The nearest thing is **ADR-070** in [discussion #468](https://github.com/gogpu/gogpu/discussions/468#discussioncomment-18147244) (by lkmavi, 2026-08-25, status *Proposed*). It lists the "App owns event/render loop" pattern of winit, GLFW and SDL3 as a **missing contract** in gogpu, and makes a "standalone engine embedding contract … on caller-owned loop" a **P0** item. That is the place to anchor the upstream proposal. The ROADMAP lists an "Independent Render Thread" idea whose research doc is not public.

## 1. Windows modal loops

**Background.** `WM_ENTERSIZEMOVE` is "sent one time to a window after it enters the moving or sizing modal loop … The operation is complete when DefWindowProc returns" ([Microsoft Learn: WM_ENTERSIZEMOVE](https://learn.microsoft.com/en-us/windows/win32/winmsg/wm-entersizemove)). Until `DefWindowProc` returns, the thread is inside the system's own loop, and only the window procedure runs.

### winit (`pump_app_events`)

- The pump installs the app handler for the whole call. It waits with `MsgWaitForMultipleObjectsEx`, then drains with `PeekMessageW` → `TranslateMessage` → `DispatchMessageW` ([event_loop.rs L293-L323, L352-L399](https://github.com/rust-windowing/winit/blob/7ed7c2031c405e37aefd8635506933fdaa51b919/winit-win32/src/event_loop.rs#L293-L399)). A modal loop therefore runs *inside* `pump_app_events`, and the handler is still installed, so events reach the app's callbacks instead of being buffered. The handler is installed by `set_app` ([runner.rs L239](https://github.com/rust-windowing/winit/blob/7ed7c2031c405e37aefd8635506933fdaa51b919/winit-win32/src/event_loop/runner.rs#L239)), and `should_buffer` is true only when no handler is installed ([runner.rs L333-L338](https://github.com/rust-windowing/winit/blob/7ed7c2031c405e37aefd8635506933fdaa51b919/winit-win32/src/event_loop/runner.rs#L333-L338)).
- **How frames keep coming:** a hidden "thread event target" window is kept visible-but-layered because it "has to be visible to receive WM_PAINT messages (which are used for delivering events during resizes)" ([L1055-L1058](https://github.com/rust-windowing/winit/blob/7ed7c2031c405e37aefd8635506933fdaa51b919/winit-win32/src/event_loop.rs#L1055-L1058)). Its window procedure re-arms itself with `RedrawWindow(…, RDW_INTERNALPAINT)` on every non-paint message ([L2602-L2604](https://github.com/rust-windowing/winit/blob/7ed7c2031c405e37aefd8635506933fdaa51b919/winit-win32/src/event_loop.rs#L2602-L2604)). Real windows turn `WM_PAINT` into `RedrawRequested` and re-arm when the app asked for another redraw ([L1396-L1414](https://github.com/rust-windowing/winit/blob/7ed7c2031c405e37aefd8635506933fdaa51b919/winit-win32/src/event_loop.rs#L1396-L1414)). No timer is used.
- **Title-bar click pause:** on `WM_NCLBUTTONDOWN` on the caption, winit posts a dummy `WM_MOUSEMOVE` with `lparam = 0`. That cancels the ~500 ms click-or-drag detection early. The source comment notes that this does *not* prevent the freeze from the right-click system menu ([L1334-L1365](https://github.com/rust-windowing/winit/blob/7ed7c2031c405e37aefd8635506933fdaa51b919/winit-win32/src/event_loop.rs#L1334-L1365)). `WM_ENTERSIZEMOVE` and `WM_EXITSIZEMOVE` only set a flag ([L1318-L1331](https://github.com/rust-windowing/winit/blob/7ed7c2031c405e37aefd8635506933fdaa51b919/winit-win32/src/event_loop.rs#L1318-L1331)).
- **Never blocking the caller for long:** after any `RedrawRequested`, the runner sets `interrupt_msg_dispatch`, "to ensure that `pump_events` can't block an external event loop for too long" ([runner.rs L386-L392](https://github.com/rust-windowing/winit/blob/7ed7c2031c405e37aefd8635506933fdaa51b919/winit-win32/src/event_loop/runner.rs#L386-L392)).
- **The documented contract:** "Some events _must_ only be handled synchronously via the closure … These events can not be buffered," including `RedrawRequested` and the lifecycle events. The API is "not a portable API … You almost certainly shouldn't use this API, unless you absolutely know it's the only practical option" ([pump_events.rs L22-L62](https://github.com/rust-windowing/winit/blob/7ed7c2031c405e37aefd8635506933fdaa51b919/winit-core/src/event_loop/pump_events.rs#L22-L62)).

### SDL3

- On `WM_ENTERSIZEMOVE` or `WM_ENTERMENULOOP`, SDL starts `SetTimer(hwnd, SDL_IterateMainCallbacks, USER_TIMER_MINIMUM, NULL)`. It also resets the keyboard, "as we won't get any key up events during the modal loop". It kills the timer on `WM_EXITSIZEMOVE` or `WM_EXITMENULOOP`, and a nesting counter (`in_modal_loop`) covers nested loops ([SDL_windowsevents.c L1903-L1956](https://github.com/libsdl-org/SDL/blob/3c9321be39de4250e6406c273e1e86fb286997e4/src/video/windows/SDL_windowsevents.c#L1903-L1956)).
- Each `WM_TIMER` calls `SDL_OnWindowLiveResizeUpdate` ([L1932-L1947](https://github.com/libsdl-org/SDL/blob/3c9321be39de4250e6406c273e1e86fb286997e4/src/video/windows/SDL_windowsevents.c#L1932-L1947)). That function calls `SDL_IterateMainCallbacks` when the app uses main callbacks. Otherwise it sends `SDL_EVENT_WINDOW_EXPOSED` "so the application can redraw" ([SDL_video.c L4359-L4369](https://github.com/libsdl-org/SDL/blob/3c9321be39de4250e6406c273e1e86fb286997e4/src/video/SDL_video.c#L4359-L4369)).
- **A plain `SDL_PollEvent` loop does not tick during the modal loop.** The only hook is an event watch or filter. `SDL_EVENT_WINDOW_EXPOSED` "can be redrawn directly from event watchers … data1 is 1 for live-resize expose events" ([SDL_events.h L140-L141](https://github.com/libsdl-org/SDL/blob/3c9321be39de4250e6406c273e1e86fb286997e4/include/SDL3/SDL_events.h#L140-L141)). It is "guaranteed to be sent from the OS on the main thread and you are expected to redraw your window in response" ([L1519-L1523](https://github.com/libsdl-org/SDL/blob/3c9321be39de4250e6406c273e1e86fb286997e4/include/SDL3/SDL_events.h#L1519-L1523)).
- **Main callbacks:** with main callbacks, `SDL_AppIterate` "is called over and over, possibly at the refresh rate of the display or some other metric that the platform dictates". The app must not call `SDL_PollEvent` itself ([README-main-functions.md L159-L186](https://github.com/libsdl-org/SDL/blob/3c9321be39de4250e6406c273e1e86fb286997e4/docs/README-main-functions.md#L159-L186)). This is the inversion that lets SDL keep the app iterating through the modal loop.

### GLFW

- GLFW does no timer and no modal-loop workaround. `WM_ENTERSIZEMOVE`, `WM_ENTERMENULOOP` and their exit messages only release and recapture a disabled or captured cursor ([win32_window.c L989-L1018](https://github.com/glfw/glfw/blob/92dcf4ce74f2e2554a98fea09be7c705c17daa5a/src/win32_window.c#L989-L1018)). `WM_PAINT` raises window damage, which calls the refresh callback ([L1140-L1144](https://github.com/glfw/glfw/blob/92dcf4ce74f2e2554a98fea09be7c705c17daa5a/src/win32_window.c#L1140-L1144)).
- The documented contract: "On some platforms, a window move, resize or menu operation will cause event processing to block … You can use the window refresh callback to redraw the contents of your window when necessary during such operations" ([glfw3.h L4514-L4518](https://github.com/glfw/glfw/blob/92dcf4ce74f2e2554a98fea09be7c705c17daa5a/include/GLFW/glfw3.h#L4514-L4518)). A pure move produces no damage, so the frame freezes for the duration of the move.

### The gogpu fork today

- gogpu already does the SDL timer approach. It starts the timer on `WM_NCLBUTTONDOWN` (to cover the ~500 ms detection) and on `WM_ENTERSIZEMOVE`, kills it on `WM_EXITSIZEMOVE`, and on `WM_TIMER` calls `modalFrameCallback` ([platform_windows.go L2895-L2960](https://github.com/dvoyni/gogpu/blob/dffe7d7cc23e3e298c529cd83934c26a64331215/internal/platform/platform_windows.go#L2895-L2960)).
- That callback is `App.modalFrameTick`. It runs `onUpdate` and then renders on the render thread ([app.go L599-L613, L1191-L1240](https://github.com/dvoyni/gogpu/blob/dffe7d7cc23e3e298c529cd83934c26a64331215/app.go#L1191-L1240)).
- No menu-loop hook was seen, which differs from SDL.

**Implication for `PumpEvents`:** while a modal loop is running, `PumpEvents` cannot return. The kernel's tick is frozen unless gogpu calls back into it from `WM_TIMER`. The options that exist in practice:

1. A registered "modal tick" callback that the kernel supplies. This is SDL's `SDL_AppIterate`-from-timer, or gogpu's `modalFrameTick` generalised.
2. Accept a frozen tick during drag, which is GLFW's behaviour.

## 2. macOS

**Can `NSApp` be pumped from the caller's loop?** Yes, on the main thread:

- **SDL3** calls `[NSApp finishLaunching]` itself during registration ([SDL_cocoaevents.m L522-L556](https://github.com/libsdl-org/SDL/blob/3c9321be39de4250e6406c273e1e86fb286997e4/src/video/cocoa/SDL_cocoaevents.m#L522-L556)). Each pump calls `nextEventMatchingMask:NSEventMaskAny untilDate:… inMode:NSDefaultRunLoopMode dequeue:YES` and `[NSApp sendEvent:]`, with `distantPast` for a non-blocking poll. Running modal sessions get `runModalSession:` first ([L602-L654](https://github.com/libsdl-org/SDL/blob/3c9321be39de4250e6406c273e1e86fb286997e4/src/video/cocoa/SDL_cocoaevents.m#L602-L654)).
- **GLFW** runs `[NSApp run]` once at init if the app is not yet finished launching ([cocoa_init.m L599-L600](https://github.com/glfw/glfw/blob/92dcf4ce74f2e2554a98fea09be7c705c17daa5a/src/cocoa_init.m#L599-L600)). It stops it from `applicationDidFinishLaunching:` with `[NSApp stop:nil]` plus a posted empty event ([L439-L443](https://github.com/glfw/glfw/blob/92dcf4ce74f2e2554a98fea09be7c705c17daa5a/src/cocoa_init.m#L439-L443)). After that it polls with `nextEventMatchingMask … distantPast` ([cocoa_window.m L1545-L1563](https://github.com/glfw/glfw/blob/92dcf4ce74f2e2554a98fea09be7c705c17daa5a/src/cocoa_window.m#L1545-L1563)).
- **winit** does it differently. Each `pump_app_events` calls `[NSApp run]` and stops it at the point the run loop is about to wait, after a redraw, or after the timeout. On first use it runs until launch finishes ([event_loop.rs L542-L598](https://github.com/rust-windowing/winit/blob/7ed7c2031c405e37aefd8635506933fdaa51b919/winit-appkit/src/event_loop.rs#L542-L598)). Stopping means `app.stop(None)` plus a posted dummy event ([L645-L652](https://github.com/rust-windowing/winit/blob/7ed7c2031c405e37aefd8635506933fdaa51b919/winit-appkit/src/event_loop.rs#L645-L652)).
  - The docs explain why: "the lower level polling primitives on MacOS are private implementation details for `NSApplication`".
  - They give the cost: this is "less efficient than polling", and "the `NSApplication` is stopped while outside of the Winit event loop - and that's observable (for example to crates like `rfd`)".
  - iOS is unsupported because "It's not possible to stop and start an `NSApplication` repeatedly" ([pump_events.rs L78-L101](https://github.com/rust-windowing/winit/blob/7ed7c2031c405e37aefd8635506933fdaa51b919/winit-core/src/event_loop/pump_events.rs#L78-L101)).
- **The gogpu fork** already uses the SDL style: `finishLaunching` and then `nextEventMatchingMask` ([darwin/application.go L82-L83, L225-L244](https://github.com/dvoyni/gogpu/blob/dffe7d7cc23e3e298c529cd83934c26a64331215/internal/platform/darwin/application.go#L225-L244)). A non-blocking `PumpEvents` on macOS is mostly a matter of exposing the `distantPast` path.

**What breaks:**

- **Live resize** runs a tracking loop inside `sendEvent:`, so the caller's loop is blocked for its duration.
  - SDL starts a 60 Hz `NSTimer` on `windowWillStartLiveResize:`, added to `NSRunLoopCommonModes` so that it also fires in the tracking mode. The timer calls the same `SDL_OnWindowLiveResizeUpdate` used on Windows ([SDL_cocoawindow.m L1141-L1159](https://github.com/libsdl-org/SDL/blob/3c9321be39de4250e6406c273e1e86fb286997e4/src/video/cocoa/SDL_cocoawindow.m#L1141-L1159)).
  - winit relies on `drawRect:`. It warns: "If you render outside of Winit you are likely to see window resizing artifacts since MacOS expects applications to render synchronously during any `drawRect` callback" ([pump_events.rs L99-L101](https://github.com/rust-windowing/winit/blob/7ed7c2031c405e37aefd8635506933fdaa51b919/winit-core/src/event_loop/pump_events.rs#L99-L101); see also L48-L55).
  - GLFW raises damage, and so the refresh callback, from `drawRect:` ([cocoa_window.m L534-L536](https://github.com/glfw/glfw/blob/92dcf4ce74f2e2554a98fea09be7c705c17daa5a/src/cocoa_window.m#L534-L536)).
  - The gogpu fork has a live-resize hook invoked from `windowDidResize:` ([platform_darwin.go L398-L401](https://github.com/dvoyni/gogpu/blob/dffe7d7cc23e3e298c529cd83934c26a64331215/internal/platform/platform_darwin.go#L398-L401)).
- **Menus:** GLFW's general note that "a window move, resize or menu operation will cause event processing to block" applies here too ([glfw3.h L4514-L4518](https://github.com/glfw/glfw/blob/92dcf4ce74f2e2554a98fea09be7c705c17daa5a/include/GLFW/glfw3.h#L4514-L4518)). SDL's timer is in common modes, so it would also tick during menu tracking. That last point is inferred from the run loop mode and was not tested.
- **`applicationDidFinishLaunching:`**
  - Apple's docs say `run()` calls `finishLaunching()` "before it starts the event loop" ([NSApplication.finishLaunching](https://developer.apple.com/documentation/appkit/nsapplication/finishlaunching())). `run()` "continues until a stop(_:) or terminate(_:) message is received" ([NSApplication.run](https://developer.apple.com/documentation/appkit/nsapplication/run())).
  - winit sets the activation policy, activates, and builds the menu only inside `applicationDidFinishLaunching:`, "Otherwise the menu bar is initially unresponsive on macOS 10.15" ([app_state.rs L123-L174](https://github.com/rust-windowing/winit/blob/7ed7c2031c405e37aefd8635506933fdaa51b919/winit-appkit/src/app_state.rs#L123-L174)).
  - SDL and the gogpu fork call `finishLaunching` directly and set the policy and activation right away. gogpu does this without a delegate notification ([darwin/application.go L76-L86](https://github.com/dvoyni/gogpu/blob/dffe7d7cc23e3e298c529cd83934c26a64331215/internal/platform/darwin/application.go#L76-L86)). This is a known-risk spot to test on the prototype.

## 3. Wayland: FIFO on a hidden or occluded surface

**Why it hangs.** The core protocol says "A server should avoid signaling the frame callbacks if the surface is not visible in any way, e.g. the surface is off-screen, or completely obscured" ([wayland.xml L1652-L1654](https://gitlab.freedesktop.org/wayland/wayland/-/blob/f2ecaa61a3a82944ae069de5413104ab7ba2109f/protocol/wayland.xml#L1652-L1654)). Anything that waits on a frame callback without a bound will hang while the window is hidden.

**Mesa Vulkan WSI:**

- **Legacy path (no `wp_fifo_v1`):** a FIFO present requests a frame callback and clears `legacy_fifo_ready` ([wsi_common_wayland.c L3490-L3498](https://gitlab.freedesktop.org/mesa/mesa/-/blob/eead8dcf02fc055f22a993f1e21f4674459efea3/src/vulkan/wsi/wsi_common_wayland.c#L3490-L3498)). The next `queue_present` loops on `wl_display_dispatch_queue` with no timeout until it is set again ([L3390-L3398](https://gitlab.freedesktop.org/mesa/mesa/-/blob/eead8dcf02fc055f22a993f1e21f4674459efea3/src/vulkan/wsi/wsi_common_wayland.c#L3390-L3398)). On a hidden surface, **`vkQueuePresentKHR` blocks indefinitely**.
- **With `wp_fifo_v1`:** "applications will no longer block in QueuePresentKHR due to frame callback", and the image count drops from 4 to 3 ([L1882-L1899](https://gitlab.freedesktop.org/mesa/mesa/-/blob/eead8dcf02fc055f22a993f1e21f4674459efea3/src/vulkan/wsi/wsi_common_wayland.c#L1882-L1899)). The 25.0.0 release notes list "wsi/wayland: Don't fallback to broken legacy throttling with FIFO" and "Add forward progress guarantee for present wait" ([relnotes 25.0.0](https://gitlab.freedesktop.org/mesa/mesa/-/blob/main/docs/relnotes/25.0.0.rst), released 2025-02-19).
- **The catch of the protocol:** "If the surface is not being updated by the compositor (off-screen, occluded) the compositor may ignore the constraint. Clients must use an additional mechanism such as frame callbacks or timestamps to ensure throttling occurs under all conditions" ([fifo-v1.xml L108-L125](https://gitlab.freedesktop.org/wayland/wayland-protocols/-/blob/3eab2dccf4bfdd85c16d312a6c8c8fbc2a6e73a8/staging/fifo/fifo-v1.xml#L108-L125)). With the protocol, a hidden window may spin unthrottled instead of hanging.

**How the libraries avoid hanging:**

- **SDL3 (EGL)** always sets the real `eglSwapInterval` to 0 "to avoid blocking inside EGL". It waits on the frame callback itself, and "If a swap blocks for too long waiting on a callback, we just go on" ([SDL_waylandopengles.c L63-L96](https://github.com/libsdl-org/SDL/blob/3c9321be39de4250e6406c273e1e86fb286997e4/src/video/wayland/SDL_waylandopengles.c#L63-L96)). The bound is **50 ms** ("20hz, so we'll progress even if throttled to zero") ([SDL_waylandframebuffer.c L82-L125](https://github.com/libsdl-org/SDL/blob/3c9321be39de4250e6406c273e1e86fb286997e4/src/video/wayland/SDL_waylandframebuffer.c#L82-L125)). Swaps on known-hidden windows are skipped entirely, because "compositors will intentionally stall us indefinitely" ([SDL_waylandopengles.c L115-L126](https://github.com/libsdl-org/SDL/blob/3c9321be39de4250e6406c273e1e86fb286997e4/src/video/wayland/SDL_waylandopengles.c#L115-L126)).
- **SDL GPU (Vulkan)** returns a NULL swapchain texture, which is not an error, for a hidden window ([SDL_gpu_vulkan.c L10151-L10154](https://github.com/libsdl-org/SDL/blob/3c9321be39de4250e6406c273e1e86fb286997e4/src/gpu/vulkan/SDL_gpu_vulkan.c#L10151-L10154)).
- **GLFW (EGL)** waits for the frame callback with a **20 ms** poll timeout, and skips the swap if the window is not visible ([wl_window.c L2342-L2377](https://github.com/glfw/glfw/blob/92dcf4ce74f2e2554a98fea09be7c705c17daa5a/src/wl_window.c#L2342-L2377)). It documents that "When the swap interval is greater than zero and the window is not in view, this function may take a few extra milliseconds to return" ([glfw3.h L6177-L6179](https://github.com/glfw/glfw/blob/92dcf4ce74f2e2554a98fea09be7c705c17daa5a/include/GLFW/glfw3.h#L6177-L6179)).
- **winit** does not present. It aligns `RedrawRequested` with frame callbacks when the app calls `pre_present_notify` ([window.rs L732-L792](https://github.com/rust-windowing/winit/blob/7ed7c2031c405e37aefd8635506933fdaa51b919/winit-core/src/window.rs#L732-L792)), so a hidden window simply gets no redraw. Its Wayland pump honours the caller's timeout and spawns a notifier thread when one is given ([event_loop/mod.rs L205-L242](https://github.com/rust-windowing/winit/blob/7ed7c2031c405e37aefd8635506933fdaa51b919/winit-wayland/src/event_loop/mod.rs#L205-L242)).
- **MAILBOX** is listed by wgpu as supported on "Wayland on Vulkan". FIFO "will block until there is a spot in the queue" in `get_current_texture` ([wgpu-types surface.rs L47-L106](https://github.com/gfx-rs/wgpu/blob/6ccdfb3bfe355779a8b26aafdf69fdae5f357a8f/wgpu-types/src/surface.rs#L47-L106)). wgpu-core bounds acquire at `FRAME_TIMEOUT_MS = 1000` ([present.rs L31](https://github.com/gfx-rs/wgpu/blob/6ccdfb3bfe355779a8b26aafdf69fdae5f357a8f/wgpu-core/src/present.rs#L31)), and `gogpu/wgpu` v0.34.2 copies that 1 s acquire timeout (`hal/vulkan/swapchain.go`, `acquireNextImage`). **A 1 s acquire bound is still a 1 s stall of the kernel tick.** It also does not cover Mesa's legacy blocking *present*.
- **The gogpu fork** already gates rendering on a `wl_surface.frame` three-state machine, modelled on winit ([libwayland_frame.go](https://github.com/dvoyni/gogpu/blob/dffe7d7cc23e3e298c529cd83934c26a64331215/internal/platform/wayland/libwayland_frame.go), [app.go L1310-L1320](https://github.com/dvoyni/gogpu/blob/dffe7d7cc23e3e298c529cd83934c26a64331215/app.go#L1310-L1320)). No timeout fallback was seen in the gate. If a callback never arrives, the frame is skipped, not hung. That is correct for a non-blocking pump, as long as acquire and present are never attempted while the gate is closed.

**Implication:** the caller-facing acquire must be able to say "no frame this tick" without blocking. That covers a closed frame gate, a hidden window, or an acquire timeout. SDL GPU's NULL texture is the precedent. Behind that, keep frame-callback gating with a bounded fallback (SDL 50 ms, GLFW 20 ms) so that `wp_fifo_v1` compositors cannot spin a hidden window.

## 4. Present thread

- **SDL GPU:**
  - `SDL_ClaimWindowForGPUDevice` and `SDL_AcquireGPUSwapchainTexture` "should only be called from the thread that created the window" ([SDL_gpu.h L4470-L4495, L4590-L4632](https://github.com/libsdl-org/SDL/blob/3c9321be39de4250e6406c273e1e86fb286997e4/include/SDL3/SDL_gpu.h#L4590-L4632)).
  - Present happens at submit, which "must be called from the thread the command buffer was acquired on" ([L4718-L4726](https://github.com/libsdl-org/SDL/blob/3c9321be39de4250e6406c273e1e86fb286997e4/include/SDL3/SDL_gpu.h#L4718-L4726)). Acquire and present are therefore effectively on the window thread.
  - In VSYNC mode acquire "may block on vblank" (L4603-L4604).
- **GLFW:** event processing and window creation "are all restricted to the main thread", and so every callback is too. `glfwSwapBuffers` and `glfwSwapInterval` "may be called from any thread" ([intro.md L454-L493](https://github.com/glfw/glfw/blob/92dcf4ce74f2e2554a98fea09be7c705c17daa5a/docs/intro.md#L454-L493)). Vulkan present is left to `vkQueuePresentKHR` (glfw3.h L6170).
- **winit:** `Window` is `Send + Sync`, but "some platforms (macOS, Web and iOS) only allow user interface interactions on the main thread", so calls from other threads are scheduled onto the main thread ([window.rs L621-L629](https://github.com/rust-windowing/winit/blob/7ed7c2031c405e37aefd8635506933fdaa51b919/winit-core/src/window.rs#L621-L629)). winit does not own presentation. Its docs still steer rendering into `RedrawRequested` on the event thread for portability (pump_events.rs L48-L55).
- **Metal:** `presentsWithTransaction` makes the layer "draw its contents synchronously, using whichever Core Animation transaction is current at the time you call the drawable's present()" ([CAMetalLayer.presentsWithTransaction](https://developer.apple.com/documentation/quartzcore/cametallayer/presentswithtransaction)). `nextDrawable` waits up to one second and then returns nil ([CAMetalLayer.nextDrawable](https://developer.apple.com/documentation/quartzcore/cametallayer/nextdrawable())).
  - `gogpu/wgpu`'s Metal `Present` uses the transaction path: commit, then `waitUntilScheduled`, then `present` (`hal/metal/queue.go`, `Queue.Present`).
  - The gogpu fork turns this on during live resize ([renderer.go L916](https://github.com/dvoyni/gogpu/blob/dffe7d7cc23e3e298c529cd83934c26a64331215/renderer.go#L916)).
- **gogpu today** presents on its own render thread: "Main thread: Window events only … Render thread: All GPU operations" ([app.go L361-L371](https://github.com/dvoyni/gogpu/blob/dffe7d7cc23e3e298c529cd83934c26a64331215/app.go#L361-L371)).

**Summary:** no library *forbids* presenting from the main thread, and SDL GPU *requires* the window thread. Making gogpu's render thread optional, with acquire and present on the caller's main thread, matches SDL GPU. It also keeps the macOS transaction present on the thread that owns the Core Animation transaction. The Metal threading point is an inference from Apple's text and is worth confirming on hardware.

## 5. gogpu upstream: what exists

I searched every issue and PR title (142 issues), every discussion (24) with their comments, and full-text searches for "event loop", "PumpEvents", "external loop", "game loop", "non-blocking", "main loop", "render thread", "own loop", "LockOSThread" and "embed". Nothing proposes `PumpEvents`, `Start`/`Stop`, or returning control to a caller's OS loop. The relevant pieces:

- **[Discussion #468, ADR-070 "Engine UI Embedding and VEE Validation"](https://github.com/gogpu/gogpu/discussions/468#discussioncomment-18147244)**, by lkmavi, 2026-08-25, status *Proposed*.
  - It comes from darkliquid's ironwail-go (pure-Go Quake on gogpu) experiments ([ironwail-go discussion #17](https://github.com/darkliquid/ironwail-go/discussions/17)): "all churn occurred at the integration seam — how a host that owns its own frame loop composites UI over 3D content."
  - Its comparison table has the row "App owns event/render loop | winit, GLFW, SDL3 | Engine path (caller loop) — **missing contract**".
  - P0 item: "Standalone engine embedding contract — widget tree → caller-owned texture/surface on caller-owned loop; gogpu owns one submit per frame".
  - P1 item: "Document dual integration paths: `desktop.Run` vs engine embed".
  - Its open question 1 asks whether the contract belongs in `gpucontext` or in `gogpu`.
  - No maintainer reply to ADR-070 was found in the thread. kolkov's 2026-09-07 comment covers other topics.
- **ROADMAP "Future Ideas": "Independent Render Thread — Decouple render loop from message pump"** ([ROADMAP.md L246](https://github.com/dvoyni/gogpu/blob/dffe7d7cc23e3e298c529cd83934c26a64331215/ROADMAP.md#L246)). It links `docs/dev/research/INDEPENDENT_RENDER_THREAD.md`, which is not in the public repo; upstream `docs/` holds only `ARCHITECTURE.md`, `PRINTING.md` and `windows-transparency.md`. The `modalFrameTick` comment says that thread "would eliminate this callback entirely" (app.go L611-L612). That direction is the opposite of ours: it moves the render loop further from the main thread, not towards a caller-owned tick.
- **[Issue #469](https://github.com/gogpu/gogpu/issues/469)** (kolkov, open): a browser `requestAnimationFrame` run loop. It states that `App.Run()` "uses a blocking `for` loop that will either deadlock or busy-spin in the browser". This is the same blocking-`Run` constraint, solved by inverting into callbacks rather than by exposing a pump.
- **[Discussion #167](https://github.com/gogpu/gogpu/discussions/167)** (multi-window RFC) chose a "centralized event loop" over Gio-style per-window goroutines with `LockOSThread`, because "macOS forces all AppKit calls onto the main thread".
- **ROADMAP shipped items that a pump builds on:** "SDL-style event queue (ADR-058) — `App.PollInputEvent()`" (L40), "Three-mode render loop … lazy swapchain acquire (ADR-023)" (L51), "Multi-thread architecture (Ebiten/Gio pattern)" (L68), "Quit wakes idle loop (#406)" (L36), and "Wayland frame callback gating (ADR-049)" (L123).

**Where to anchor the proposal:** reply on #468 under ADR-070 or link it from a new issue. ADR-070 already names the gap and cites the same three libraries. The pump is the event-loop half of its "caller-owned loop" P0 item.

## Design notes drawn from the above

These are inferences, offered for the prototype ticket, not facts from the sources:

1. `PumpEvents` cannot return during a Windows move or resize or a macOS live resize. Either keep a modal-tick callback, as SDL does and gogpu's `modalFrameTick` already does, now pointed at a kernel-supplied function, or document that the tick freezes, as GLFW does. Add `WM_ENTERMENULOOP` as SDL does.
2. The acquire API needs a non-blocking "no frame" result, for a closed frame gate, a hidden or minimised window, or a timeout, rather than relying on the 1 s acquire timeout.
3. On macOS, check that `finishLaunching` without `[NSApp run]` gives a responsive menu bar. winit's comment says it may not on 10.15.
4. Keep acquire and present on the main thread when the render thread is off. That matches SDL GPU and the Core Animation transaction present.
