package platform

import "github.com/dvoyni/cog/slots/platform/internal"

// UnlockAchievementsCmd marks achievements earned on the storefront. The upload
// follows within StoreInterval, so a caller sends every unlock as it happens
// and never batches on its own.
type UnlockAchievementsCmd = internal.UnlockAchievementsCmd

// UnlockAchievementsRequest names the achievements by their storefront names.
type UnlockAchievementsRequest = internal.UnlockAchievementsRequest

// UnlockAchievementsResponse is empty: a name the storefront refused is
// reported, because no caller can act on it.
type UnlockAchievementsResponse = internal.UnlockAchievementsResponse

// SetStatsCmd sets stats on the storefront to absolute values. Like an unlock,
// the upload follows within StoreInterval.
type SetStatsCmd = internal.SetStatsCmd

// SetStatsRequest maps each stat's storefront name to its value.
type SetStatsRequest = internal.SetStatsRequest

// SetStatsResponse is empty: a stat the storefront refused is reported.
type SetStatsResponse = internal.SetStatsResponse

// LanguageCmd asks which language the storefront runs the game in.
type LanguageCmd = internal.LanguageCmd

type LanguageRequest = internal.LanguageRequest

// LanguageResponse carries a BCP 47 tag such as "en", "pt-BR" or "zh-Hans", and
// "" when the storefront has no opinion.
type LanguageResponse = internal.LanguageResponse

// PlayerNameCmd asks the name the player goes by on the storefront, which a
// game can offer as the player's own before making one up.
type PlayerNameCmd = internal.PlayerNameCmd

type PlayerNameRequest = internal.PlayerNameRequest

// PlayerNameResponse carries the storefront's name for the player, "" when it
// has none.
type PlayerNameResponse = internal.PlayerNameResponse

// ShowTextInputCmd opens the storefront's on-screen keyboard while the player
// types into a field, so a handheld player can type at all. A game sends it
// when typing starts and HideTextInputCmd when it ends; what is typed arrives
// as ordinary text input either way.
type ShowTextInputCmd = internal.ShowTextInputCmd

// ShowTextInputRequest says where the field is, so the keyboard does not cover
// it.
type ShowTextInputRequest = internal.ShowTextInputRequest

// ShowTextInputResponse reports whether a keyboard opened. Most of the time it
// does not: a desktop player has a keyboard, and the storefront offers its own
// only on a handheld or in a gamepad mode.
type ShowTextInputResponse = internal.ShowTextInputResponse

// HideTextInputCmd closes the on-screen keyboard, if it is open.
type HideTextInputCmd = internal.HideTextInputCmd

type HideTextInputRequest = internal.HideTextInputRequest

type HideTextInputResponse = internal.HideTextInputResponse
