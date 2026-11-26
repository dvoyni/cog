package font

import (
	"embed"
	"io/fs"
	"sync"

	gotext "github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/font/opentype"
)

// builtInFontPath is the path among the package's embedded files of its
// built-in font, JetBrains Mono.
const builtInFontPath = "jetbrainsmono.ttf"

// builtInFiles are the package's embedded files: JetBrains Mono and its
// licence.
//
//go:embed jetbrainsmono.ttf jetbrainsmono-OFL.txt
var builtInFiles embed.FS

// BuiltInFiles answers the package's embedded files: the built-in font,
// JetBrains Mono, which a Font of no Path loads, at jetbrainsmono.ttf, and
// its licence at jetbrainsmono-OFL.txt, for a plugin to mount among its
// own, as canvas mounts them under cog.canvas.
func BuiltInFiles() fs.FS { return builtInFiles }

// findBuiltInFont answers the built-in font, JetBrains Mono, parsed from the
// package's embedded files once for the process: a parsed font is
// read-only, so every set's fonts share its tables.
var findBuiltInFont = sync.OnceValue(func() *gotext.Font {
	file, err := builtInFiles.Open(builtInFontPath)
	if err != nil {
		panic(err) // the package embeds its built-in font, which is always there.
	}
	face, err := gotext.ParseTTF(file.(opentype.Resource))
	if err != nil {
		panic(err) // the built-in font always parses.
	}
	return face.Font
})
