# What must run on the main thread on each platform

Research for [cog#645](https://github.com/dvoyni/cog/issues/645), on the map [cog#610](https://github.com/dvoyni/cog/issues/610). Gathered 2026-11-05.

The question: for each desktop platform (Windows, macOS, Linux X11 and Wayland), which `system` operations must run on the main thread, and so in a pinned kernel task (`PinToMainThread`, `kernel/runner.go`)? The operations are creating and closing windows, title, size, fullscreen, DPI queries, cursor show/hide/lock/shape, IME, and reading or pumping events. Which ones are safe from any goroutine?

This builds on [cog#613](https://github.com/dvoyni/cog/issues/613), which covers gogpu's loop and its main and render threads, and [cog#614](https://github.com/dvoyni/cog/issues/614), which covers modal loops, macOS pumping, Wayland FIFO and the present thread. Neither is repeated here.

## Sources and versions

| Source | Version |
|---|---|
| gogpu fork | `dvoyni/gogpu` [`9de6298`](https://github.com/dvoyni/gogpu/tree/9de62984b6c1dceb45abd7a56efd3538732a9d1f) (the `submodules/gogpu` pin) |
| Apple | [Threading Programming Guide: Thread Safety Summary](https://developer.apple.com/library/archive/documentation/Cocoa/Conceptual/Multithreading/ThreadSafetySummary/ThreadSafetySummary.html) (archived); current API reference ([NSWindow](https://developer.apple.com/documentation/appkit/nswindow), [NSView](https://developer.apple.com/documentation/appkit/nsview)); [Diagnosing memory, thread, and crash issues early](https://developer.apple.com/documentation/xcode/diagnosing-memory-thread-and-crash-issues-early) |
| Microsoft | Win32 docs on learn.microsoft.com (linked inline; sources `MicrosoftDocs/sdk-api@656e965`, `MicrosoftDocs/win32@51c9b7c`) |
| libwayland | [`f2ecaa6`](https://gitlab.freedesktop.org/wayland/wayland/-/tree/f2ecaa61a3a82944ae069de5413104ab7ba2109f) |
| libX11 | [`4c8ed23`](https://gitlab.freedesktop.org/xorg/lib/libx11/-/tree/4c8ed231178d218915f9749f593b3faeac7d27b6) |

`G:` below means `https://github.com/dvoyni/gogpu/blob/9de62984b6c1dceb45abd7a56efd3538732a9d1f/`.

## Answer in short

| Operation | Windows | macOS | X11 | Wayland |
|---|---|---|---|---|
| Create window | owner thread | main thread | any goroutine (OS); gogpu: pump thread | any goroutine (OS); gogpu: pump thread |
| Close / destroy | `Close` (posts `WM_CLOSE`): any goroutine; `DestroyWindow`: owner thread | main thread | any goroutine (OS); gogpu: pump thread | pump thread |
| Title, size, position, fullscreen | owner thread in practice (a cross-thread call is a synchronous send that blocks until the owner pumps) | main thread | any goroutine (OS); gogpu: pump thread | any goroutine (OS, requests are locked in libwayland); gogpu: pump thread |
| DPI / scale query | any thread (OS) | main thread | gogpu reads cached state | gogpu reads state set by events |
| Cursor shape | owner thread | main thread | any goroutine (OS) | pump thread (needs the enter serial) |
| Cursor show / hide / lock | owner thread (`ShowCursor` counter and capture are per thread) | main thread; gogpu has no cursor lock on macOS | any goroutine (OS) | pump thread |
| IME | owner thread (input context is per thread) | main thread | needs Xlib/XIM, which gogpu lacks | needs `text-input-v3`, which gogpu lacks; pump thread |
| Read / pump events | owner thread only | main thread only | one goroutine at a time | one thread per queue |
| Wake the pump | any goroutine | any goroutine | any goroutine | any goroutine |

- **Windows has no "main thread" rule. It has an owner-thread rule:** a window belongs to the thread that created it. Only that thread can pump its messages, destroy it, capture the mouse for it, focus it, or change its cursor-show count and IME context.
- **macOS is the only platform with a true main-thread rule.** In today's SDK, `NSWindow` and `NSView` are `@MainActor`, and Apple says most AppKit UI APIs must run on the main thread. Every window, cursor, IME and event call in gogpu is a direct AppKit call with no dispatch, so each one has to be made on the main thread.
- **X11 and Wayland have no thread affinity in the OS libraries.** gogpu's pure-Go X11 client locks its writes. libwayland locks `wl_display` internally and supports reading from several threads. However, gogpu's Go-side window state is not locked consistently, and event handlers run on whichever thread pumps. So under gogpu they are pump-thread operations as well.
- **The only operations gogpu documents as safe from any goroutine** are `App.Quit`, `App.RequestRedraw`, `App.TrackResource`/`UntrackResource` and the platform `WakeUp`. `HostLoop` requires every call to be made on the thread that called `Start`, which must be the OS main thread ([G:hostloop.go#L26-L29](https://github.com/dvoyni/gogpu/blob/9de62984b6c1dceb45abd7a56efd3538732a9d1f/hostloop.go#L26-L29)).
- **For cog, then:** every `system` operation that touches a window, the cursor, IME or events goes in a task pinned to the main thread, on all four platforms. Only one platform (macOS) strictly needs it, but Windows needs the same thread for a different reason, and gogpu does not make the Linux backends goroutine-safe. Waking the loop, quitting and requesting a redraw are safe from any goroutine. So is plain file I/O, which has no thread affinity. Native file dialogs are windows and do not count as file I/O.
- **IME is not implemented in gogpu on any platform.** Searching for `NSTextInputClient`, `setMarkedText`, `Imm*`, `WM_IME_*`, `text_input_v3`, `XOpenIM` and preedit finds nothing under `internal/platform`. Adding it means using a per-thread (Windows) or main-thread (macOS) API, so it lands on the pinned side.

## Windows

### What the OS requires

- **Ownership:** "Any thread can create a window. The thread that creates the window owns the window and its associated message queue. Therefore, the thread must provide a message loop to process the messages in its message queue" ([Creating Windows in Threads](https://learn.microsoft.com/en-us/windows/win32/procthread/creating-windows-in-threads)).
- **Input routing:** the system posts input "to the message queue of the thread that created the destination window. A thread's message queue receives all mouse and keyboard messages for the windows created by the thread" ([About Messages and Message Queues](https://learn.microsoft.com/en-us/windows/win32/winmsg/about-messages-and-message-queues)).
- **Pumping:** `PeekMessage`'s `hWnd` "must belong to the current thread. If hWnd is NULL, PeekMessage retrieves messages for any window that belongs to the current thread" ([PeekMessageW](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-peekmessagew)). Pumping from another thread does not see the window's messages at all.
- **Destroying:** "A thread cannot use DestroyWindow to destroy a window created by a different thread" ([DestroyWindow](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-destroywindow)).
- **Posting is any-thread.** `PostMessage` "places (posts) a message in the message queue associated with the thread that created the specified window and returns without waiting" ([PostMessageW](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-postmessagew)).
- **Title from another thread blocks.** `SetWindowText` "causes a WM_SETTEXT message to be sent" for a window in the same process ([SetWindowTextW](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-setwindowtextw)). A sending thread "cannot continue executing until the window procedure that receives the message returns" ([Message Deadlocks](https://learn.microsoft.com/en-us/windows/win32/winmsg/about-messages-and-message-queues#message-deadlocks)). If the owner thread is waiting on the caller, for example a kernel main thread waiting on a worker job, this deadlocks.
- **Size and position from another thread are asynchronous.** For `SetWindowPos`, "If the calling thread and the thread that owns the window are attached to different input queues, the system posts the request to the thread that owns the window" ([SetWindowPos](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-setwindowpos)). It does not block, but the change only happens when the owner pumps, and the caller gets no result.
- **Per-thread input state:**
  - `SetCapture` works on "the specified window belonging to the current thread" ([SetCapture](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-setcapture)).
  - `SetFocus` requires a window "attached to the calling thread's message queue" ([SetFocus](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-setfocus)).
  - `GetKeyState` "changes as a thread reads key messages from its message queue" ([GetKeyState](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-getkeystate)).
  - Raymond Chen (Microsoft): "the cursor show counter became a thread-local state … when a thread calls ShowCursor it affects the cursor show state only for windows that belong to that thread" ([What was the ShowCursor function intended to be used for?](https://devblogs.microsoft.com/oldnewthing/20091217-00/?p=15643)).
- **Cursor shape:** "The cursor is a shared resource. A window should set the cursor shape only when the cursor is in its client area or when the window is capturing mouse input" ([SetCursor](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-setcursor)). In practice that means doing it in `WM_SETCURSOR`, on the owner thread.
- **Global, any-thread calls:** `ClipCursor` and `SetCursorPos` affect the whole window station and state no thread rule ([ClipCursor](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-clipcursor), [SetCursorPos](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-setcursorpos)). `GetDpiForWindow` also states no thread rule ([GetDpiForWindow](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-getdpiforwindow)).
- **IME:** "By default, the operating system creates and assigns an input context to each thread. Within the thread, this default input context is a shared resource" ([Input Context](https://learn.microsoft.com/en-us/windows/win32/intl/input-context)).
- **Modal loops ([cog#614](https://github.com/dvoyni/cog/issues/614)):** during a move or resize, the owner thread is inside `DefWindowProc`. Work queued for it waits until the user lets go, unless it runs from the modal timer.

### What gogpu does

- `Run` locks the OS thread ([G:app.go#L380](https://github.com/dvoyni/gogpu/blob/9de62984b6c1dceb45abd7a56efd3538732a9d1f/app.go#L380)) and creates the windows on it, so the owner thread is the main thread.
- `PollEvents` calls `PeekMessageW` with a NULL HWND, "all windows on this thread" ([G:internal/platform/platform_windows.go#L1230-L1240](https://github.com/dvoyni/gogpu/blob/9de62984b6c1dceb45abd7a56efd3538732a9d1f/internal/platform/platform_windows.go#L1230-L1240)).
- Calls that are safe from any goroutine:
  - `WakeUp` posts `wmWakeUp` ([L1362-L1364](https://github.com/dvoyni/gogpu/blob/9de62984b6c1dceb45abd7a56efd3538732a9d1f/internal/platform/platform_windows.go#L1362-L1364)).
  - `Close` posts `WM_CLOSE` ([L1135-L1137](https://github.com/dvoyni/gogpu/blob/9de62984b6c1dceb45abd7a56efd3538732a9d1f/internal/platform/platform_windows.go#L1135-L1137)).
- Calls that must run on the owner thread:
  - `Destroy` calls `DestroyWindow` ([L1195-L1205](https://github.com/dvoyni/gogpu/blob/9de62984b6c1dceb45abd7a56efd3538732a9d1f/internal/platform/platform_windows.go#L1195-L1205)).
  - `SetTitle` calls `SetWindowTextW` directly ([L934-L939](https://github.com/dvoyni/gogpu/blob/9de62984b6c1dceb45abd7a56efd3538732a9d1f/internal/platform/platform_windows.go#L934-L939)).
  - `SetFullscreen` calls `SetWindowLongPtrW` and `SetWindowPos` ([L1011-L1049](https://github.com/dvoyni/gogpu/blob/9de62984b6c1dceb45abd7a56efd3538732a9d1f/internal/platform/platform_windows.go#L1011-L1049)), and it also writes Go fields that are not locked.
  - `setCursorMode` pairs global `ClipCursor`/`SetCursorPos` with per-thread `ShowCursor` ([L1683-L1764](https://github.com/dvoyni/gogpu/blob/9de62984b6c1dceb45abd7a56efd3538732a9d1f/internal/platform/platform_windows.go#L1683-L1764)), so it has to run on the owner thread.
  - The drag source says it "MUST be called from the HWND owner thread (main thread)" ([G:internal/platform/drag_windows.go#L88-L93](https://github.com/dvoyni/gogpu/blob/9de62984b6c1dceb45abd7a56efd3538732a9d1f/internal/platform/drag_windows.go#L88-L93)).

## macOS

### What the OS requires

- **Today's API reference:** `NSWindow` and `NSView` are declared `@MainActor class` ([NSWindow](https://developer.apple.com/documentation/appkit/nswindow), [NSView](https://developer.apple.com/documentation/appkit/nsview)). Their members (`title`, `toggleFullScreen(_:)`, `backingScaleFactor`, `setContentSize`) inherit that isolation.
- **Apple's Xcode docs:** "Some system frameworks contain APIs that you must only call from your app's main thread. This requirement applies to most of the AppKit and UIKit user interface APIs … Failure to perform these operations on the main thread may result in visual defects, data corruption, or crashes" ([Diagnosing memory, thread, and crash issues early](https://developer.apple.com/documentation/xcode/diagnosing-memory-thread-and-crash-issues-early)).
- **Events:** "The main thread of the application is responsible for handling events. The main thread is the one blocked in the run method of NSApplication … While the Application Kit continues to work if other threads are involved in the event path, operations can occur out of sequence." `postEvent:atStart:` may be called "from a secondary thread to post an event to the main thread's event queue" ([Thread Safety Summary: Event Handling Restrictions](https://developer.apple.com/library/archive/documentation/Cocoa/Conceptual/Multithreading/ThreadSafetySummary/ThreadSafetySummary.html)).
- **Views:** "You should create, destroy, resize, move, and perform other operations on NSView objects only from the main thread" ([same page, NSView Restrictions](https://developer.apple.com/library/archive/documentation/Cocoa/Conceptual/Multithreading/ThreadSafetySummary/ThreadSafetySummary.html)). A window's content view is an `NSView`, so window creation and resizing fall under this rule.
- **Window creation:** the archived guide still says "You can create a window on a secondary thread". That is out of date. Modern AppKit raises "NSWindow should only be instantiated on the main thread!", and Apple DTS answers that report by moving the call to the main thread ([Apple Developer Forums thread 820622](https://developer.apple.com/forums/thread/820622)). The `@MainActor` declaration above is the current contract.
- **Not stated:** Apple does not document a thread rule for `NSCursor` (`set`, `hide`, `push`) or for Quartz's `CGAssociateMouseAndMouseCursorPosition` (used for cursor lock). Cursor changes go through window cursor rects and the event path, so treat them as main thread like the rest of AppKit UI. `NSTextInputClient` callbacks (IME) are made by AppKit from `interpretKeyEvents:` while handling an event, so they also run on the main thread.

### What gogpu does

- The main goroutine is pinned at init ([G:internal/platform/darwin/init.go#L28-L32](https://github.com/dvoyni/gogpu/blob/9de62984b6c1dceb45abd7a56efd3538732a9d1f/internal/platform/darwin/init.go#L28-L32)).
- gogpu has `PerformOnMain`, which wraps `performSelectorOnMainThread:withObject:waitUntilDone:` ([G:internal/platform/darwin/mainthread.go#L96-L139](https://github.com/dvoyni/gogpu/blob/9de62984b6c1dceb45abd7a56efd3538732a9d1f/internal/platform/darwin/mainthread.go#L96-L140)), and `IsMainThread`. Only printing uses them, and it refuses a call made off the main thread ([G:internal/platform/darwin/print.go#L146-L149](https://github.com/dvoyni/gogpu/blob/9de62984b6c1dceb45abd7a56efd3538732a9d1f/internal/platform/darwin/print.go#L146-L149)).
- Every window operation calls AppKit directly on the caller's thread, with only a Go mutex around it:
  - `SetTitle` ([G:internal/platform/darwin/window.go#L242](https://github.com/dvoyni/gogpu/blob/9de62984b6c1dceb45abd7a56efd3538732a9d1f/internal/platform/darwin/window.go#L242))
  - `BackingScaleFactor` ([L449](https://github.com/dvoyni/gogpu/blob/9de62984b6c1dceb45abd7a56efd3538732a9d1f/internal/platform/darwin/window.go#L449))
  - `ToggleFullScreen` ([L903](https://github.com/dvoyni/gogpu/blob/9de62984b6c1dceb45abd7a56efd3538732a9d1f/internal/platform/darwin/window.go#L903))
  - `NSCursor` selection ([G:internal/platform/platform_darwin.go#L1008](https://github.com/dvoyni/gogpu/blob/9de62984b6c1dceb45abd7a56efd3538732a9d1f/internal/platform/platform_darwin.go#L1008))

  None of these may be called from a worker goroutine.
- `WakeUp` calls `PostEmptyEvent`, the documented any-thread `postEvent:atStart:` ([G:internal/platform/platform_darwin.go#L292-L296](https://github.com/dvoyni/gogpu/blob/9de62984b6c1dceb45abd7a56efd3538732a9d1f/internal/platform/platform_darwin.go#L292-L296)).
- **Gap:** `SetCursorMode` is a no-op on macOS, and `CursorMode` always returns 0 ([G:internal/platform/platform_darwin.go#L472-L473](https://github.com/dvoyni/gogpu/blob/9de62984b6c1dceb45abd7a56efd3538732a9d1f/internal/platform/platform_darwin.go#L472-L473)). Cursor lock and confine do not exist on macOS today.

## Linux X11

### What the OS requires

- The X protocol has no thread affinity. Windows are server resources named by ID, and any connection, or any thread on one, may send requests for them.
- **Xlib:** "XInitThreads … must be the first Xlib function a multi-threaded program calls … It is only necessary to call this function if multiple threads might use Xlib concurrently" ([XInitThreads.man](https://gitlab.freedesktop.org/xorg/lib/libx11/-/blob/4c8ed231178d218915f9749f593b3faeac7d27b6/man/XInitThreads.man)). Current libX11 calls `XInitThreads()` from its library constructor unless it is built with `--disable-thread-safety-constructor` ([configure.ac#L144-L151](https://gitlab.freedesktop.org/xorg/lib/libx11/-/blob/4c8ed231178d218915f9749f593b3faeac7d27b6/configure.ac#L144-L151)).
- **XIM (IME)** is an Xlib API, and gogpu does not use Xlib for windowing (see below).

### What gogpu does

- gogpu speaks the X11 wire protocol in pure Go ([G:internal/platform/x11/doc.go](https://github.com/dvoyni/gogpu/blob/9de62984b6c1dceb45abd7a56efd3538732a9d1f/internal/platform/x11/doc.go)). Its `Connection` serialises writes and replies with `writeMu` and `replyMu`, and reads in its own `readerLoop` goroutine ([G:internal/platform/x11/connection.go#L90-L93](https://github.com/dvoyni/gogpu/blob/9de62984b6c1dceb45abd7a56efd3538732a9d1f/internal/platform/x11/connection.go#L90-L93), [L170](https://github.com/dvoyni/gogpu/blob/9de62984b6c1dceb45abd7a56efd3538732a9d1f/internal/platform/x11/connection.go#L170)). A request (title, size, fullscreen, cursor) is therefore safe on the wire from any goroutine.
- A parallel Xlib `Display*` exists only for `VK_KHR_xlib_surface` ([G:internal/platform/x11/platform.go#L91-L99](https://github.com/dvoyni/gogpu/blob/9de62984b6c1dceb45abd7a56efd3538732a9d1f/internal/platform/x11/platform.go#L91-L99)). gogpu never calls `XInitThreads` and relies on libX11's constructor.
- **The Go-side state is not consistently locked.**
  - `SetCursor` and `SetCursorMode` take `p.mu` ([L2444](https://github.com/dvoyni/gogpu/blob/9de62984b6c1dceb45abd7a56efd3538732a9d1f/internal/platform/x11/platform.go#L2444), [L2562](https://github.com/dvoyni/gogpu/blob/9de62984b6c1dceb45abd7a56efd3538732a9d1f/internal/platform/x11/platform.go#L2562)).
  - `SetFullscreen` writes `w.fullscreen` without a lock ([L2404-L2411](https://github.com/dvoyni/gogpu/blob/9de62984b6c1dceb45abd7a56efd3538732a9d1f/internal/platform/x11/platform.go#L2404-L2411)).
  - The `App` setters write `a.config` without a lock ([G:app.go#L245-L250](https://github.com/dvoyni/gogpu/blob/9de62984b6c1dceb45abd7a56efd3538732a9d1f/app.go#L245-L250)).

  So only one goroutine at a time should drive the platform. The pump thread is the natural choice, because event handling mutates the same state.
- `WakeUp` is a non-blocking channel send that is safe from any goroutine ([G:internal/platform/platform_linux.go#L432-L439](https://github.com/dvoyni/gogpu/blob/9de62984b6c1dceb45abd7a56efd3538732a9d1f/internal/platform/platform_linux.go#L432-L439)).

## Linux Wayland

### What the OS requires

- libwayland has no main thread. `wl_display` keeps a mutex, and request marshalling takes it ([wayland-client.c#L906-L916](https://gitlab.freedesktop.org/wayland/wayland/-/blob/f2ecaa61a3a82944ae069de5413104ab7ba2109f/src/wayland-client.c#L906-L916)), so requests can be sent from any thread.
- Event queues "allow the events on a display to be handled in a thread-safe manner" ([wayland-client-core.h#L114-L121](https://gitlab.freedesktop.org/wayland/wayland/-/blob/f2ecaa61a3a82944ae069de5413104ab7ba2109f/src/wayland-client-core.h#L114-L121)). "A client can ensure that a certain assumption, such as holding a lock or running from a given thread, is true when a proxy event handler is called by assigning that proxy to an event queue and making sure that this queue is only dispatched when the assumption holds" ([L80-L88](https://gitlab.freedesktop.org/wayland/wayland/-/blob/f2ecaa61a3a82944ae069de5413104ab7ba2109f/src/wayland-client-core.h#L80-L88)). In other words, handlers run on whichever thread dispatches their queue.
- Several threads may read: `wl_display_prepare_read_queue` "only registers that the thread calling this function has intention to read … When all registered readers call wl_display_read_events(), only one (at random) eventually reads … This way we avoid races and still can read from more threads" ([wayland-client.c#L1928-L1977](https://gitlab.freedesktop.org/wayland/wayland/-/blob/f2ecaa61a3a82944ae069de5413104ab7ba2109f/src/wayland-client.c#L1928-L1977)).
- Protocol rule: requests such as `wl_pointer.set_cursor`, `wp_cursor_shape_device_v1.set_shape` and pointer-constraint activation need the serial of the latest `enter` event. That serial comes from dispatch, so these requests belong with the dispatching thread.

### What gogpu does

- All app objects live on a private `appQueue`, which is dispatched from `PollEvents` under `displayMu` ([G:internal/platform/wayland/libwayland_input.go#L170-L215](https://github.com/dvoyni/gogpu/blob/9de62984b6c1dceb45abd7a56efd3538732a9d1f/internal/platform/wayland/libwayland_input.go#L170-L215); [G:internal/platform/platform_linux.go#L3009-L3090](https://github.com/dvoyni/gogpu/blob/9de62984b6c1dceb45abd7a56efd3538732a9d1f/internal/platform/platform_linux.go#L3009-L3090)). Its handlers (configure, input, scale) therefore run on the pump thread and write Go state there.
- `SetTitle`, `SetMinSize` and `SetFullscreen` marshal on `xdg_toplevel` without `displayMu` ([G:internal/platform/wayland/libwayland_xdg.go#L551-L599](https://github.com/dvoyni/gogpu/blob/9de62984b6c1dceb45abd7a56efd3538732a9d1f/internal/platform/wayland/libwayland_xdg.go#L551-L599)). This is legal for libwayland, but it is not coordinated with the pump's Go state.
- The comment on `displayMu` says "libwayland docs state that wl_display is NOT thread-safe" ([G:internal/platform/wayland/libwayland.go#L89-L94](https://github.com/dvoyni/gogpu/blob/9de62984b6c1dceb45abd7a56efd3538732a9d1f/internal/platform/wayland/libwayland.go#L89-L94)). The libwayland source above contradicts it. The lock is harmless, but its stated reason is wrong.
- Each secondary window opens its own `wl_display` connection ([G:internal/platform/platform_linux.go#L109-L117](https://github.com/dvoyni/gogpu/blob/9de62984b6c1dceb45abd7a56efd3538732a9d1f/internal/platform/platform_linux.go#L127-L135)), and the same `PollEvents` dispatches it.
- DPI is not a query on Wayland. The compositor pushes the scale through events (`preferred_buffer_scale` or `wp_fractional_scale_v1`), and gogpu reads the cached value under `scaleMu`.

## Not window-bound

- **Plain file I/O** through Go's `os` package has no thread affinity on any platform. Filesystem operations can run in ordinary, unpinned tasks.
- **Native file dialogs** are windows: `NSOpenPanel` and `NSSavePanel` are `NSWindow` subclasses, and on Windows the dialog is owned by the HWND's thread. They follow the window rules above, on the main thread.
- **Present thread:** see [cog#614](https://github.com/dvoyni/cog/issues/614). That ticket settled acquire and present on the main-thread tick as the portable choice.
