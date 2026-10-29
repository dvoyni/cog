//go:build windows || linux || darwin

package internal

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ebitengine/purego"
)

// errMsgSize is the size of Steam's SteamErrMsg buffer, k_cchMaxSteamErrMsg.
const errMsgSize = 1024

// initOK is k_ESteamAPIInitResult_OK.
const initOK = 0

// floatingTextInputSingleLine is k_EFloatingGamepadTextInputModeModeSingleLine:
// one line, and Enter closes the keyboard.
const floatingTextInputSingleLine = 0

// api is the slice of the Steamworks flat API steamplatform calls, bound with
// purego so the build needs no cgo. The interface accessors are versioned
// because the flat library exports only the versioned names: these are the
// versions SDK 1.65 ships, and a newer library keeps exporting them.
//
// Every function here is cdecl, which is the only convention on the 64-bit
// targets Steam ships for. No callback struct is ever read, so the packing
// that differs between Windows and the rest never matters.
type api struct {
	restartAppIfNecessary func(appID uint32) bool
	initFlat              func(errMsg *[errMsgSize]byte) int32
	runCallbacks          func()
	shutdown              func()

	userStats      func() uintptr
	setAchievement func(self uintptr, name string) bool
	setStatInt32   func(self uintptr, name string, value int32) bool
	storeStats     func(self uintptr) bool
	resetAllStats  func(self uintptr, achievementsToo bool) bool

	apps                   func() uintptr
	getCurrentGameLanguage func(self uintptr) string

	friends        func() uintptr
	getPersonaName func(self uintptr) string

	utils                           func() uintptr
	showFloatingGamepadTextInput    func(self uintptr, mode int32, x, y, width, height int32) bool
	dismissFloatingGamepadTextInput func(self uintptr) bool
}

// loadAPI opens the Steam library and binds every function api names. It
// looks beside the executable first, where a Steam depot puts the library, and
// then lets the system search for it by name.
func loadAPI() (*api, error) {
	handle, err := openSteamLibrary()
	if err != nil {
		return nil, err
	}
	a := &api{}
	bindings := []struct {
		fptr any
		name string
	}{
		{&a.restartAppIfNecessary, "SteamAPI_RestartAppIfNecessary"},
		{&a.initFlat, "SteamAPI_InitFlat"},
		{&a.runCallbacks, "SteamAPI_RunCallbacks"},
		{&a.shutdown, "SteamAPI_Shutdown"},
		{&a.userStats, "SteamAPI_SteamUserStats_v013"},
		{&a.setAchievement, "SteamAPI_ISteamUserStats_SetAchievement"},
		{&a.setStatInt32, "SteamAPI_ISteamUserStats_SetStatInt32"},
		{&a.storeStats, "SteamAPI_ISteamUserStats_StoreStats"},
		{&a.resetAllStats, "SteamAPI_ISteamUserStats_ResetAllStats"},
		{&a.apps, "SteamAPI_SteamApps_v009"},
		{&a.getCurrentGameLanguage, "SteamAPI_ISteamApps_GetCurrentGameLanguage"},
		{&a.friends, "SteamAPI_SteamFriends_v018"},
		{&a.getPersonaName, "SteamAPI_ISteamFriends_GetPersonaName"},
		{&a.utils, "SteamAPI_SteamUtils_v011"},
		{&a.showFloatingGamepadTextInput, "SteamAPI_ISteamUtils_ShowFloatingGamepadTextInput"},
		{&a.dismissFloatingGamepadTextInput, "SteamAPI_ISteamUtils_DismissFloatingGamepadTextInput"},
	}
	for _, binding := range bindings {
		if err := bind(binding.fptr, handle, binding.name); err != nil {
			return nil, err
		}
	}
	return a, nil
}

// openSteamLibrary opens steamLibrary from the executable's directory, and
// failing that by bare name.
func openSteamLibrary() (uintptr, error) {
	if executable, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(executable); err == nil {
			executable = resolved
		}
		beside := filepath.Join(filepath.Dir(executable), steamLibrary)
		if _, err := os.Stat(beside); err == nil {
			return openLibrary(beside)
		}
	}
	return openLibrary(steamLibrary)
}

// bind binds one symbol. purego panics on a symbol the library does not
// export, and a library too old to export one is a reason to run without
// Steam, not to crash.
func bind(fptr any, handle uintptr, name string) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("bind %s: %v", name, recovered)
		}
	}()
	purego.RegisterLibFunc(fptr, handle, name)
	return nil
}
