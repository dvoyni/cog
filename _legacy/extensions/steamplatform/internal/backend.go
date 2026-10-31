//go:build windows || linux || darwin

package internal

import (
	"bytes"
	"sync"

	"github.com/dvoyni/cog/slots/platform"
)

// backend is platform's Backend over the Steamworks flat API. platform
// serializes every call into it, so it holds no lock but the one that says the
// unavailability once.
//
// A backend Steam could not be reached for has a nil api and behaves as
// noplatform does: every call succeeds and goes nowhere. The cause is said
// once, by the first Update, because Register has no Kernel to say it with.
type backend struct {
	api       *api
	userStats uintptr
	apps      uintptr
	friends   uintptr
	utils     uintptr

	unavailable     error
	unavailableOnce sync.Once
}

// openBackend loads and initializes Steam. Its error is only ErrRelaunching,
// which fails composition so the process exits and Steam starts it again;
// every other failure is a backend that runs without Steam.
func openBackend(cfg Config) (*backend, error) {
	a, err := loadAPI()
	if err != nil {
		return &backend{unavailable: ErrUnavailable{Err: err}}, nil
	}
	if a.restartAppIfNecessary(cfg.AppID) {
		return nil, ErrRelaunching{AppID: cfg.AppID}
	}
	var message [errMsgSize]byte
	if result := a.initFlat(&message); result != initOK {
		text, _, _ := bytes.Cut(message[:], []byte{0})
		return &backend{unavailable: ErrUnavailable{Err: ErrInitFailed{Result: result, Message: string(text)}}}, nil
	}
	b := &backend{api: a, userStats: a.userStats(), apps: a.apps(), friends: a.friends(), utils: a.utils()}
	if cfg.ResetStats && a.resetAllStats(b.userStats, true) {
		a.storeStats(b.userStats)
	}
	return b, nil
}

func (b *backend) UnlockAchievement(name string) error {
	if b.api == nil {
		return nil
	}
	if !b.api.setAchievement(b.userStats, name) {
		return ErrAchievementRefused{Name: name}
	}
	return nil
}

func (b *backend) SetStat(name string, value int32) error {
	if b.api == nil {
		return nil
	}
	if !b.api.setStatInt32(b.userStats, name, value) {
		return ErrStatRefused{Name: name}
	}
	return nil
}

// Store uploads with StoreStats. A false return is a call Steam would not
// queue; the outcome of one it did queue arrives as a callback steamplatform
// does not read, because the next start uploads everything again anyway.
func (b *backend) Store() error {
	if b.api == nil {
		return nil
	}
	if !b.api.storeStats(b.userStats) {
		return errStoreFailed
	}
	return nil
}

// Language maps the language the player runs the game in on Steam. Steam
// answers only languages the app's Steamworks configuration lists, and
// English when it lists none.
func (b *backend) Language() string {
	if b.api == nil {
		return ""
	}
	return steamLanguages[b.api.getCurrentGameLanguage(b.apps)]
}

// PlayerName is the player's Steam persona name, the one their friends see.
func (b *backend) PlayerName() string {
	if b.api == nil {
		return ""
	}
	return b.api.getPersonaName(b.friends)
}

// ShowTextInput opens Steam's floating keyboard, which types into the game as
// the operating system's keyboard does. Steam opens it only on a Steam Deck or
// in Big Picture mode, and answers false everywhere else.
func (b *backend) ShowTextInput(field platform.TextField) bool {
	if b.api == nil {
		return false
	}
	return b.api.showFloatingGamepadTextInput(b.utils, floatingTextInputSingleLine,
		int32(field.X), int32(field.Y), int32(field.Width), int32(field.Height))
}

func (b *backend) HideTextInput() {
	if b.api == nil {
		return
	}
	b.api.dismissFloatingGamepadTextInput(b.utils)
}

// Update pumps Steam's callbacks. Nothing reads them, but Steam needs its
// queue drained.
func (b *backend) Update() error {
	if b.api == nil {
		var err error
		b.unavailableOnce.Do(func() { err = b.unavailable })
		return err
	}
	b.api.runCallbacks()
	return nil
}

func (b *backend) Close() {
	if b.api == nil {
		return
	}
	b.api.shutdown()
	b.api = nil
}
