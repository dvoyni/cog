package internal

// steamLanguages maps Steam's API language codes, which are what
// GetCurrentGameLanguage answers, to BCP 47 tags. A code missing here is one
// Steam added later, and maps to no opinion.
var steamLanguages = map[string]string{
	"arabic":     "ar",
	"brazilian":  "pt-BR",
	"bulgarian":  "bg",
	"czech":      "cs",
	"danish":     "da",
	"dutch":      "nl",
	"english":    "en",
	"finnish":    "fi",
	"french":     "fr",
	"german":     "de",
	"greek":      "el",
	"hungarian":  "hu",
	"indonesian": "id",
	"italian":    "it",
	"japanese":   "ja",
	"koreana":    "ko",
	"latam":      "es-419",
	"norwegian":  "no",
	"polish":     "pl",
	"portuguese": "pt",
	"romanian":   "ro",
	"russian":    "ru",
	"schinese":   "zh-Hans",
	"spanish":    "es",
	"swedish":    "sv",
	"tchinese":   "zh-Hant",
	"thai":       "th",
	"turkish":    "tr",
	"ukrainian":  "uk",
	"vietnamese": "vi",
}
