# Which of gfx's surface calls each OS requires on the main thread

Research for [cog#710](https://github.com/dvoyni/cog/issues/710), on the map [cog#611](https://github.com/dvoyni/cog/issues/611). Gathered 2026-11-07.

The question: for each OS the gogpu backend targets, which of gfx's surface calls must run on the OS main thread (the window's thread), and why? The calls are the tasks in `gfx.Tasks` ([gfx/resources.go](https://github.com/dvoyni/cog/blob/6581d1b0bab1c6b365fee243f0b3eb7c0554bd92/gfx/resources.go#L8-L25)): CreateSurface, ReleaseSurface (and the teardown release of [cog#711](https://github.com/dvoyni/cog/issues/711), which makes the same calls), ConfigureSurfaces, AcquireFramebuffers and PresentFramebuffers. The decision that uses this list is [cog#708](https://github.com/dvoyni/cog/issues/708): the gogpu plugin pins, with `configurator.PinToMainThread`, only the tasks each OS requires.

It builds on [What must run on the main thread on each platform](https://github.com/dvoyni/cog/blob/research/main-thread-rules/research/main-thread-rules.md) ([cog#645](https://github.com/dvoyni/cog/issues/645)), [How gogpu's loop runs today](https://github.com/dvoyni/cog/blob/research/gogpu-loop-today/research/gogpu-loop-today.md) ([cog#613](https://github.com/dvoyni/cog/issues/613)) and [Caller-owned event loops](https://github.com/dvoyni/cog/blob/research/caller-owned-loops/research/caller-owned-loops.md) ([cog#614](https://github.com/dvoyni/cog/issues/614)). None of them is repeated here.

## Sources and versions

| Source | Version |
|---|---|
| gogpu fork | `dvoyni/gogpu` [`92d976d`](https://github.com/dvoyni/gogpu/tree/92d976d655fb366878fa120272e97093bf71cf1b) (the `submodules/gogpu` pin at cog `6581d1b`) |
| gogpu/wgpu (Go HAL the fork uses) | [`v0.34.2`](https://github.com/gogpu/wgpu/tree/v0.34.2), from the fork's `go.mod` |
| cog | [`6581d1b`](https://github.com/dvoyni/cog/tree/6581d1b0bab1c6b365fee243f0b3eb7c0554bd92) |
| Apple | developer.apple.com API reference (read 2026-11-07); the [Creating a custom Metal view](https://developer.apple.com/documentation/metal/creating-a-custom-metal-view) sample code; the archived [Thread Safety Summary](https://developer.apple.com/library/archive/documentation/Cocoa/Conceptual/Multithreading/ThreadSafetySummary/ThreadSafetySummary.html) |
| Microsoft | learn.microsoft.com (read 2026-11-07), linked inline |
| Khronos | Vulkan specification, latest on [docs.vulkan.org](https://docs.vulkan.org/spec/latest/) and the [registry man pages](https://registry.khronos.org/vulkan/specs/latest/man/html/) (read 2026-11-07) |
| Mesa (Vulkan WSI) | [`eead8dc`](https://gitlab.freedesktop.org/mesa/mesa/-/tree/eead8dcf02fc055f22a993f1e21f4674459efea3/src/vulkan/wsi) |
| libwayland, libX11 | as pinned in the [main-thread-rules note](https://github.com/dvoyni/cog/blob/research/main-thread-rules/research/main-thread-rules.md#sources-and-versions) |
| Rust wgpu, raw-window-metal (corroboration only) | [`gfx-rs/wgpu@b672a54`](https://github.com/gfx-rs/wgpu/tree/b672a540043b7177cdd53858340d7aca7ff8eb68), [`rust-windowing/raw-window-metal@7a83ffd`](https://github.com/rust-windowing/raw-window-metal/tree/7a83ffdac443f878ddb1f5c4b410fa486d9d6af1) |

Below, `G:` means `https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/`, `W:` means `https://github.com/gogpu/wgpu/blob/v0.34.2/` and `C:` means `https://github.com/dvoyni/cog/blob/6581d1b0bab1c6b365fee243f0b3eb7c0554bd92/`.

## Answer in short

| gfx task | macOS / Metal | Windows / Vulkan | Windows / DX12 | Linux / Vulkan on X11 | Linux / Vulkan on Wayland |
|---|---|---|---|---|---|
| CreateSurface | any thread | any thread | any thread | any thread | any thread |
| ReleaseSurface (and the teardown release) | any thread | **required on main** | unclear | any thread | any thread |
| ConfigureSurfaces | any thread (one property unclear) | **required on main** | **required on main** | any thread | any thread |
| AcquireFramebuffers | any thread | **required on main** | unclear | any thread | any thread |
| PresentFramebuffers | any thread while `presentsWithTransaction` is false | **required on main** | **required on main** | any thread | any thread |

"Any thread" means the call has no thread affinity. It still needs the external synchronization gfx already gives it: the write lock on `gfx.Device` for create, release and configure, and frame order for acquire and present.

- **macOS: pin nothing.** The thread-bound work is creating the `CAMetalLayer` and attaching it to the `NSView`. The fork's platform does both at window creation, on the main thread, before gfx sees the window. gfx's CreateSurface only retains the layer. Configure, `nextDrawable` and present are `CAMetalLayer` and Metal calls with no thread rule. `CAMetalLayer` is not `@MainActor`, unlike `NSView`. Apple's own sample renders from a background thread, and the fork's HAL presents without a transaction by default. `contentsScale` is not set by configure at all: the fork's platform owns it.
- **Windows: pin every call that touches a swapchain.** The rule comes from window messages, not from a main-thread rule. Khronos and Microsoft both say a swapchain call may `SendMessage` to the window. From another thread that call blocks until the window's thread pumps. Both say to make the call on the pump thread, or keep the pump running. cog's main thread cannot keep it running. It parks in `WaitMainThread` between ticks and pumps only inside platform's ProcessEvents. ProcessEvents needs the write lock on `*platform.WindowManager`, which ConfigureSurfaces and AcquireFramebuffers hold for reading. A message sent from inside either of them is therefore never pumped, and the job deadlocks. Surface creation has no swapchain on either API, so it runs on any thread.
- **Linux: pin nothing.** Vulkan has no thread affinity, only external synchronization. On X11, Vulkan's WSI uses a separate Xlib connection that nothing else in the fork reads. On Wayland, libwayland is thread-safe, and the spec obliges the WSI to keep its own event queue. The Wayland frame gate imposes an order, not a thread: the `wl_surface.frame` request goes before `vkQueuePresentKHR`, from the same job.

## macOS / Metal

### Who does what in the fork

- **Layer creation and attachment happen in the platform, at window creation.** `CreateWindow` calls `darwin.NewSurface` right after `darwin.NewWindow` ([G:internal/platform/platform_darwin.go#L141-L158](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/internal/platform/platform_darwin.go#L141-L158)).
  - `NewSurface` creates the `CAMetalLayer` and sets its pixel format, `opaque`, `autoresizingMask`, `contentsGravity` and `contentsScale`, then its frame from the content view's bounds ([G:internal/platform/darwin/surface.go#L316-L378](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/internal/platform/darwin/surface.go#L316-L378)).
  - It attaches the layer with `setLayer:` and then `setWantsLayer:YES` ([G:internal/platform/darwin/window.go#L417-L436](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/internal/platform/darwin/window.go#L417-L436)).
  - Window creation is main-thread-only already ([main-thread-rules, macOS](https://github.com/dvoyni/cog/blob/research/main-thread-rules/research/main-thread-rules.md#macos)), and this code is part of it.
- **gfx's CreateSurface only retains the layer.** The window's native handle is the attached layer, falling back to the content view only when no layer was attached ([G:internal/platform/platform_darwin.go#L355-L364](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/internal/platform/platform_darwin.go#L355-L364)). The HAL's `Instance.CreateSurface` requires a Metal layer target and calls `Retain` on it, nothing else ([W:hal/metal/api.go#L39-L49](https://github.com/gogpu/wgpu/blob/v0.34.2/hal/metal/api.go#L39-L49)).
- **gfx's ReleaseSurface only releases that retain** ([W:hal/metal/surface.go#L248-L255](https://github.com/gogpu/wgpu/blob/v0.34.2/hal/metal/surface.go#L248-L255)). Detaching the layer stays with the platform's window destroy ([G:internal/platform/platform_darwin.go#L320-L330](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/internal/platform/platform_darwin.go#L320-L330)).
- **Configure** sets `device`, `pixelFormat`, `drawableSize`, `framebufferOnly`, `maximumDrawableCount`, `allowsNextDrawableTimeout = NO`, `displaySyncEnabled`, `presentsWithTransaction` and `contentsGravity = topLeft` on the layer ([W:hal/metal/surface.go#L29-L157](https://github.com/gogpu/wgpu/blob/v0.34.2/hal/metal/surface.go#L29-L157)). It does not set `contentsScale`.
- **Acquire** is `nextDrawable` inside an autorelease pool ([W:hal/metal/surface.go#L184-L232](https://github.com/gogpu/wgpu/blob/v0.34.2/hal/metal/surface.go#L184-L232)). The pool locks the calling goroutine to its OS thread only while the pool is open ([W:hal/metal/objc.go#L490-L503](https://github.com/gogpu/wgpu/blob/v0.34.2/hal/metal/objc.go#L490-L503)), so any thread works.
- **Present** ([W:hal/metal/queue.go#L413-L459](https://github.com/gogpu/wgpu/blob/v0.34.2/hal/metal/queue.go#L413-L459)):
  - With `presentsWithTransaction` false, it calls `[commandBuffer presentDrawable:]` and `commit`.
  - With it true, it calls `commit`, then `waitUntilScheduled`, then `[drawable present]`.

### `presentsWithTransaction` in the fork

- **The HAL's default is false.** Its comment says why: "enabling transaction present outside a live main-thread CA transaction defers every frame until the next AppKit event (blank window)". Configure re-applies the current value each time ([W:hal/metal/surface.go#L113-L128](https://github.com/gogpu/wgpu/blob/v0.34.2/hal/metal/surface.go#L113-L128)).
- The comment on `Queue.Present` says the opposite, "default on macOS" ([W:hal/metal/queue.go#L405-L410](https://github.com/gogpu/wgpu/blob/v0.34.2/hal/metal/queue.go#L405-L410)). That comment is stale: nothing in the HAL turns it on.
- **Only gogpu's App turns it on.** It does so for the duration of a live-resize drag, from `windowWillStartLiveResize:` to `windowDidEndLiveResize:`. It goes through `SetPresentsWithTransaction` ([W:hal/metal/surface.go#L166-L182](https://github.com/gogpu/wgpu/blob/v0.34.2/hal/metal/surface.go#L166-L182)), called from `setupLiveResizePhaseHooks` and `setTransactionPresent` ([G:app.go#L667-L693](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/app.go#L667-L693), [G:renderer.go#L920-L932](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/renderer.go#L920-L932)).
  - During that drag, App renders a whole frame from inside `windowDidResize:`, with the main thread blocked until the render thread has presented ([G:app.go#L628-L665](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/app.go#L628-L665)).
  - A gfx backend that calls wgpu itself never goes through this path. Its surfaces keep the default, false.
- **Apple:** with the default false, the layer "displays the output of a rendering pass to the display as quickly as possible and asynchronously to any Core Animation transactions". With true, it draws "using whichever Core Animation transaction is current at the time you call the drawable's present()", after `commit` and `waitUntilScheduled` ([CAMetalLayer.presentsWithTransaction](https://developer.apple.com/documentation/quartzcore/cametallayer/presentswithtransaction)).
- **Apple:** implicit transactions "are committed automatically when the thread's runloop next iterates" ([CATransaction](https://developer.apple.com/documentation/quartzcore/catransaction)). "If your application does not have a runloop, you must call this method explicitly" ([CATransaction.flush()](https://developer.apple.com/documentation/quartzcore/catransaction/flush())).
  - A kernel worker has no run loop. A transaction present from a worker would join that worker's own implicit transaction, which nothing commits.
  - So transaction present, if cog ever wants it for live resize, belongs on the main thread inside AppKit's resize transaction. During the drag the main thread is inside AppKit's tracking loop, not in `TickMainThread`, so a pinned PresentFramebuffers could not run there either. That is a live-resize design question ([cog#616](https://github.com/dvoyni/cog/issues/616)), not a reason to pin today.

### What Apple says about each call

- **Creating and attaching the layer is main-thread work.**
  - `NSView` is declared `@MainActor class NSView` ([NSView](https://developer.apple.com/documentation/appkit/nsview)).
  - "You should create, destroy, resize, move, and perform other operations on NSView objects only from the main thread" ([Thread Safety Summary, NSView Restrictions](https://developer.apple.com/library/archive/documentation/Cocoa/Conceptual/Multithreading/ThreadSafetySummary/ThreadSafetySummary.html)).
  - For a layer-hosting view, "you must set the layer property first and then set this property to true" ([NSView.wantsLayer](https://developer.apple.com/documentation/appkit/nsview/wantslayer)). The fork does this in that order.
  - Corroboration: Rust's raw-window-metal panics when `Layer::from_ns_view` "is called from a thread that is not the main thread" ([raw-window-metal src/lib.rs#L371-L403](https://github.com/rust-windowing/raw-window-metal/blob/7a83ffdac443f878ddb1f5c4b410fa486d9d6af1/src/lib.rs#L371-L403)).
  - In the fork this all happens in the platform, so gfx's CreateSurface needs no pin.
- **Retain and release have no thread rule.** "Object allocation and retain count functions" are listed as thread-safe ([Thread Safety Summary](https://developer.apple.com/library/archive/documentation/Cocoa/Conceptual/Multithreading/ThreadSafetySummary/ThreadSafetySummary.html)).
- **`CAMetalLayer` has no main-actor isolation.** It is declared `class CAMetalLayer`, and `CALayer` is `class CALayer`, where `NSView` is `@MainActor` ([CAMetalLayer](https://developer.apple.com/documentation/quartzcore/cametallayer), [CALayer](https://developer.apple.com/documentation/quartzcore/calayer)). Neither page states a thread rule for `drawableSize`, `nextDrawable` or `present`.
- **Apple's own sample renders off the main thread.** [Creating a custom Metal view](https://developer.apple.com/documentation/metal/creating-a-custom-metal-view) has a `RENDER_ON_MAIN_THREAD` switch (`AAPLConfig.h` L8-L14 in the sample download). With it off:
  - `nextDrawable` and `presentDrawable:` run from the `CVDisplayLink` callback, "a secondary thread" (`AAPLNSView.m` L154-L182, `AAPLRenderer.m` L131 and L170).
  - `drawableSize` is set from AppKit's resize and backing-scale callbacks on the main thread (`AAPLNSView.m` L207-L223), inside `@synchronized(_metalLayer)`. Rendering takes the same lock (`AAPLView.m` L99-L139, L146-L160). Apple's comment: "Synchronize if rendering on background thread to ensure resize operations from the main thread are complete before rendering".
  - So acquire and present have no main-thread rule, and `drawableSize` needs mutual exclusion with them. gfx gives that already: configure holds the device's write lock, and acquire and present hold its read lock.
- **`drawableSize` from a background thread** is not shown by Apple, because the sample sets it on main. Two codebases do it:
  - The fork does it on gogpu's render thread, an ordinary Go thread with no run loop. `Surface.Configure` runs there ([G:app.go#L695-L705](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/app.go#L695-L705), [G:renderer.go#L629-L648](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/renderer.go#L629-L648)).
  - Rust wgpu configures and acquires on whatever thread calls it, with the layer behind a mutex ([wgpu-hal metal/surface.rs#L35-L43, L250-L357, L361-L420](https://github.com/gfx-rs/wgpu/blob/b672a540043b7177cdd53858340d7aca7ff8eb68/wgpu-hal/src/metal/surface.rs#L250-L357)). It documents that reading `bounds` and `contentsScale` "is safe to call off of the main thread" (L186-L206). Its only main-thread check is for reading `NSScreen`/`NSWindow` HDR info (L55-L95).
- **Unclear: `contentsGravity` set from a worker.** Configure also sets `contentsGravity`, which is render-tree state. Per `CATransaction` above, a change made on a thread with no run loop goes into an implicit transaction that only an explicit `flush` or `commit` sends.
  - The worst case is cosmetic. The layer keeps the platform's `resize` gravity instead of `topLeft`, so during a live resize the old frame stretches instead of letterboxing.
  - To settle it, run configure from a kernel worker with `CA_DEBUG_TRANSACTIONS=1` and look for an uncommitted-transaction warning, or check `contentsGravity` on the presentation layer. Bracketing the property writes in an explicit `CATransaction` would sidestep it on any thread.
- **`contentsScale` belongs to the platform.**
  - "For layers you create and manage yourself, you must set the value of this property yourself" ([CALayer.contentsScale](https://developer.apple.com/documentation/quartzcore/calayer/contentsscale)).
  - The fork sets it at creation and in `Surface.UpdateSize`, which runs only from `Show` ([G:internal/platform/darwin/surface.go#L345-L351, L427-L452](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/internal/platform/darwin/surface.go#L427-L452), [G:internal/platform/platform_darwin.go#L552-L569](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/internal/platform/platform_darwin.go#L552-L569)).
  - `Surface.Resize` would update it, but nothing calls it, by design ("RETINA-002: Do NOT call w.surface.Resize() here", [G:internal/platform/platform_darwin.go#L677-L680](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/internal/platform/platform_darwin.go#L677-L680)).
  - So moving a window to a screen with another scale changes `drawableSize`, through configure, but not `contentsScale`. That is a fork gap, outside this question. If gfx's backend ever sets `contentsScale` itself, that write is a layer-tree change on a hosted view's root layer. Like the platform's other view and layer work, it belongs on main.

## Windows / Vulkan

### What Khronos says

The Win32 section of the WSI chapter ([Vulkan spec, Win32 Platform](https://docs.vulkan.org/spec/latest/chapters/VK_KHR_surface/wsi.html#platformCreateSurface_win32)):

> Some Vulkan functions may call the SendMessage system API when interacting with a VkSurfaceKHR through a VkSwapchainKHR. In a multithreaded environment, calling SendMessage from a thread that is not the thread associated with pCreateInfo->hwnd will block until the application has processed the window message. Thus, applications should either call these Vulkan functions on the message pump thread, or make sure their message pump is actively running. Failing to do so may result in deadlocks.
>
> The functions subject to this requirement are: vkCreateSwapchainKHR, vkDestroySwapchainKHR, vkAcquireNextImageKHR and vkAcquireNextImage2KHR, vkQueuePresentKHR, vkReleaseSwapchainImagesKHR …

`vkCreateWin32SurfaceKHR` and `vkDestroySurfaceKHR` are not in that list.

### What each gfx task calls

- **CreateSurface:** `vkCreateWin32SurfaceKHR` ([W:hal/vulkan/api_windows.go#L26-L55](https://github.com/gogpu/wgpu/blob/v0.34.2/hal/vulkan/api_windows.go#L26-L55)). It is not in the list, so it runs on **any thread**.
- **ConfigureSurfaces:** `Surface.Configure` ([W:hal/vulkan/api.go#L292](https://github.com/gogpu/wgpu/blob/v0.34.2/hal/vulkan/api.go#L292)) creates the swapchain with `vkCreateSwapchainKHR`, retiring the old one with `vkDestroySwapchainKHR` ([W:hal/vulkan/swapchain.go#L478-L600, L812-L821](https://github.com/gogpu/wgpu/blob/v0.34.2/hal/vulkan/swapchain.go#L478-L600)). It is **required on main**.
- **AcquireFramebuffers:** `vkAcquireNextImageKHR` ([W:hal/vulkan/swapchain.go#L1083](https://github.com/gogpu/wgpu/blob/v0.34.2/hal/vulkan/swapchain.go#L1083)). **Required on main.**
- **PresentFramebuffers:** `vkQueuePresentKHR` ([W:hal/vulkan/swapchain.go#L1256](https://github.com/gogpu/wgpu/blob/v0.34.2/hal/vulkan/swapchain.go#L1256)). **Required on main.**
- **ReleaseSurface:** `Surface.Destroy` destroys the swapchain (`vkDestroySwapchainKHR`) before `vkDestroySurfaceKHR` ([W:hal/vulkan/api.go#L412-L432](https://github.com/gogpu/wgpu/blob/v0.34.2/hal/vulkan/api.go#L412-L432)). **Required on main.** A ConfigureSurfaces that recreates a lost surface releases it the same way ([C:gfx/surface.go#L160-L168](https://github.com/dvoyni/cog/blob/6581d1b0bab1c6b365fee243f0b3eb7c0554bd92/gfx/surface.go#L160-L168)).

### Why "keep the pump running" does not hold in cog

- The main thread pumps Win32 messages only inside platform's ProcessEvents, a pinned task ([C:platform/doc.go#L49-L53](https://github.com/dvoyni/cog/blob/6581d1b0bab1c6b365fee243f0b3eb7c0554bd92/platform/doc.go#L49-L53)). Between ticks it parks in `WaitMainThread` on a Go channel ([C:kernel/supervisor.go#L60-L112](https://github.com/dvoyni/cog/blob/6581d1b0bab1c6b365fee243f0b3eb7c0554bd92/kernel/supervisor.go#L60-L112)).
- A message sent from another thread is delivered only while the owner thread runs message-retrieval code ([main-thread-rules, Windows](https://github.com/dvoyni/cog/blob/research/main-thread-rules/research/main-thread-rules.md#windows)).
- ProcessEvents takes `kernel.Write[*WindowManager]` ([C:platform/eventprocessor.go#L43](https://github.com/dvoyni/cog/blob/6581d1b0bab1c6b365fee243f0b3eb7c0554bd92/platform/eventprocessor.go#L43)).
  - ConfigureSurfaces and AcquireFramebuffers hold `kernel.Read[*platform.WindowManager]` ([C:gfx/surface.go#L151](https://github.com/dvoyni/cog/blob/6581d1b0bab1c6b365fee243f0b3eb7c0554bd92/gfx/surface.go#L151), [C:gfx/framepool.go#L383](https://github.com/dvoyni/cog/blob/6581d1b0bab1c6b365fee243f0b3eb7c0554bd92/gfx/framepool.go#L383)).
  - A writer waits for every reader to finish ([C:kernel/lockstate.go#L5-L18](https://github.com/dvoyni/cog/blob/6581d1b0bab1c6b365fee243f0b3eb7c0554bd92/kernel/lockstate.go#L5-L18)).
  - So a swapchain call that sends a message from inside either task waits for a pump that waits for it. That is a deadlock.
- PresentFramebuffers holds no `WindowManager` lock. A message from present would therefore wait until the next ProcessEvents rather than forever. That holds only if the application schedules ProcessEvents independently of the present, which the platform leaves to the application. Khronos's "should" covers it either way.
- Pinned on main, the same calls send their messages straight to the window procedure, because "When the DXGI call is on the same thread as the message pump, SendMessage calls the window's WindowProc" ([DXGI best practices](https://learn.microsoft.com/en-us/windows/win32/direct3darticles/dxgi-best-practices#multithreading-and-dxgi)). The same applies to any `SendMessage` a Vulkan driver makes.

### What the fork does today, for contrast

- gogpu's `Run` makes all these calls on its render thread while the main thread blocks in `Thread.CallVoid` ([G:internal/thread/thread.go#L90-L110](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/internal/thread/thread.go#L90-L110), [G:app.go#L695-L705](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/app.go#L695-L705)). That is the pattern Khronos warns against. No hang is recorded against it, so in ordinary windowed use drivers apparently do not send messages.
- The sources name no case where they do. The likely ones are exclusive full screen and mode changes ([DXGI overview, Multithread considerations](https://learn.microsoft.com/en-us/windows/win32/direct3ddxgi/d3d10-graphics-programming-guide-dxgi#multithread-considerations)). That is evidence of how rare the message is, not permission to make the calls off main.
- `HostLoop` makes them on the Start thread, the main thread ([G:hostloop.go#L26-L29](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/hostloop.go#L26-L29)).
- Consequence for [cog#616](https://github.com/dvoyni/cog/issues/616): with these tasks pinned, they cannot run while the main thread sits in `DefWindowProc`'s move or resize modal loop. The frame freezes during a drag unless the modal timer ticks the kernel.

## Windows / DX12

### What Microsoft says

- "Because of DXGI's close interaction with windowing, it occasionally sends window messages to the associated application window. DXGI needs the windowing changes to occur before it can continue, so it will use SendMessage, which is a synchronous call." The page continues: "Remember that IDXGISwapChain calls, such as IDXGISwapChain::Present, are also considered DXGI calls; DXGI may defer work from ResizeBuffers or ResizeTarget until Present is called." It closes: "To avoid this, never block the message pump. If a block is unavoidable, then all DXGI interaction should occur on the same thread as the message pump." ([DXGI best practices, Multithreading and DXGI](https://learn.microsoft.com/en-us/windows/win32/direct3darticles/dxgi-best-practices#multithreading-and-dxgi))
- "There are two situations where this can occur. The rendering thread is not the message-pump thread. The thread executing a DXGI API is not the same thread that created the window." The example given is `Present1` calling `SetWindowPos` or `SetWindowStyle` during a mode change ([DXGI overview, Multithread considerations](https://learn.microsoft.com/en-us/windows/win32/direct3ddxgi/d3d10-graphics-programming-guide-dxgi#multithread-considerations)).
- `Present`: "Because calling Present might cause the render thread to wait on the message-pump thread, be careful when calling this method in an application that uses multiple threads" ([IDXGISwapChain::Present](https://learn.microsoft.com/en-us/windows/win32/api/dxgi/nf-dxgi-idxgiswapchain-present)).
- "your application should also use that same thread for Microsoft DirectX Graphics Infrastructure (DXGI) operations, especially when the application makes calls to the IDXGISwapChain::Present method" ([Multithreading and DXGI](https://learn.microsoft.com/en-us/windows/win32/direct3d11/overviews-direct3d-11-render-multi-thread-intro)). That page is about Direct3D 11's immediate context, so it does not bind DX12 by itself.
- `MakeWindowAssociation` with a window handle makes DXGI watch that window's messages, and `DXGI_MWA_NO_WINDOW_CHANGES` stops it ([IDXGIFactory::MakeWindowAssociation](https://learn.microsoft.com/en-us/windows/win32/api/dxgi/nf-dxgi-idxgifactory-makewindowassociation)).
  - The HAL passes only `DXGI_MWA_NO_ALT_ENTER` ([W:hal/dx12/surface.go#L168-L177](https://github.com/gogpu/wgpu/blob/v0.34.2/hal/dx12/surface.go#L168-L177)), so DXGI still watches the window.
- None of these pages says that a windowed flip-model swapchain, which never calls `SetFullscreenState`, sends a message from `CreateSwapChainForHwnd`, `ResizeBuffers`, `GetCurrentBackBufferIndex` or `Release`. Microsoft gives the rule for DXGI interaction in general: "occasionally".

### What each gfx task calls

- **CreateSurface:** the HAL stores the HWND and makes no DXGI call ([W:hal/dx12/instance.go#L177-L189](https://github.com/gogpu/wgpu/blob/v0.34.2/hal/dx12/instance.go#L177-L189)). It runs on **any thread**.
- **ConfigureSurfaces:**
  - The first configure creates the swapchain with `CreateSwapChainForHwnd`, then calls `MakeWindowAssociation` ([W:hal/dx12/surface.go#L123-L177](https://github.com/gogpu/wgpu/blob/v0.34.2/hal/dx12/surface.go#L123-L177)). Later ones call `ResizeBuffers` ([W:hal/dx12/instance.go#L375-L402](https://github.com/gogpu/wgpu/blob/v0.34.2/hal/dx12/instance.go#L375-L402), [W:hal/dx12/surface.go#L294-L335](https://github.com/gogpu/wgpu/blob/v0.34.2/hal/dx12/surface.go#L294-L335)).
  - gfx configures opaque ([C:gfx/sockets.go#L116-L117](https://github.com/dvoyni/cog/blob/6581d1b0bab1c6b365fee243f0b3eb7c0554bd92/gfx/sockets.go#L116-L117)), so the DirectComposition path for premultiplied alpha does not arise.
  - This task holds the `WindowManager` read lock, which hits the cog block described under Windows / Vulkan. Microsoft's remedy for a pump that can block is "all DXGI interaction … on the same thread as the message pump", so it is **required on main**.
- **PresentFramebuffers:** `Present` or `Present1` ([W:hal/dx12/queue.go#L803-L831](https://github.com/gogpu/wgpu/blob/v0.34.2/hal/dx12/queue.go#L803-L831)). Microsoft names Present as the call that may wait on the pump thread, and says deferred `ResizeBuffers` work lands in it. **Required on main.**
- **AcquireFramebuffers: unclear.** It waits on the frame-latency waitable object (up to 1 s) and calls `GetCurrentBackBufferIndex` ([W:hal/dx12/instance.go#L442-L491](https://github.com/gogpu/wgpu/blob/v0.34.2/hal/dx12/instance.go#L442-L491)).
  - Microsoft's blanket rule ("all DXGI interaction") covers it. No page says it sends a message, and it holds the `WindowManager` read lock.
  - To settle it, on Windows run acquire from a worker while the main thread is parked and not pumping, and see whether it ever blocks. Alternatively, log `InSendMessage()` in the window procedure during the call. If the evidence stays thin, pinning it with Present costs little: acquire and present already run back to back for a frame.
- **ReleaseSurface: unclear.** `Destroy` is `Unconfigure`, which waits for the GPU, then releases the back buffers, the waitable handle and the swapchain ([W:hal/dx12/instance.go#L405-L436, L508-L510](https://github.com/gogpu/wgpu/blob/v0.34.2/hal/dx12/instance.go#L405-L436)).
  - Microsoft warns only against releasing a full-screen swapchain ("may create thread contention", [DXGI overview, Destroying a swap chain](https://learn.microsoft.com/en-us/windows/win32/direct3ddxgi/d3d10-graphics-programming-guide-dxgi#destroying-a-swap-chain)). gfx never goes full screen through DXGI.
  - Whether dropping the last swapchain reference unhooks the `MakeWindowAssociation` window with a sent message is not documented. The same Windows experiment would settle it. Pinning it does no harm, since release is rare.

## Linux / Vulkan on X11

### How the connection is shared

- **It is not shared.** The fork talks to X in pure Go on its own socket, with a reader goroutine ([main-thread-rules, X11](https://github.com/dvoyni/cog/blob/research/main-thread-rules/research/main-thread-rules.md#linux-x11)). For Vulkan it opens a second, parallel Xlib connection: "VK_KHR_xlib_surface expects a real Display* from XOpenDisplay() … We load libX11 dynamically via goffi (no CGO) and open a parallel Xlib connection. The window ID is shared … because X11 window IDs are server-side resources visible to all connections" ([G:internal/platform/x11/platform.go#L93-L103, L243-L290](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/internal/platform/x11/platform.go#L93-L103)).
- `GetHandle` returns that `Display*` and the window ID ([L1617-L1629](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/internal/platform/x11/platform.go#L1617-L1629)). The HAL makes an Xlib surface from them ([W:hal/vulkan/api_linux.go#L29-L70](https://github.com/gogpu/wgpu/blob/v0.34.2/hal/vulkan/api_linux.go#L29-L70)).
- Nothing in the fork reads or pumps that Xlib connection. So no thread pumps the display connection Vulkan uses, and there is no pump for WSI calls to race.

### What Khronos says

- "All commands support being called concurrently from multiple threads, but certain parameters, or components of parameters are defined to be externally synchronized" ([Vulkan spec, Threading Behavior](https://docs.vulkan.org/spec/latest/chapters/fundamentals.html#fundamentals-threadingbehavior)). The rules are per object, not per thread:
  - "Host access to swapchain must be externally synchronized" for acquire ([vkAcquireNextImageKHR](https://registry.khronos.org/vulkan/specs/latest/man/html/vkAcquireNextImageKHR.html)) and destroy ([vkDestroySwapchainKHR](https://registry.khronos.org/vulkan/specs/latest/man/html/vkDestroySwapchainKHR.html)).
  - "Host access to queue must be externally synchronized" for present ([vkQueuePresentKHR](https://registry.khronos.org/vulkan/specs/latest/man/html/vkQueuePresentKHR.html)).
  - "Host access to surface must be externally synchronized" for surface destroy ([vkDestroySurfaceKHR](https://registry.khronos.org/vulkan/specs/latest/man/html/vkDestroySurfaceKHR.html)).
- From the Xlib section ([Vulkan spec, Xlib Platform](https://docs.vulkan.org/spec/latest/chapters/VK_KHR_surface/wsi.html#platformCreateSurface_xlib)):
  - "Some Vulkan functions may send protocol over the specified Xlib Display connection … Applications must therefore ensure the display connection is available to Vulkan for the duration of any functions that manipulate such swapchains".
  - "Avoid holding a server grab on a display connection while waiting for Vulkan operations to complete using a swapchain derived from a different display connection referring to the same X server instance".
  - "Some implementations may require threads to implement some presentation modes so applications must call XInitThreads() before calling any other Xlib functions."
- None of these binds a call to a thread.

### The conditions, checked

- **Server grab:** the fork defines `GrabServer` and `UngrabServer` opcodes but never sends them (only [G:internal/platform/x11/wire.go#L143-L144](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/internal/platform/x11/wire.go#L143-L144) matches).
- **`XInitThreads`:**
  - Mesa does run threads on that connection. Its X11 swapchain takes `XGetXCBConnection(dpy)` ([wsi_common_x11.c#L702](https://gitlab.freedesktop.org/mesa/mesa/-/blob/eead8dcf02fc055f22a993f1e21f4674459efea3/src/vulkan/wsi/wsi_common_x11.c#L702)) and starts its own present and event threads ([L3540-L3552](https://gitlab.freedesktop.org/mesa/mesa/-/blob/eead8dcf02fc055f22a993f1e21f4674459efea3/src/vulkan/wsi/wsi_common_x11.c#L3540-L3552)).
  - The fork never calls `XInitThreads`. It relies on libX11 1.8+ calling it from its library constructor ([main-thread-rules, X11](https://github.com/dvoyni/cog/blob/research/main-thread-rules/research/main-thread-rules.md#linux-x11)). That dependency already exists for gogpu's own render thread and does not change with the thread gfx calls from.
- **External synchronization:** configure, create and release hold the `gfx.Device` write lock. Acquire and present of one window follow each other in frame order.

All five tasks run on **any thread**.

## Linux / Vulkan on Wayland

### How the connection is shared

- **It is shared.** The fork uses libwayland-client through goffi. `GetHandle` hands Vulkan the same `wl_display` and `wl_surface` the platform uses ([G:internal/platform/platform_linux.go#L657-L662](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/internal/platform/platform_linux.go#L657-L662)).
- The platform keeps its objects on a private `appQueue` and dispatches only that queue, from `PollEvents` on the main thread. "The default queue is left exclusively for Mesa Vulkan WSI's internal wl_buffer.release callbacks" ([G:internal/platform/wayland/libwayland_input.go#L168-L220](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/internal/platform/wayland/libwayland_input.go#L168-L220)).
- **Khronos obliges the WSI to stay on its own queue:** "a new wl_event_queue must be created for each successful vkCreateWaylandSurfaceKHR call, and every Wayland object created by the implementation must be assigned to this event queue. If the platform provides Wayland 1.11 or greater, this must be implemented by the use of Wayland proxy object wrappers, to avoid race conditions" ([Vulkan spec, Wayland Platform](https://docs.vulkan.org/spec/latest/chapters/VK_KHR_surface/wsi.html#platformCreateSurface_wayland)).
  - Mesa does exactly that: `wl_display_create_queue_with_name`, then a `wl_proxy_create_wrapper` set to that queue ([wsi_common_wayland.c#L1636-L1651](https://gitlab.freedesktop.org/mesa/mesa/-/blob/eead8dcf02fc055f22a993f1e21f4674459efea3/src/vulkan/wsi/wsi_common_wayland.c#L1636-L1651), [L4061-L4082](https://gitlab.freedesktop.org/mesa/mesa/-/blob/eead8dcf02fc055f22a993f1e21f4674459efea3/src/vulkan/wsi/wsi_common_wayland.c#L4061-L4082)).
- **libwayland has no thread affinity.** Requests are marshalled under the display's mutex, each queue's handlers run on whichever thread dispatches it, and several threads may read through `wl_display_prepare_read_queue` ([main-thread-rules, Wayland](https://github.com/dvoyni/cog/blob/research/main-thread-rules/research/main-thread-rules.md#linux-wayland)).
  - So a WSI call on a worker and the platform's dispatch on main do not race inside libwayland.
  - Mesa's acquire and present read the socket themselves for their own queue. They do not need the main thread to pump.
- **The fork's `displayMu`.** The fork wraps its dispatch, CSD and frame-callback calls in `displayMu`. gogpu's renderer takes it around acquire and present ([G:renderer.go#L170-L185, L650-L656, L884-L891](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/renderer.go#L170-L185)).
  - Its stated reason, "libwayland docs state that wl_display is NOT thread-safe" ([G:internal/platform/wayland/libwayland.go#L89-L94](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/internal/platform/wayland/libwayland.go#L89-L94), [G:internal/platform/platform.go#L361-L378](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/internal/platform/platform.go#L361-L378)), is contradicted by libwayland's source, as the earlier note found. The lock is harmless, and nothing requires it.
  - **One hazard if a backend keeps taking it around present:** on Mesa's legacy FIFO path, `vkQueuePresentKHR` can block without a timeout waiting for a frame callback on a hidden surface ([caller-owned-loops, Wayland FIFO](https://github.com/dvoyni/cog/blob/research/caller-owned-loops/research/caller-owned-loops.md)). Held across that, `displayMu` would also stop the main thread's dispatch. That is a reason to gate present (next section), not to pin it.

### The frame gate

- **Request and gate:**
  - Before present, gogpu calls `SyncFrame`, which sends `wl_surface.frame` under `displayMu` and marks the window "Requested" ([G:internal/platform/platform_linux.go#L845-L863](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/internal/platform/platform_linux.go#L845-L863), [G:internal/platform/wayland/libwayland_frame.go#L131-L203](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/internal/platform/wayland/libwayland_frame.go#L131-L203)).
  - The `done` event lives on `appQueue`, so it is handled on the main thread during dispatch. It stores the state atomically ([L101-L129](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/internal/platform/wayland/libwayland_frame.go#L101-L129)).
  - `FrameCallbackReady` is an atomic load, "Safe to call from any goroutine" ([L236-L243](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/internal/platform/wayland/libwayland_frame.go#L236-L243)).
- **Ordering:**
  - Khronos: "wl_surface.attach, wl_surface.damage, and wl_surface.commit requests must be issued by the implementation during the call to vkQueuePresentKHR". Also: "If the application wishes to synchronize any window changes with a particular frame, such requests must be sent to the Wayland display server prior to calling vkQueuePresentKHR" ([Vulkan spec, Wayland Platform](https://docs.vulkan.org/spec/latest/chapters/VK_KHR_surface/wsi.html#platformCreateSurface_wayland)).
  - The frame request is double-buffered and takes effect on the commit present makes, as the fork's own comment says (L183-L192).
  - So it must happen before present: in PresentFramebuffers, just before `vkQueuePresentKHR`, on whatever thread runs it.
  - The gate check belongs before acquire. A worker can read it, because it is atomic.
- **Ordering, not thread:** requests the main thread sends on the same `wl_surface` between frames take effect on the next commit, the one present makes. This is the same on any thread. Examples are the viewport destination set in `PrepareFrame` ([G:internal/platform/platform_linux.go#L787-L792](https://github.com/dvoyni/gogpu/blob/92d976d655fb366878fa120272e97093bf71cf1b/internal/platform/platform_linux.go#L787-L792)) and `xdg_surface.ack_configure` during dispatch.

All five tasks run on **any thread**.

## What would change the answer

- **macOS:** if gfx's backend took the `NSView` instead of the platform's layer, CreateSurface would create and attach a layer, and it would be required on main. The same goes for any task that writes `contentsScale` or another root-layer property of the hosted view. Turning on `presentsWithTransaction` would move present onto the main thread's live-resize transaction, which pinning alone cannot reach.
- **Windows:** two changes would allow these tasks off main again. One is if ConfigureSurfaces and AcquireFramebuffers stopped holding the `WindowManager` read lock while making the swapchain call. The other is if main pumped messages while parked, for example `MsgWaitForMultipleObjects` in place of the channel wait. Khronos's second option, "make sure their message pump is actively running", would then hold. Until one of those changes, the swapchain calls go on main.
- **Linux:** a Vulkan driver whose WSI used the platform's own X11 connection, or the default Wayland queue, would break the separation the spec and Mesa rely on. No such driver was found.
