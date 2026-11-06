package gfx

// PresentMode is how a surface hands its framebuffers to the display,
// numbered as gputypes numbers it.
type PresentMode uint32

const (
	// PresentModeUndefined is the zero mode, which a SurfaceRequest reads as
	// PresentModeFifo.
	PresentModeUndefined PresentMode = 0x0
	// PresentModeFifo waits for the display: the acquire blocks, which paces
	// metronome's vsync preset. Every backend has it.
	PresentModeFifo PresentMode = 0x1
	// PresentModeFifoRelaxed is Fifo that tears when a frame is late; on
	// Metal it turns vsync off.
	PresentModeFifoRelaxed PresentMode = 0x2
	// PresentModeImmediate presents at once and may tear; for metronome's
	// limit and uncapped presets.
	PresentModeImmediate PresentMode = 0x3
	// PresentModeMailbox replaces the queued framebuffer and does not tear;
	// on Metal it is Immediate.
	PresentModeMailbox PresentMode = 0x4
)

// CompositeAlphaMode is how the compositor blends a surface's alpha with
// what lies behind the window, numbered as gputypes numbers it. gfx
// configures every surface Opaque until platform's window request can ask for
// a transparent window.
type CompositeAlphaMode uint32

const (
	CompositeAlphaModeAuto            CompositeAlphaMode = 0x0
	CompositeAlphaModeOpaque          CompositeAlphaMode = 0x1
	CompositeAlphaModePremultiplied   CompositeAlphaMode = 0x2
	CompositeAlphaModeUnpremultiplied CompositeAlphaMode = 0x3
	CompositeAlphaModeInherit         CompositeAlphaMode = 0x4
)

// SurfaceRequest is what the application asks every window's surface to be
// configured with: Config.Surface at start, ExclusiveDevice.RequestSurface
// after. A part the surface cannot honour falls back, with a LogErr, and
// Device.SurfaceConfiguration answers what each window got.
type SurfaceRequest struct {
	// Format is the framebuffer's format; zero is TextureFormatBGRA8Unorm,
	// which every backend accepts, leaving sRGB encoding to the last pass.
	// TextureFormatBGRA8UnormSrgb is honoured on Vulkan and Metal.
	Format TextureFormat
	// PresentMode is the present mode; zero is PresentModeFifo, the fallback
	// when the surface lacks the one asked for.
	PresentMode PresentMode
	// Capturable lets Device.RequestCapture copy the framebuffer out: CopySrc
	// on Vulkan, StorageBinding on Metal, which costs it framebufferOnly. DX12
	// cannot honour it yet. Off by default; development and agent builds set
	// it.
	Capturable bool
}

// SurfaceConfiguration is what one window's surface is configured with.
type SurfaceConfiguration struct {
	Format        TextureFormat
	PresentMode   PresentMode
	Width, Height uint32
}

// SurfaceCapabilities is what one window's surface can be configured with,
// as the backend reports it.
type SurfaceCapabilities struct {
	Formats      []TextureFormat
	PresentModes []PresentMode
	AlphaModes   []CompositeAlphaMode
}

// AcquireStatus is what acquiring a window's framebuffer answered, mapped by
// the backend from its own errors so gfx handles every backend alike.
type AcquireStatus uint8

const (
	// AcquireStatusAcquired is a framebuffer to draw.
	AcquireStatusAcquired AcquireStatus = iota
	// AcquireStatusSuboptimal is a framebuffer to draw, with the surface to
	// be configured again before the next frame.
	AcquireStatusSuboptimal
	// AcquireStatusOutdated is no framebuffer: the window draws nothing this
	// frame, and its surface is configured again before the next.
	AcquireStatusOutdated
	// AcquireStatusLost is no framebuffer: the surface is recreated from the
	// window's native handle and configured again.
	AcquireStatusLost
	// AcquireStatusTimeout is no framebuffer this frame, and nothing else.
	AcquireStatusTimeout
)
