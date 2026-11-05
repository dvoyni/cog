# Platform

Platform is what the operating system provides to a kernel: windows, input and filesystems. It declares them as interfaces that an implementing plugin fills. This glossary records its domain language.

## Language

### Windows and events

**Window**:
One operating-system window, named by a handle that names nothing once the window is closed.

**Main window**:
The window the implementing plugin opens at start, from its configuration.
_Avoid_: Primary window, root window

**Window request**:
What the application wants its windows to be: which exist, and each one's title, size, fullscreen, visibility and cursor request.
_Avoid_: Window config, window descriptor

**Window state**:
What a window currently is: open or not yet, its sizes, scale, focus and effective cursor mode.
_Avoid_: Window info, window status

**Pump**:
The pinned task that reads the platform's pending events and publishes them as kernel events; the application schedules it from the tracks it chooses.
_Avoid_: Event loop, message loop, poll

### Input

**Input state**:
The resource holding input levels as of the last pump run: held keys, which are global, and each window's pointer position and whether the pointer is inside it.
_Avoid_: Input snapshot, keyboard state

**Input event**:
One change of input or of a window, published by the pump in the order the operating system reported it.
_Avoid_: Input message, raw event

**Key**:
Anything that can be held down: a keyboard key, named by its position on a US keyboard whatever the layout, or a mouse button.
_Avoid_: Button, scancode, key code

**Key label**:
The character the current layout prints on a key, unshifted; what a binding or shortcut hint shows, and it changes when the user switches layout.
_Avoid_: Key char, key name

**Text**:
The characters the user typed, after the layout, dead keys and input method, which no single key need match.
_Avoid_: Chars, typed keys

**Window space**:
Coordinates in a window's logical points, independent of the screen's scale; every pointer position is in it.
_Avoid_: Screen space, DIP, logical pixels

**Framebuffer space**:
Coordinates in the pixels of a window's framebuffer: window space times the window's scale.
_Avoid_: Physical pixels, device pixels

**Input source**:
A plugin that produces input for platform: the operating system's through gogpu, or a scripted one.
_Avoid_: Driver, input backend

**Input change**:
One raw change an input source reports, which platform folds into the input state and an input event.
_Avoid_: Raw event, input delta

**Cursor request**:
What the application wants a window's cursor to be: its shape, whether it is visible, its mode, and a pending cursor position; part of the window request.
_Avoid_: Cursor state, cursor settings

**Effective mode**:
The cursor mode actually in force, which differs from the requested one while the window lacks focus or the platform lacks the mode.
_Avoid_: Current mode, actual mode

**Cursor position**:
A point in a window the application moves the operating system's cursor to, which the user did not do and so produces no pointer motion; once applied, the window's pointer position.
_Avoid_: Warp, move cursor, wrap

### Files

**FileSystem**:
The one overlay of every read mount and the writable filesystem, through which all files are read.
_Avoid_: Storage, VFS, disk

**Read mount**:
One read-only layer of the FileSystem, with a priority among the others.
_Avoid_: Mount point, read FS, layer

**Writable filesystem**:
The single persistent layer that writes land in, searched before every read mount.
_Avoid_: Permanent filesystem, permanent FS, save directory

**Values**:
The key-value store kept in a file among the files, read once and written back when it changed; what a game reads before spawning its real kernel, through a throwaway one.
_Avoid_: Settings, preferences, config store
