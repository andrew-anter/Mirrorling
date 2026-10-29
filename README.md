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
Makefile             builds the fork from ../doubletake-samsung (override: make VENDOR=...), symlinks bin/
bin/                 build output symlinks (not tracked)
docs/                protocol findings for this TV
~/.config/samsung-cast/config   name/FPS/encoder/port-range (written on first run)
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

First run pops the Hyprland screen-share picker once — mirror remembers your panel (portal
restore token); after that, streams start silently.

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
