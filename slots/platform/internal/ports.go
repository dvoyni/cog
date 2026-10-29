package internal

import "github.com/dvoyni/cog/kernel"

// Backend is the interface platform's Adapter implements: one storefront's
// achievements, stats and language, Steam's or Epic's or nobody's.
//
// platform calls every method under a write lock on its own state, so the
// calls are serialized against each other by construction and a Backend needs
// no lock of its own. It calls them from scheduler goroutines, never from a
// thread of the Backend's choosing.
//
// What a Backend is told is final, never incremental: an achievement is
// unlocked by name and a stat is set to an absolute value. The game's own
// records are the source of truth, and resending them is how a Backend that
// lost a write catches up.
type Backend interface {
	// UnlockAchievement marks the named achievement earned. Unlocking one
	// already earned is not an error.
	UnlockAchievement(name string) error
	// SetStat sets the named stat to value.
	SetStat(name string, value int32) error
	// Store uploads what UnlockAchievement and SetStat changed. platform calls
	// it at most once every StoreInterval, and calls it again later when it
	// fails, so a Backend never retries on its own.
	Store() error
	// Language reports the language the storefront runs the game in, as a
	// BCP 47 tag such as "en", "pt-BR" or "zh-Hans", and "" when it has no
	// opinion.
	Language() string
	// PlayerName reports the name the player goes by on the storefront, and ""
	// when it has none to give.
	PlayerName() string
	// ShowTextInput opens the storefront's on-screen keyboard beside field, for
	// a player with no physical keyboard, and reports whether it opened. What
	// the player types arrives as ordinary key and text input. A storefront
	// with no keyboard to offer here - not on a handheld, not in a gamepad
	// mode - opens nothing and reports false.
	ShowTextInput(field TextField) bool
	// HideTextInput closes the on-screen keyboard, if it is open.
	HideTextInput()
	// Update runs once per frame, for a Backend that has to pump its
	// storefront. Its error is a failure nothing else could have reported, such
	// as a storefront that never came up.
	Update() error
	// Close releases the storefront. It is the last call platform makes.
	Close()
}

// BackendPort is the Port platform requires exactly one Adapter for: the
// storefront an Extension such as steamplatform or noplatform provides. A
// composition without one fails with kernel.ErrMissingAdapter.
type BackendPort kernel.RequiredPort[Backend]
