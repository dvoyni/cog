# Vendored: Ebitengine's gamepad layer

Everything under this directory is from
[Ebitengine](https://github.com/hajimehoshi/ebiten) at commit
`16f18b02d4b03ca44a081a502a492c82dde4e00e` (2026-10-15), licensed under
Apache-2.0 ([`LICENSE`](LICENSE)). The mapping database is
[SDL_GameControllerDB](https://github.com/mdqinc/SDL_GameControllerDB) under the
zlib license, and parts of `gamepaddb` and `gamepad_js.go` are adapted from SDL.
Both notices are in [`THIRD_PARTY_LICENSES.md`](THIRD_PARTY_LICENSES.md).

It is vendored rather than imported because these are internal packages of
Ebitengine.

| here | upstream |
| --- | --- |
| `gamepad/` | `internal/gamepad/` |
| `gamepad/sonyhid/` | `internal/gamepad/sonyhid/` |
| `gamepaddb/` | `internal/gamepaddb/` |
| `cocoa/` | `internal/cocoa/` |
| `mathutil/` | `internal/mathutil/` |

## Changes from upstream

- Import paths are rewritten from `github.com/hajimehoshi/ebiten/v2/internal/`
  to `github.com/dvoyni/cog/libs/gamepad/internal/`.
- The Android, Nintendo SDK, PlayStation 5 and Xbox (Microsoft GDK) backends
  are removed with their files. `gamepad/gamepad_windows.go` is rewritten to
  always pick the desktop DirectInput/XInput backend, and it now declares
  `handleError`, which upstream declares in the removed Xbox file.
- Line endings are LF.

Nothing else is changed, so updating is mechanical: copy the same packages from
a newer commit, repeat the changes above, and run `go test ./libs/gamepad/...`
and `go vet` for windows, linux, darwin and js/wasm. To refresh the mappings
alone, run `go run gen.go` in `gamepaddb/` after replacing
`gamecontrollerdb.txt` with a newer copy from SDL_GameControllerDB.
