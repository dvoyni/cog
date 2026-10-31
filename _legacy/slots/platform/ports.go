package platform

import "github.com/dvoyni/cog/slots/platform/internal"

// Backend is the interface platform's Adapter implements: one storefront's
// achievements, stats and language. platform calls every method under a write
// lock on its own state, so a Backend needs no lock of its own. What it is told
// is final, never incremental: an achievement is unlocked by name and a stat
// is set to an absolute value.
//
// It is declared in internal and aliased here:
//
//	interface {
//		UnlockAchievement(name string) error
//		SetStat(name string, value int32) error
//		Store() error
//		Language() string
//		ShowTextInput(field TextField) bool
//		HideTextInput()
//		Update() error
//		Close()
//	}
type Backend = internal.Backend

// BackendPort is the Port platform requires exactly one Adapter for: the
// storefront an Extension such as steamplatform or noplatform provides. A
// composition without one fails with kernel.ErrMissingAdapter.
type BackendPort = internal.BackendPort
