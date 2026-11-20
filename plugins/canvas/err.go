package canvas

import "errors"

// ReportKind names what kind of mistake a Report is, for a test to name the
// reports it expects.
type ReportKind uint8

const (
	// ReportKindRecordedOutsideFrame is an entry recorded or a camera set
	// while no frame is recording: by a task on another beat, or by one
	// hung off render.FrameEvent that runs once canvas's drawing has begun.
	// It is reported once for each task.
	ReportKindRecordedOutsideFrame ReportKind = iota + 1
	// ReportKindCameraTargetRefused is a camera naming both a window and a
	// texture, or a texture render does not hold, which is skipped. It is
	// reported once for each target.
	ReportKindCameraTargetRefused
	// ReportKindPixelPerfectStretch is a pixel-perfect camera's FitStretch,
	// whose pixels would not be square, which is drawn as FitInscribe. It
	// is reported once.
	ReportKindPixelPerfectStretch
	// ReportKindImageOptionsIgnored is an image ImageFromFile or
	// ImageFromTexture was given more than one set of options for, drawn
	// with the first. It is reported once for each path or texture, at the
	// image's first draw, and again after the path is unloaded.
	ReportKindImageOptionsIgnored
	// ReportKindAtlasFull is a sprite atlas at its ceiling of pages, every
	// image it holds drawn this frame, so an image that fits on none of its
	// pages is drawn from a texture of its own. It is reported once for
	// each atlas.
	ReportKindAtlasFull
	// ReportKindImageHeaderUnread is an image whose file's header
	// Assets.ImageSize could not read, which it answered false. It is
	// reported once for each path, until the path is unloaded.
	ReportKindImageHeaderUnread
	// ReportKindAtlasPageSizeRaised is a Config.AtlasPageSize that is no
	// multiple of the atlas grid Config.MipLevels sets holding the white texel's
	// cell, one texel rounded up to the atlas grid and its gutter, so pages are
	// made the next side that is instead. It is reported once, as canvas
	// starts.
	ReportKindAtlasPageSizeRaised
	// ReportKindMipLevelsLowered is a Config.MipLevels of more than 12,
	// whose smallest page would pass WebGPU's default limit of 8192 texels
	// on a texture's side, so pages are made of 12 levels instead. It is
	// reported once, as canvas starts.
	ReportKindMipLevelsLowered
	// ReportKindFrameOutOfBounds is an image whose ImageOptions.SourceRect
	// reaches past its file or texture, or holds no texel of a frame, or a
	// sprite's Frame past its image's sheet's last, which draws nothing. It
	// is reported once for each path or texture, and again after the path
	// is unloaded.
	ReportKindFrameOutOfBounds
	// ReportKindImageTextureRefused is an image ImageFromTexture made of a
	// texture render does not hold at the image's first draw, the zero
	// Texture among them, which draws nothing. It is reported once for each
	// texture.
	ReportKindImageTextureRefused
	// ReportKindConverterRefused is an image given a Converter that is not
	// comparable, or an image of a render texture given one, which is
	// drawn unconverted. It is reported once for each path or texture, at
	// the image's first draw, and again after the path is unloaded.
	ReportKindConverterRefused
	// ReportKindMaterialNotHeld is a Material canvas does not hold, never
	// made by Assets.MakeMaterial or released, that an entry is drawn with,
	// whose entry is skipped, or that is released or has a parameter set;
	// or the default material, the zero Material, released or given a
	// parameter, which are canvas's. It is reported once for each material.
	ReportKindMaterialNotHeld
)

// The keys canvas logs a report once under are one of these, then the
// report's subject: an image's path or texture, a sprite atlas's label, or a
// material.
const (
	imageOptionsIgnoredLogKey = "canvas: image options ignored: "
	imageHeaderUnreadLogKey   = "canvas: image header unread: "
	atlasFullLogKey           = "canvas: sprite atlas full: "
	frameOutOfBoundsLogKey    = "canvas: frame out of bounds: "
	imageTextureRefusedLogKey = "canvas: image texture refused: "
	converterRefusedLogKey    = "canvas: converter refused: "
	materialNotHeldLogKey     = "canvas: material not held: "
)

// pixelPerfectStretchReport is the report of a pixel-perfect camera's
// FitStretch, made once, so that a camera drawing it each frame allocates
// nothing.
var pixelPerfectStretchReport error = Report{Kind: ReportKindPixelPerfectStretch, Err: errors.New("canvas: a pixel-perfect camera's FitStretch, whose pixels would not be square, is drawn as FitInscribe")}

// Report is a mistake canvas found at run time and carried past, logged
// through the kernel's LogErrOnce, so each is logged once for its subject,
// and typed, so a test can tell which it is.
type Report struct {
	Kind ReportKind
	Err  error
}

// Error answers Err's message.
func (r Report) Error() string { return r.Err.Error() }

// Unwrap answers Err.
func (r Report) Unwrap() error { return r.Err }
