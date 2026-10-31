package internal

import "github.com/dvoyni/cog/slots/platform"

// backend is the storefront that is not there: every unlock, stat and upload
// succeeds and goes nowhere, it has no language or player name to report and
// no on-screen keyboard to open.
type backend struct{}

func (backend) UnlockAchievement(string) error { return nil }

func (backend) SetStat(string, int32) error { return nil }

func (backend) Store() error { return nil }

func (backend) Language() string { return "" }

func (backend) PlayerName() string { return "" }

func (backend) ShowTextInput(platform.TextField) bool { return false }

func (backend) HideTextInput() {}

func (backend) Update() error { return nil }

func (backend) Close() {}
