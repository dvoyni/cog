package internal

import (
	"fmt"
)

// resolveConfig reads the plugin's configuration value, filling every zero field
// from the defaults, and validates what results.
func resolveConfig(value any) (Config, error) {
	config := WithDefaults(Config{})
	if value != nil {
		given, ok := value.(Config)
		if !ok {
			return Config{}, fmt.Errorf("canvas: invalid config %T", value)
		}
		config = WithDefaults(given)
	}
	if config.AtlasSize <= 0 || config.LayersPerArray < 2 || config.MaxAtlasBytes <= 0 {
		return Config{}, fmt.Errorf("canvas: invalid config values")
	}
	if err := validateFallbacks(config.FallbackFonts, config.FallbackLanguage); err != nil {
		return Config{}, err
	}
	arrayBytes := int64(config.AtlasSize) * int64(config.AtlasSize) * 4 * int64(config.LayersPerArray)
	if arrayBytes > int64(config.MaxAtlasBytes) {
		return Config{}, fmt.Errorf("canvas: atlas array requires %d bytes, budget is %d", arrayBytes, config.MaxAtlasBytes)
	}
	return config, nil
}

// Config sizes canvas's atlases; see canvas.Config. It is declared here because
// the atlases and the Lookup resource hold it, and the root aliases it.
type Config struct {
	// AtlasSize is the width and height of one atlas page, in texels.
	AtlasSize int
	// LayersPerArray is how many pages one atlas texture array holds. It must be
	// at least two.
	LayersPerArray int
	// MaxAtlasBytes bounds the GPU memory one atlas may allocate; one whole
	// array must fit within it.
	MaxAtlasBytes int
	// FallbackFonts are the fonts a character is looked up in when neither the
	// text's own font nor the built-in default has it, each named by the
	// language its glyphs are drawn for. Han characters share one code point
	// across Chinese, Japanese and Korean and are drawn differently in each, so
	// the order they are tried in decides what a reader sees; see
	// FallbackLanguage. None is the default: canvas draws only what the text's
	// font and its own default font carry.
	FallbackFonts []FallbackFont
	// FallbackLanguage is the fallback font tried first, named by its Language.
	// The rest follow in the order FallbackFonts lists them. Empty tries them in
	// that order from the first. SetFallbackLanguageCmd changes it at run time.
	FallbackLanguage string
}

// FallbackFont is one font of Config.FallbackFonts: a font file and the
// language it is drawn for, which is the name a $[language]{...} span and
// FallbackLanguage choose it by.
type FallbackFont struct {
	Language string
	Path     string
}

// validateFallbacks refuses a fallback with no language, a path canvas would not
// open, a language named twice, and a preferred language no fallback carries.
func validateFallbacks(fonts []FallbackFont, preferred string) error {
	seen := map[string]bool{}
	for _, font := range fonts {
		if font.Language == "" {
			return fmt.Errorf("canvas: fallback font %q has no language", font.Path)
		}
		if _, ok := ValidateResourcePath(font.Path); !ok {
			return fmt.Errorf("canvas: fallback font for %q has invalid path %q", font.Language, font.Path)
		}
		if seen[font.Language] {
			return fmt.Errorf("canvas: fallback language %q is named twice", font.Language)
		}
		seen[font.Language] = true
	}
	if preferred != "" && !seen[preferred] {
		return fmt.Errorf("canvas: fallback language %q has no fallback font", preferred)
	}
	return nil
}

// WithDefaults fills every zero field of config with the value canvas runs with
// when it is given none: 4096-texel pages, two pages per array and 256 MiB.
func WithDefaults(config Config) Config {
	if config.AtlasSize == 0 {
		config.AtlasSize = 4096
	}
	if config.LayersPerArray == 0 {
		config.LayersPerArray = 2
	}
	if config.MaxAtlasBytes == 0 {
		config.MaxAtlasBytes = 256 << 20
	}
	return config
}
