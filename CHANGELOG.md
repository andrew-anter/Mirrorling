# Changelog

All notable changes to this project are documented here. The format loosely follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions use semantic versioning.

## [Unreleased]

## [0.1.1-alpha] — 2026-11-04

### Fixed

- **Casts fail with "portal request was canceled" after upgrading from samsung-cast/samcast.**
  `xdph.conf` written by a pre-rename release points `screencopy:custom_picker_binary` at the old
  `samcast-picker`/`samsung-cast-picker` path, which no longer exists — and `ensure_picker` only
  reclaimed paths ending in `mirrorling-picker`, treating the legacy names as a foreign picker it
  must not clobber. Legacy picker names are now reclaimed (rewritten to the live
  `mirrorling-picker`, portal restarted); genuinely foreign pickers are still left alone.
- **TV volume no longer jumps to 100% after a cast.** The engine sent AirPlay
  `SET_PARAMETER volume: 0.000000` (0 dB = full scale) at session start, and the TV's firmware
  restores its start-of-session volume snapshot when the session closes — so the TV always ended
  up at 100%. The sender now never writes the receiver's volume; the TV keeps the level set with
  its remote (fork patch 7).

## [0.1.0-beta.1] — 2026-11-01

First public beta.

### Added

- **TV selection.** `mirrorling tvs`, `mirrorling tv <NAME>`, `mirrorling tv --forget <NAME>`, saved/offline
  TVs (MRU list in `~/.config/mirrorling/tvs`), a no-default rescue prompt when the configured
  TV is missing, and `--raw` output for the panel.
- **Monitor selection.** `mirrorling monitors` / `mirrorling monitor <NAME|auto>`; on Hyprland a headless
  `mirrorling-picker` is installed as XDPH's `screencopy:custom_picker_binary`, so a pinned monitor
  casts without the share dialog.
- **Control panel.** `mirrorling ui` with `--port`, `--browser`, `--no-open`; native Wails/WebKitGTK
  window, attach-to-running-server behavior, PIN entry, TV/monitor management and daemon logs.
- **Tray icon.** `mirrorling tray [enable|disable|status]`, a StatusNotifierItem + dbusmenu helper and
  a systemd user unit.
- **Single-file image.** `make app` produces `dist/mirrorling`, embedding the CLI, picker, engine
  binaries, panel, tray helper and license files; first run extracts to
  `~/.cache/mirrorling/app-<hash>`.
- **Release metadata.** `mirrorling version` (image hash, version, commit, extraction dir),
  `mirrorling --licenses`, `VERSION`, `make checksums` and `make release`.

### Security

- Control-panel state-changing POSTs now require `Content-Type: application/json`, blocking
  cross-site form/fetch CSRF against the unauthenticated loopback API.
- The loopback panel rejects requests with a non-loopback `Host` header (DNS-rebinding guard) and
  refuses to bind non-loopback addresses without an explicit `--allow-nonlocal` override.
- JSON bodies are size-limited (8 KiB) and unknown fields are rejected; the panel threat model is
  documented in `SECURITY.md`.
- Command/path injection defenses for TV names, monitor names and systemd unit `ExecStart` paths;
  config writes are `flock`-serialized.
- The single-file image verifies its extraction by content hash and clears damaged cache entries.

### Fixed

- `mirrorling ui` with no `DISPLAY` and no `WAYLAND_DISPLAY` now falls back to the headless server,
  prints the panel URL and keeps serving. Explicit `mirrorling-ui --window` without a display fails
  with a one-line actionable error instead of a WebKit/GTK crash.
- Closing the window that owns the panel stops its server; a second window attaches to the
  running server, and if the owner closes, the attached window shows
  `control panel server closed — run: mirrorling ui` instead of stale data.
- A port already held by a non-mirrorling service is reported (`port … is in use by something else
  (try: mirrorling ui --port N)`) instead of being killed or double-bound.
- TV discovery and matching now handle full mDNS service names, including spaces; tabs, newlines
  and other control characters are rejected because they would corrupt the config or saved list.
  Samsung model filtering accepts `U` and `Q` consumer models, and only IPv4 advertisement
  records are used (IPv6 blocks are ignored).
- The generated config no longer hardcodes a TV name; with no TV selected, `pair`/`on`/`target`
  print an actionable error. Environment variables override config-file values for every
  `MIRRORLING_*` setting, and missing keys get their documented defaults.
- Stale avahi advertisements are probed over TCP before a TV is reported live, so a powered-off
  TV is listed as saved/offline rather than offered as castable.
- `mirrorling start` resets a failed transient unit and removes a stale control socket left by a
  crashed daemon before starting, so a previous failure no longer wedges future starts.
- Monitor changes on X11 are detected and the daemon is restarted automatically; daemon startup
  waits for the control socket before `connect`, and the daemon waits for mDNS discovery.
- Picker wiring no longer clobbers a foreign `custom_picker_binary`, reclaims a stale
  `mirrorling-picker` path from an old extraction directory, and survives the monitor being
  disconnected (falls back to the GUI picker).
- Tray helper now implements dbusmenu protocol-v3 `EventGroup`/`AboutToShowGroup`, re-registers
  after a bar restart, and refuses a second instance on the same bus name.
- Desktop entry and tray unit prefer an installed `mirrorling` on `PATH` over a rotating extraction
  directory, so launchers and services survive single-file updates.

### Changed

- `mirrorling ui` opens a native Wails/WebKitGTK window by default; it no longer launches Chromium or
  Firefox. `--browser` still hands the URL to the desktop opener.
- `mirrorling` CLI usage, `--help`/`help`, and `version` are now documented entry points.
- Control-panel server and tray helper are optional at runtime: casting and the non-panel CLI
  commands work without GTK/WebKit or a tray host, but the panel (window or headless server) needs
  GTK3 + WebKitGTK 4.1.

### Build

- `make` builds the fork engine, panel (`CGO_ENABLED=1`, tags `production,webkit2_41`) and tray;
  `make app` builds the single-file image and `make release` adds `dist/SHA256SUMS`.
- The panel is dynamically linked against GTK3/WebKitGTK 4.1; these are runtime dependencies, not
  bundled. The tray helper and engine binaries are static.
- Go 1.25+ is required; the vendor build honours `VENDOR=` and absolute paths.
- LGPL-3.0-or-later compliance: `LICENSES/LGPL-3.0.txt`, the incorporated
  `LICENSES/GPL-3.0.txt`, `THIRD-PARTY-NOTICES.md`, and the fork's source commit are shipped in the
  image and printable with `mirrorling --licenses`.

### License

- Project code is now MIT-licensed in `LICENSE`; the repository previously carried an
  all-rights-reserved notice.
- The bundled `doubletake` / `doubletake-ctl` binaries remain LGPL-3.0-or-later, run as separate
  processes, and keep their own notices and corresponding-source information.

### Docs

- Complete README rewrite for public beta: tested models, support matrix, install (release and
  source), pairing, commands, monitor/TV selection, panel, tray, single-file image, troubleshooting,
  uninstall, security/privacy and licensing.
- Added `SECURITY.md` (supported versions, private reporting, panel threat model, credentials,
  network exposure, no telemetry, scope).
- `docs/cu7000.md` updated: hostname and device MAC redacted, XDPH restore-token behavior
  corrected, current quirks and README link added.

[0.1.1-alpha]: https://github.com/andrew-anter/mirrorling/releases
[0.1.0-beta.1]: https://github.com/andrew-anter/mirrorling/releases
