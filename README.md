# samsung-cast

AirPlay 2 screen sender for Linux: **mirror** your desktop to a Samsung Tizen TV, or use the TV
as a **second monitor** (extend). samsung-cast is a fork of
[omarroth/doubletake](https://github.com/omarroth/doubletake) (Go, LGPL-3.0) developed in this
repo, plus the `samcast` CLI that turns it into a set-and-forget appliance: discovery, pairing,
daemon lifecycle, Hyprland integration, mirror/extend/toggle.

## Why this exists

The CU7000 (Tizen 2022) speaks no Miracast and has no Chromecast; its mDNS advertisement is
AirPlay 2 only (`model=UAU7000`, `_airplay._tcp`). No Linux tool spoke that as a *sender*
until doubletake (Go, LGPL-3.0): SRP-6a PIN pairing, FairPlay SAP, encrypted H.264/HEVC stream,
PipeWire/portal capture, GStreamer encoding (VAAPI/NVENC/OpenH264/x264). A Samsung Tizen TV
(TU8300) is on its tested-devices list. This fork carries the Samsung-specific work directly in
its tree (see `docs/patches.md`) and adds the `samcast` appliance layer: discovery, pairing,
daemon lifecycle, config, extend mode, toggle keybind.

## Layout

```
samcast              CLI: discover / pair / start / on / mirror / extend / off / toggle / status / log
Makefile             builds the fork, symlinks bin/
vendor/doubletake    the fork itself, tracked in this repo (base: upstream @ 5e2f1cb)
bin/                 build output symlinks (not tracked)
docs/                protocol findings for this TV
~/.config/samsung-cast/config   name/FPS/encoder/port-range (written on first run)
```

## Setup (once)

```sh
sudo pacman -S --needed gstreamer gst-plugins-base gst-plugins-good \
                        gst-plugins-bad gst-plugins-ugly gst-libav   # encoder runtime
cd ~/projects/samsung-cast && make                                 # ~6 s Go build
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
./samcast extend    # TV as a second monitor: streams a virtual Hyprland output
                    # (1080p60 right of your panel); drag windows onto it.
                    # First time only, the share picker pops — choose "SAMCAST".
./samcast off       # stop mirroring/extending (removes the virtual output)
./samcast toggle    # one-key switch — bind it in Hyprland:
# ~/.config/hypr/hyprland.conf:
#   bind = SUPER, D, exec, ~/projects/samsung-cast/samcast toggle
./samcast log -f    # daemon logs (journal)
./samcast stop      # stop the daemon entirely
```

Extend mode creates a headless Hyprland output named `SAMCAST` (`hyprctl output create headless`)
and the daemon captures that instead of your panel; switching modes restarts the daemon with a
different portal restore-token scope, so mirror and extend each remember their monitor silently.

First run of each mode pops the Hyprland screen-share picker once — mirror remembers your
panel, extend remembers the `SAMCAST` output (separate restore tokens); after that, streams
start silently.

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
| `SAMCAST_EXTEND_OUTPUT` | `SAMCAST` | name of the virtual Hyprland output streamed in extend mode |

## Status (verified on this machine, 2026-10-29)

| Milestone | Status |
|---|---|
| Build (Go 1.27, GStreamer 1.28) | ✅ |
| Discovery + model/firmware decode | ✅ live |
| Pairing (PIN on TV, SRP over HAP type 3) | ✅ credentials saved at `~/.config/doubletake/credentials.json` |
| Mirroring (portal capture → NVENC → encrypted H.264, NTP timing) | ✅ `state: streaming` |
| Extend mode (virtual headless output → scoped portal capture) | 🚧 implemented; verified to the `connect` step — TV was off, first live run pending |
| Audio (ALAC over ChaCha20, from `echo_cancel_sink.monitor`) | ✅ |

Patches applied to the fork (`vendor/doubletake`, documented in `docs/patches.md`):
`DOUBLETAKE_TIMING=NTP` override — the CU7000 advertises PTP (feature 41) but its SETUP
response omits `timingPeerInfo.ClockID`, so the PTP media clock can never start
(same root cause as upstream issue #30, Finding 3) — and `DOUBLETAKE_RESTORE_SCOPE`, which
keeps one screencast restore token per capture target so mirror and extend remember different
monitors.

## Known limits

- Extend mode adds a real (headless) monitor to the session: Hyprland focuses it on creation,
  and `off`/`stop` remove it. If the share picker appears again after a removal, choose
  `SAMCAST` once — portal restore tokens can lapse when the output is recreated.
- TV standby kills the cast path (no Wi-Fi) — power the screen first.
- **The on-TV PIN expires fast**: entering it ~45 s after it appears gives SRP `M4 error: 2`
  (auth failure); ~17 s works. Re-run `samcast pair` and enter the code immediately. Once paired,
  reconnects never need a PIN.
- The TV's AirPlay port is dynamic (was 51820), and a freshly started daemon needs a few seconds
  of mDNS discovery before `connect` can resolve it — `samcast on` waits for that automatically.
- ~75–85 ms added latency (auto wireless profile): fine for video/browsing, not gaming.
- DRM players (Netflix in a browser) render black — portal capture can't grab protected surfaces.
- Default stream canvas is the receiver's AirPlay nominal (1920x1080 H.264), max 3840x2160/4k60;
  `-video-codec hevc` experimentally unlocks the 4K canvas (untested here).
- `SET_PARAMETER volume` gets HTTP 500 from this firmware — volume control unsupported
  (non-fatal, logged once per session; control TV volume with its remote).
- Mirrored audio comes from `echo_cancel_sink.monitor` (doubletake picks the monitor of the
  default sink — here the PipeWire echo-cancel sink). If TV audio is silent, check your default
  output routing in pavucontrol.
- doubletake upstream disclaimer applies: much of its code was LLM-written, reviewed but not
  security-audited. LAN-only traffic, no inbound TCP, no port forwarding.
