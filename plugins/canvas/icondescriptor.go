package canvas

import (
	"fmt"
	"io/fs"

	"github.com/dvoyni/cog/libs/m"
)

// IconDescriptor is a picture text places like a glyph, registered under a
// name with WriteFontLibrary.RegisterIcon.
type IconDescriptor struct {
	// Image is the icon's picture, and Frame the frame of its sheet it
	// draws, so a game's button prompts are one image.
	Image Image
	Frame uint32
	// Scale multiplies the font's cap height, the icon's height, zero
	// meaning 1.
	Scale float32
	// Offset raises the icon's bottom above the baseline, in em, a
	// negative one lowering it below.
	Offset float32
	// Tinted takes its span's tint; otherwise the icon takes the run's alpha
	// alone.
	Tinted bool
}

// registeredIcon is an icon registered under a name: its descriptor; the
// size in texels of its image's frame, read from its file's header as it
// was registered, which a layout sizes it by; and the serial it was
// registered under, never zero, which the layout cache's key holds.
type registeredIcon struct {
	descriptor IconDescriptor
	frameSize  m.Vec2
	serial     uint64
}

// findIconFrameSize answers the size in texels of icon's image's frame, read
// from its file's header through files, the zero Image's, the white
// texel's, one texel: an error for an image the text material cannot draw
// from the sRGB sprite atlas, a render texture's or one of linear colour,
// and for one whose header cannot be read or whose frame does not lie
// within its file.
func findIconFrameSize(files fs.FS, icon IconDescriptor) (m.Vec2, error) {
	if icon.Image == (Image{}) {
		return m.Vec2{X: 1, Y: 1}, nil
	}
	key := imageNames.findKey(icon.Image)
	switch {
	case key.ofTexture:
		return m.Vec2{}, fmt.Errorf("its image is of render texture %v, which the text material cannot draw from the sRGB sprite atlas", key.texture)
	case key.options.ColorSpace == ColorSpaceLinear:
		return m.Vec2{}, fmt.Errorf("its image %q is of linear colour, which the text material cannot draw from the sRGB sprite atlas", key.path)
	}
	config, err := readImageConfig(files, key.path)
	if err != nil {
		return m.Vec2{}, fmt.Errorf("reading the header of its image %q: %w", key.path, err)
	}
	frameRect, withinFile := key.options.findFrameRect(config.Width, config.Height, icon.Frame)
	if !withinFile {
		return m.Vec2{}, fmt.Errorf("frame %d of its image %q does not lie within its file of %d×%d texels", icon.Frame, key.path, config.Width, config.Height)
	}
	return frameRect.Size().Float(), nil
}
