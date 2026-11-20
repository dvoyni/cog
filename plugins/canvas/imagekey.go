package canvas

import (
	"fmt"
	"strings"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/render"
)

// isUnder answers whether path is one Unload(unloaded) unloads: unloaded
// itself, or, when unloaded ends in a slash, a path in the folder it names,
// so Unload("level1/") unloads "level1/hero.png" and not "level10/hero.png".
func isUnder(path, unloaded string) bool {
	return path == unloaded || strings.HasSuffix(unloaded, "/") && strings.HasPrefix(path, unloaded)
}

// imageKey is what ImageFromFile and ImageFromTexture intern: the image's
// path, or, for an image of a render texture, which ofTexture marks, that
// texture, the zero one among them, and no path; its options; and whether
// more sets of options were given, which were ignored.
type imageKey struct {
	path           string
	texture        render.Texture
	ofTexture      bool
	options        ImageOptions
	ignoredOptions bool
}

// findSubject answers what canvas reports k's image under, once: its path,
// or its texture.
func (k imageKey) findSubject() string {
	if k.ofTexture {
		return fmt.Sprintf("texture %v", k.texture)
	}
	return k.path
}

// describe answers k's image as a report names it: its quoted path, or its
// texture.
func (k imageKey) describe() string {
	if k.ofTexture {
		return fmt.Sprintf("of texture %v", k.texture)
	}
	return fmt.Sprintf("%q", k.path)
}

// reportIgnoredOptions reports to logger, once for the subject of key, an
// image's, the options ImageFromFile or ImageFromTexture was given for it
// and ignored.
func reportIgnoredOptions(logger *kernel.Logger, key imageKey) {
	if !key.ignoredOptions {
		return
	}
	ignored := Report{Kind: ReportKindImageOptionsIgnored, Err: fmt.Errorf("canvas: image %s was given more than one set of options, and is drawn with the first", key.describe())}
	logger.LogErrOnce(imageOptionsIgnoredLogKey+key.findSubject(), ignored)
}

// packedKey is what canvas packs as a region of its own: the texels of a
// rectangle of a file, with its gutter wrapped along the axes wrap names,
// which every frame and image of them shares.
type packedKey struct {
	source sourceKey
	rect   m.Recti
	wrap   Wrap
}
