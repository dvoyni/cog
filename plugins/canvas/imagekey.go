package canvas

import (
	"strings"

	"github.com/dvoyni/cog/libs/m"
)

// isUnder answers whether path is one Unload(unloaded) unloads: unloaded
// itself, or, when unloaded ends in a slash, a path in the folder it names,
// so Unload("level1/") unloads "level1/hero.png" and not "level10/hero.png".
func isUnder(path, unloaded string) bool {
	return path == unloaded || strings.HasSuffix(unloaded, "/") && strings.HasPrefix(path, unloaded)
}

// imageKey is what ImageFromFile interns: the image's path, its options, and
// whether ImageFromFile was given more sets of options, which it ignored.
type imageKey struct {
	path           string
	options        ImageOptions
	ignoredOptions bool
}

// packedKey is what canvas packs as a region of its own: the texels of a
// rectangle of a file, with its gutter wrapped along the axes wrap names,
// which every frame and image of them shares.
type packedKey struct {
	source sourceKey
	rect   m.Recti
	wrap   Wrap
}
