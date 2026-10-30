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
samcast              CLI: discover / tvs / tv / pair / start / on / mirror / off / toggle / status / log / ui
samcast-picker       headless monitor picker run by xdg-desktop-portal-hyprland per cast (Wayland)
ui/                  Go control-panel server — served + windowed by `samcast ui` (built to bin/samcast-ui)
Makefile             builds the fork from ../doubletake-samsung (override: make VENDOR=...) + ui/, symlinks bin/
bin/                 build output symlinks + samcast-ui (not tracked)
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

## TV selection

A TV's identity is its mDNS service name — `SAMCAST_NAME` in the config (default `Sam58`);
switching TVs means switching that name, and a name is always required (there is no auto):

```sh
./samcast tvs            # list Samsung TVs seen on the LAN; the configured one is marked
./samcast tv "Den TV"    # set the TV by mDNS name (spaces are fine)
./samcast tv             # show the current selection
```

Names are free-form (they come straight from the TV's advertisement), but `|`, `"`, `\`, `&` and
newlines are rejected — they would corrupt the sourced config or the sed rewrite that persists
them. Setting a name that is currently unreachable still works (standby drops the TV's Wi-Fi):
samcast warns and pins it for later. A TV only ever needs `./samcast pair` once — pairing
credentials are per-TV — before `samcast on` can mirror to it.

Picked TVs are also remembered in `~/.config/samsung-cast/tvs` (one name per line,
most-recently-used first): `samcast tvs` keeps listing those names even while their TV is off
the network — marked `saved — not on the network` — so standby never makes a known TV vanish,
and picking a saved one just pins it for when it wakes. `./samcast tv --forget <NAME>` removes
a name from that list; the configured TV itself stays set (with a note when the forgotten name
is the configured one).

When the configured TV is unreachable while other Samsung receivers are in sight, a run in a
terminal (`samcast on`/`toggle`/`pair`/`target`) prints the TVs it sees, numbered, and offers to
switch: a valid pick is persisted and the cast proceeds against it. The prompt deliberately has
no default — empty or invalid input falls back to the usual "'…' not found on the network"
failure, and non-interactive runs (a keybound `toggle`, scripts) never prompt at all, so nothing
ever starts casting to another room's TV by surprise.

## Control panel (UI)

`samcast ui` serves a small Go control panel (source in `ui/`, built to `bin/samcast-ui` by
`make`) on `http://127.0.0.1:8791` and opens it as a standalone desktop window — the same daily
controls (TV, monitor, daemon, mirror on/off) against the same config and ctl socket as the CLI:

```sh
./samcast ui                # serve + open the control window (Ctrl-C stops the server)
./samcast ui --port 8800    # listen elsewhere
./samcast ui --browser      # plain browser tab (xdg-open) instead of the app window
./samcast ui --no-open      # start the server, print the URL only
```

It also appears in desktop launchers (wofi/rofi/app grid) as **Samsung Cast**: every `samcast ui`
run writes `~/.local/share/applications/samsung-cast.desktop` — only when missing or stale
(`Exec` carries an absolute path, since launchers share no cwd with your shell). In the TV list,
saved/offline TVs stay visible, dimmed, each with a small forget button.

- **Reuse, never double-start:** before starting anything, samcast probes `GET /api/health` on
  the port. Something already answering 200 is reused — the window opens and `samcast ui` exits
  0. Anything else holding the port (404, non-HTTP, accept-but-silent) fails with
  "port … is in use by something else (try: samcast ui --port N)"; nothing is ever killed.
- **Localhost-only, no auth:** the server binds `127.0.0.1` and trusts the same user on the same
  machine — keep it that way (do not expose or port-forward it).
- **PIN pairing happens in-page:** a first-time cast leaves the daemon's connection in a waiting
  state and the panel asks for the PIN the TV shows, submitting it through the same ctl socket
  `samcast` uses.
- **Window rules (optional, not applied automatically):** the app window advertises the class
  `samsung-cast` — what Hyprland `windowrulev2` matches against — so for a tidy floating panel
  add to `~/.config/hypr/hyprland.conf`:
  ```ini
  windowrulev2 = float, class:^(samsung-cast)$
  windowrulev2 = size 480 720, class:^(samsung-cast)$   # optional
  windowrulev2 = center, class:^(samsung-cast)$         # optional
  ```

The panel reads the CLI's machine-readable modes (handy in scripts too): `samcast tvs --raw` /
`samcast monitors --raw` print plain lists — dying exactly like their interactive forms when
nothing is discovered — while `samcast tv --raw` prints the configured TV name and
`samcast monitor --raw` prints the pinned monitor or `auto`.

## Single-file build

`make app` produces `dist/samcast` — ONE static x86-64 executable carrying the whole appliance:
the `samcast` CLI, `samcast-picker`, the fork's static `doubletake`/`doubletake-ctl`, and the
control-panel server. Copy it anywhere (`~/.local/bin`, another machine) — no repo checkout
needed. On first run it unpacks itself to `~/.cache/samsung-cast/app-<hash>` and from then on
behaves exactly like the `samcast` CLI, except **no arguments means `samcast ui`** (the file IS
the app, so it opens the control panel):

```sh
make app
install -m755 dist/samcast ~/.local/bin/samcast
samcast version    # print the image hash + extraction dir
samcast            # opens the control panel; samcast on/off/toggle/... all work as usual
```

The extraction is content-hashed (`samcast version`): a rebuilt image with any changed byte
unpacks to its own directory, so a stale extraction never shadows a newer one.

Runtime dependencies are NOT bundled (the engine execs them at cast time):

```sh
# Debian trixie
sudo apt install gstreamer1.0-tools gstreamer1.0-plugins-base \
                 gstreamer1.0-plugins-good gstreamer1.0-plugins-bad \
                 avahi-utils        # and enable avahi-daemon
# Arch
sudo pacman -S --needed gstreamer gst-plugins-base gst-plugins-good \
                        gst-plugins-bad gst-plugins-ugly gst-libav avahi
```

On Wayland/Hyprland, monitor picking needs `xdg-desktop-portal-hyprland` (same package name on
Debian), and Wayland capture also needs PipeWire.

## Tuning

`~/.config/samsung-cast/config` (sourced by `samcast`):

| Var | Default | Notes |
|---|---|---|
| `SAMCAST_NAME` | `Sam58` | mDNS service name — the TV identity; set with `samcast tv` (`samcast tvs` lists). TV must be on (standby drops its Wi-Fi) |
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
