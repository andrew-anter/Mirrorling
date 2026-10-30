# samsung-cast

AirPlay 2 screen sender for Linux: **mirror** your desktop to a Samsung Tizen TV. samsung-cast
is the `samcast` CLI — a set-and-forget appliance: discovery, pairing, daemon lifecycle,
mirror on/off/toggle — built on a
patched fork of [omarroth/doubletake](https://github.com/omarroth/doubletake) (Go, LGPL-3.0)
kept in a separate repo (`../doubletake-samsung`).

## Why this exists

The CU7000 (Tizen 2022) speaks no Miracast and has no Chromecast; its mDNS advertisement is
AirPlay 2 only (`model=UAU7000`, `_airplay._tcp`). No Linux tool spoke that as a *sender*
until doubletake (Go, LGPL-3.0): SRP-6a PIN pairing, FairPlay SAP, encrypted H.264/HEVC stream,
PipeWire/portal capture, GStreamer encoding (VAAPI/NVENC/OpenH264/x264). A Samsung Tizen TV
(TU8300) is on its tested-devices list. The fork carries the Samsung-specific receiver work
(`PATCHES.md` in that repo); samcast adds the appliance layer: discovery, pairing, daemon
lifecycle, config, toggle keybind.

## Layout

```
samcast              CLI: discover / pair / start / on / mirror / off / toggle / status / log
samcast-picker       headless monitor picker run by xdg-desktop-portal-hyprland per cast (Wayland)
Makefile             builds the fork from ../doubletake-samsung (override: make VENDOR=...), symlinks bin/
bin/                 build output symlinks (not tracked)
docs/                protocol findings for this TV
~/.config/samsung-cast/config   name/FPS/encoder/port-range/monitor (written on first run)
```

## Setup (once)

```sh
sudo pacman -S --needed gstreamer gst-plugins-base gst-plugins-good \
                        gst-plugins-bad gst-plugins-ugly gst-libav   # encoder runtime
cd ~/projects/samsung-cast && make                                 # Go build
```

Then pair once — **a PIN appears on the TV**, type it into the prompt:

```sh
./samcast pair
```

Credentials persist in `~/.config/doubletake/credentials.json`; re-pair only if the TV forgets
the laptop or you change its settings.

## Daily use

```sh
./samcast on        # start daemon (systemd-run --user, unit "samsung-cast") + mirror
./samcast off       # stop mirroring
./samcast toggle    # one-key switch — bind it in Hyprland:
# ~/.config/hypr/hyprland.conf:
#   bind = SUPER, D, exec, ~/projects/samsung-cast/samcast toggle
./samcast log -f    # daemon logs (journal)
./samcast stop      # stop the daemon entirely
```

Each cast pops the Hyprland screen-share picker until you pin a monitor (`samcast monitor eDP-1`)
— xdg-desktop-portal-hyprland implements no portal restore tokens, so the dialog would return on
every cast. See [Monitor selection](#monitor-selection).

## Monitor selection

Wayland/Hyprland is the primary target: on every cast, xdg-desktop-portal-hyprland (xdph) runs a
share-picker because it implements no portal `restore_token`s. samcast replaces that dialog with a
headless picker and a persisted choice:

```sh
./samcast monitors        # list connectable monitors; the configured one is marked
./samcast monitor eDP-1   # pin the capture monitor — casts start silently from now on
./samcast monitor         # show the current selection
./samcast monitor auto    # back to the GUI share-picker dialog on every cast
```

With more than one monitor connected and none configured, the first interactive `samcast on` asks
in the terminal and persists the pick (a keybound `samcast toggle` never prompts; undocked
monitors can still be pinned — the picker falls back to the GUI while they are absent).

Mechanics: on the first `samcast start`/`on` (Wayland sessions only), samcast writes
`screencopy:custom_picker_binary = <repo>/samcast-picker` into `~/.config/hypr/xdph.conf` —
creating the file if needed and restarting `xdg-desktop-portal-hyprland.service` only when the
file changed; a key already pointing elsewhere is left alone with a warning. The picker (xdph
spawns it fresh per cast, so config changes apply immediately) reads `SAMCAST_MONITOR` from
`~/.config/samsung-cast/config`, answers xdph's `[SELECTION]` protocol with `screen:<monitor>`
(names are Hyprland output names, enforced by xdph; see xdph's `src/shared/ScreencopyShared.cpp`),
and delegates to `/usr/bin/hyprland-share-picker` when nothing valid is configured.

On X11 (fallback path in the Go engine) the monitor is chosen by the `DOUBLETAKE_MONITOR` env
var, which `samcast` passes to the daemon at spawn (`SAMCAST_MONITOR` also feeds it). Because a
daemon's env is fixed at spawn, changing the monitor on X11 needs a daemon restart — `samcast on`
detects the drift and restarts automatically. Wayland selection ignores that variable entirely.

## Tuning

`~/.config/samsung-cast/config` (sourced by `samcast`):

| Var | Default | Notes |
|---|---|---|
| `SAMCAST_NAME` | `Sam58` | mDNS service name; TV must be on (standby drops its Wi-Fi) |
| `SAMCAST_FPS` | `60` | set `30` on weak Wi-Fi |
| `SAMCAST_HWACCEL` | `auto` | `vaapi` (AMD iGPU) / `nvenc` (GTX 1650) / `openh264` / `none` |
| `SAMCAST_PORT_RANGE` | `60000-60010` | UDP for timing/audio/control |
| `SAMCAST_TIMING` | `NTP` | `NTP`/`PTP`; CU7000 needs NTP (see patches note below) |
| `SAMCAST_EXTRA_FLAGS` | — | passed through, e.g. `"-bitrate 4500 -no-audio"` |
| `SAMCAST_MONITOR` | `""` | capture monitor (e.g. `eDP-1`); empty = picker dialog per cast — see Monitor selection |

## Status (verified on this machine, 2026-10-29)

| Milestone | Status |
|---|---|
| Build (Go 1.27, GStreamer 1.28) | ✅ |
| Discovery + model/firmware decode | ✅ live |
| Pairing (PIN on TV, SRP over HAP type 3) | ✅ credentials saved at `~/.config/doubletake/credentials.json` |
| Mirroring (portal capture → NVENC → encrypted H.264, NTP timing) | ✅ `state: streaming` |
| Audio (ALAC over ChaCha20, follows the system default output) | ✅ |

Patch samcast relies on (documented in `PATCHES.md` in the doubletake-samsung repo): the
`DOUBLETAKE_TIMING=NTP` override — the CU7000 advertises PTP (feature 41) but its SETUP
response omits `timingPeerInfo.ClockID`, so the PTP media clock can never start (same root
cause as upstream issue #30, Finding 3).

## Known limits

- A stream that drops seconds after start with `broadcast sink backlog limit exceeded` was hit
  by a >250 ms send stall (Wi-Fi hiccup); doubletake disconnects the receiver by design. Just
  rerun `samcast on`.
- TV standby kills the cast path (no Wi-Fi) — power the screen first.
- **The on-TV PIN expires fast**: entering it ~45 s after it appears gives SRP `M4 error: 2`
  (auth failure); ~17 s works. Re-run `samcast pair` and enter the code immediately. Once paired,
  reconnects never need a PIN.
- The TV's AirPlay port is dynamic (was 51820), and a freshly started daemon needs a few seconds
  of mDNS discovery before `connect` can resolve it — `samcast on` waits for that automatically.
- ~75–85 ms added latency (auto wireless profile): fine for video/browsing, not gaming.
- DRM players (Netflix in a browser) render black — portal capture can't grab protected surfaces.
- Default stream canvas is the receiver's AirPlay nominal (1920x1080 H.264), max 3840x2160/4k60.
  `-video-codec hevc` (via `SAMCAST_EXTRA_FLAGS`) unlocks the TV's 4K canvas, but 4K60 stalls
  its Wi-Fi link (`writev i/o timeout`); 4K30 HEVC was verified stable.
- `SET_PARAMETER volume` is rejected by this firmware (HTTP 500) — the sender cannot set TV
  volume at all (verified live mid-session). When an AirPlay session closes, Tizen resets its
  speaker volume to 100%; that is firmware behavior with no sender-side fix. Lower the volume
  with the TV remote after casting, or try the TV's Sound → Auto Volume setting.
- Mirrored audio captures the monitor of whatever the sound server's current
  default output is (`pulsesrc device=@DEFAULT_MONITOR@`, fork `PATCHES.md` #5),
  resolved at each stream start — no longer pinned to the
  sink that was default when the daemon started (previously
  `echo_cancel_sink.monitor` here). If TV audio is silent, check your default
  output routing in pavucontrol; a routing change applies from the next stream
  start.
- doubletake upstream disclaimer applies: much of its code was LLM-written, reviewed but not
  security-audited. LAN-only traffic, no inbound TCP, no port forwarding.

## License

samsung-cast is closed source: the `samcast` CLI, docs, and all other contents of this repo are
Copyright (C) 2026 samsung-cast contributors. All rights reserved.

The build depends on [doubletake-samsung](https://github.com/andrew-anter/doubletake-samsung)
(sibling clone, `../doubletake-samsung`), a modified
fork of [omarroth/doubletake](https://github.com/omarroth/doubletake) released under the LGPL-3.0
— see that repo's `LICENSE` and `PATCHES.md`. samcast executes its binaries as a separate process
and contains none of its code.
