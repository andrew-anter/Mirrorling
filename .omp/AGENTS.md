# mirrorling (repo dir: samsung-cast)

Wireless desktop mirroring from this laptop to Samsung AirPlay 2 (Tizen) TVs.
Product/CLI name is **mirrorling** — renamed from samsung-cast/samcast; the old names are stale.
Public repo: github.com/andrew-anter/mirrorling. MIT (CLI/Go); bundled engine LGPL-3.0-or-later.

## Layout
- `mirrorling` — bash CLI, main entry (`./mirrorling help`). `mirrorling-picker` — headless portal picker.
- `app/` — Go single-file launcher (embeds the whole appliance → `dist/mirrorling`).
- `ui/` — Wails control panel: CGO, system gtk3 + webkit2gtk-4.1, build tags `desktop,production,webkit2_41` (load-bearing).
- `tray/` — pure-Go SNI tray helper, CGO_ENABLED=0, static.
- Engine: doubletake-samsung fork in sibling clone `../doubletake-samsung` (Makefile `VENDOR`); `bin/doubletake*` are symlinks into it.
- `tests/cli.sh` — CLI regression suite: plain bash, stub binaries, private HOME/XDG tree; no root, no network, no TV.
- `docs/cu7000.md` — protocol findings for the reference TV (58CU7000, Tizen 2022).

## Build / test / verify — run before claiming done
- `make` — fork engine + ui + tray into bin/.
- `make test` — `go test ./...` in ui/ and tray/ + `bash tests/cli.sh`.
- `make vet` — go vet across app/, ui/, tray/.
- `make app` / `make release` — single-file dist/mirrorling (+ SHA256SUMS); explicit-only targets.
- `make licenses-check` — third-party bundle completeness; run when touching deps or licenses.
- Live smoke (TV must be powered on): `./mirrorling status`, `./mirrorling on`, `./mirrorling log`.

## Run
- `./mirrorling discover|tvs|tv NAME|pair|on [PIN]|off|toggle|status|log [-f]|monitors|monitor NAME|ui|tray|version`.
- Daemon: transient systemd user unit `mirrorling` (systemd-run --user); logs: `journalctl --user -u mirrorling`.
- Config: ~/.config/mirrorling/config (sourced shell); env `MIRRORLING_*` overrides config. Saved TVs: ~/.config/mirrorling/tvs.
- Pairing creds: ~/.config/doubletake/credentials.json (one-time `pair`; reconnects need no PIN).

## Traps (verified on the CU7000)
- Keep `MIRRORLING_TIMING=NTP` (default). Tizen advertises PTP but SETUP omits ClockID → media clock fails (upstream doubletake #30).
- On-TV PIN is short-lived: enter promptly (~17 s works, ~45 s → SRP M4 error 2). Re-run `mirrorling pair`.
- Never write the TV's volume: fork patch 7 removed the session-start volume write (it made the TV jump to 100% after a cast). Mid-session SET_PARAMETER volume → HTTP 500 on this firmware (non-fatal). Use the TV remote.
- 4K60 stalls the TV's WiFi link; 4K30 HEVC was stable. Auto canvas is 1080p H.264 even on the 4K panel.
- DRM-protected surfaces capture black (portal limitation).
- TV standby removes it from the LAN; stale avahi entries linger — mirrorling probes the advertised AirPlay port (dynamic, not 7000).
- Portal: mirrorling installs its own picker as `screencopy:custom_picker_binary` in ~/.config/hypr/xdph.conf and reclaims legacy samcast-picker paths (0.1.1-alpha fix); genuinely foreign pickers are left alone.
- Makefile: nested make goes through literal `make` (MAKE_BIN) because ZCode AppImage's exported $ARGV0 breaks $(MAKE). Don't "fix" this.
- After `pacman -Syu` upgrades (gstreamer, webkit2gtk, go, …) rebuild: `make` here, and in ../doubletake-samsung if engine deps changed.

## Conventions
- CHANGELOG.md follows Keep a Changelog; VERSION drives release identity (ldflags; dirty tree → `-dirty` suffix — rebuild after committing for a clean hash).
- Never commit/push unasked (user hard rule).
