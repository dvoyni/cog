package font

// CountCharacterFonts answers how many characters, each in a language, set
// has found the font drawing, for the package's tests to see them kept as
// the language changes.
func CountCharacterFonts(set *Set) int {
	set.characterMutex.RLock()
	defer set.characterMutex.RUnlock()
	return len(set.characterFonts)
}
