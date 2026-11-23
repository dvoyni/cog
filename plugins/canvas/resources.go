package canvas

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
)

// Handlers is the resource holding every event handler canvas subscribes, so
// a plugin listed after canvas orders its own around them with
// configurator.OrderSubscriptions.
type Handlers struct {
	// RecordFrame handles render.FrameEvent: it publishes FrameEvent inside
	// it, draws the frame after the nested publication's out dependency, and
	// returns that drawing joined with its in dependency, so render's
	// second pickup of loads and gfx's flush wait for it.
	RecordFrame kernel.Sub[render.FrameEvent]
	// Recover handles render.RecoveredEvent, a fresh start: the atlases,
	// the glyphs packed and the mesh files' buffers, which a lost device
	// left empty, are dropped, and each image, glyph and mesh file is
	// loaded again at its next draw. It runs in a task of its own, in no
	// order with the frames, so a frame drawn after the recovery and before
	// it draws what the lost device left empty, once.
	Recover kernel.Sub[render.RecoveredEvent]
	// TrackOpenedWindow handles platform.WindowOpenedEvent, and
	// ForgetClosedWindow platform.WindowClosedEvent: each keeps canvas's
	// account of the open windows, whose first opened is the main window,
	// the target of a camera naming none.
	TrackOpenedWindow  kernel.Sub[platform.WindowOpenedEvent]
	ForgetClosedWindow kernel.Sub[platform.WindowClosedEvent]
	// DeliverInspection handles render.InspectedEvent: when canvas wrote
	// down the frame render inspected, for a request of its own, it puts
	// render's inspection beside canvas's, and publishes InspectedEvent for
	// it, in a batch of its own.
	DeliverInspection kernel.Sub[render.InspectedEvent]
}

// Targets is the resource of each target canvas has drawn into, its size and
// scale as it was last drawn, which a Camera's conversions read, so a click
// converts through what the player saw: read it under
// kernel.Read[canvas.Targets]. canvas writes it once a frame, after drawing
// the frame, in a task of its own under its write lock, so a reader sees one
// account for its whole run, and a task converting clicks waits only for
// that copy. It holds its reads alone, and a target never drawn has no
// account.
type Targets interface {
	// Window answers window's size in points as it was last drawn, its
	// scale, its framebuffer's pixels to a point, and whether canvas has
	// drawn into it; the zero window answers the main window of the frame
	// last drawn, the target of a camera naming none.
	Window(window platform.Window) (size m.Vec2, scale float32, drawn bool)
	// Texture answers texture's size in pixels as it was last drawn, and
	// whether canvas has drawn into it.
	Texture(texture render.Texture) (size m.Vec2, drawn bool)
}

// FontLibrary is the resource of canvas's font sets, the current language, and the
// icons and colours registered for text: read it under
// kernel.Read[canvas.FontLibrary]. It holds its reads alone, so a task holding its
// read lock cannot make a set; ElevateFontLibrary answers its writing side from a
// write handle.
type FontLibrary interface {
	// Language answers the current language, a tag such as "en", starting
	// from Config.Language.
	Language() string
	// Layout answers text shaped, broken into lines and measured, with no
	// frame, for a UI's sizes, carets and baselines: shaped with HarfBuzz's
	// rules, ordered by Unicode's bidi algorithm, in text's set and
	// language, its own or the current one: each character in the first of
	// the language's fonts that has it, else in the set's default font,
	// its lines measured by the font measuring the language, at text's
	// Size, zero meaning Config.TextSize. A character no font of the set
	// has is drawn as the default font's missing-glyph box, reported once
	// for the set as ReportKindCharacterMissing. It answers from the layout
	// cache DrawText lays out through, so what is measured is what is
	// drawn, and a run measured or drawn frame after frame is shaped once:
	// keyed by text's String, its Font set, its Size, WrapWidth, Align,
	// LineHeight and Tracking, and its language, a layout neither measured
	// nor drawn for Config.LayoutIdleFrames frames freed. A set canvas does
	// not hold is laid out as the default set, reported once for it as
	// ReportKindFontSetNotHeld.
	Layout(text Text) TextLayout
	// Preload makes the distance fields of the glyphs of text, or a bitmap
	// font's coverage, shaped on one line in set's fonts for the current
	// language, ahead of
	// their first draw, behind a loading screen, so the first frame drawing
	// a run of text at any size makes none; it lays nothing out for the
	// cache. A set canvas does not hold preloads the default set's,
	// reported once for it as ReportKindFontSetNotHeld.
	Preload(set FontSet, text string)
	// Metrics answers the measures at size, its em in world units, of the
	// font of set measuring language, an empty language meaning the current
	// one: the language's first font in set, or set's default font, a
	// bitmap font's size rounded to the nearest whole multiple of its pixel
	// size, one at least, as a run's is. A set
	// canvas does not hold, never made or destroyed, is measured as the
	// default set, reported once for it as ReportKindFontSetNotHeld.
	Metrics(set FontSet, size float32, language string) FontMetrics
}

// ElevateFontLibrary answers the FontLibrary resource a job holds the write lock on as
// WriteFontLibrary, making its sets through platform's FS, which the job holds the
// read lock on. Only the write handle gives it, so a job holding the read
// lock cannot make a set.
func ElevateFontLibrary(library kernel.Write[FontLibrary], files kernel.Read[platform.FS]) WriteFontLibrary {
	return fontLibraryWriter{fontLibrary: library.Get().(*fontLibrary), files: files.Get()}
}

// WriteFontLibrary is FontLibrary's writing side, which ElevateFontLibrary answers; it is no
// resource. It is valid only while the job that elevated it runs.
type WriteFontLibrary interface {
	FontLibrary
	// MakeFontSet makes a font set of descriptor, loading every font file
	// it names through platform's FS now, in the calling task, at a start or
	// behind a loading screen, and answers it. A font file that fails to
	// load is reported once for its path as ReportKindFontNotLoaded and left
	// out of the set, a failed default font replaced by canvas's JetBrains
	// Mono.
	MakeFontSet(descriptor FontSetDescriptor) FontSet
	// DestroyFontSet destroys set, which is then measured as the default
	// set. Destroying a set canvas does not hold, or the default set, is
	// reported once for it as ReportKindFontSetNotHeld.
	DestroyFontSet(set FontSet)
	// SetLanguage sets the current language to tag, such as "ja", which
	// picks each set's fonts, its tags matched exactly.
	SetLanguage(tag string)
	// RegisterIcon registers icon under name, for a span of text to place
	// like a glyph, replacing an icon registered under it before everywhere:
	// the layouts holding the name are laid out again. Its image's size is
	// read now from its file's header, through platform's FS, so a layout
	// knows its width before any draw; its image is packed into the sRGB
	// sprite atlas at its first draw, as a sprite's is. An image of a render
	// texture or of linear colour, which the text material cannot draw, or
	// one whose header cannot be read or whose frame does not lie within its
	// file, is refused, reported once for the name as ReportKindIconRefused,
	// and the name left unregistered. An icon whose image is drawn from a
	// texture of its own, too large for an atlas page, draws nothing.
	RegisterIcon(name string, icon IconDescriptor)
	// RegisterColor registers color under name, for text's markup to tint
	// with, `\c:name{text}`, replacing a colour registered under it before.
	// Markup is read each time its run is drawn or laid out, so a run
	// takes the colour registered then, a theme changing it in one place,
	// with no layout made anew; a run naming a colour not registered does
	// not parse, and is drawn as written, reported once for its string.
	RegisterColor(name string, color m.Color)
}
