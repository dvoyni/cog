// Package platform declares the platform Slot: the storefront a game ships on,
// reached the same way whichever storefront it is. It reports achievements and
// stats to the storefront and asks it which language the game runs in.
//
// platform is a Slot: its plugin, built by platformplugin.New, requires exactly
// one Backend Adapter through BackendPort, which an Extension provides -
// steamplatform for Steam, noplatform where there is no storefront. platform
// itself carries no storefront code and no build tags.
//
// The game's own records are the source of truth. It tells platform every
// unlock and every stat as an absolute value, and platform uploads them at most
// once every Config.StoreInterval and once more at Stop, so a game never
// batches or throttles on its own. Nothing is read back.
//
// Cloud saves are not here: Steam syncs the saves directory itself, with no
// code. A storefront that needs code for it will add it.
package platform
