# Mirrorling

**Unofficial AirPlay 2 screen-mirroring sender for Linux.** Mirror a Wayland or X11 desktop to a
Samsung Tizen TV that advertises AirPlay 2 — over your LAN, with no dongle, no cloud account and
nothing installed on the TV.

**Mirrorling** is the project; `mirrorling` is its command-line tool. It wraps a patched fork of
[doubletake](https://github.com/omarroth/doubletake) (Go, LGPL-3.0-or-later) that implements the
AirPlay 2 pairing, capture, encoding and streaming protocol, and adds the appliance layer:
discovery, TV and monitor selection, pairing, daemon lifecycle, a native control panel, a tray
icon and a single-file release image.

> **Unofficial and independent.** This project is not affiliated with, endorsed by, sponsored by
> or supported by Samsung Electronics or Apple Inc. “Samsung”, “Tizen”, “AirPlay”, “Apple” and
> other marks are trademarks of their respective owners and are used only to describe
> interoperability. You are responsible for complying with the terms that apply to your TV,
> network and content.

> **Public beta.** The only TV model verified by this project's author is the Samsung CU7000
> (2022). Other models are unverified or listed only because the underlying doubletake project
> lists them. Treat this as beta software and read [Known limits](#known-limits) before relying
> on it.

## Tested models

| TV | Status |
|---|---|
| Samsung CU7000 series (2022; author's unit is a 58″ set advertised as model `UAU7000`) | **Author-verified**: discovery, PIN pairing, 1080p H.264 mirroring and ALAC audio. Firmware observations are in [`docs/cu7000.md`](docs/cu7000.md). |
| Samsung TU8300 series | Appears on the upstream doubletake tested-devices list. **Not verified by this project's author.** |
| Any other model | **Not claimed.** It may work if the TV advertises AirPlay 2 and reports a `U`/`Q` model, but there is no verified result. |

No other TVs, vendors or results are claimed. `mirrorling` discovers Samsung receivers whose mDNS
`model` TXT value starts with `U` or `Q` (for example `UAU7000`); Apple TVs, Roku devices and
other vendors' AirPlay receivers are hidden and unsupported. The TV must be powered on: standby
drops its Wi-Fi and its mDNS advertisement.

## Support matrix

| Area | Status |
|---|---|
| Operating system | Arch Linux x86-64 is the tested platform. Release images are x86-64. Source builds on other architectures are untested and unsupported. |
| Session | Hyprland on Wayland with PipeWire and xdg-desktop-portal-hyprland **≥ 1.4.1** is the tested path. |
| X11 | Implemented capture fallback. Less exercised than Wayland; monitor pinning works through `MIRRORLING_MONITOR` and a daemon restart (see [Monitor selection](#monitor-selection)). |
| Other Wayland compositors | Untested. Monitor pinning and the headless picker are Hyprland/XDPH-specific; only the generic portal dialog path could work, and it has not been verified. |
| Debian 13 | Runtime packages are documented below but the combination is **untested** by the author. |
| Init / service manager | A working **systemd user instance** is required (`systemd-run`, `systemctl --user`, journal). Non-systemd distributions are unsupported. |
| Control panel | Native Wails/WebKitGTK window needs **GTK 3 and WebKitGTK 4.1** at runtime, dynamically linked. Casting and the other CLI commands work without them; the panel (window or headless server) does not. |
| Audio | A PulseAudio-compatible sound server (PipeWire's `pipewire-pulse`, or PulseAudio). Audio follows its current default output. |
| Tested TV | Samsung CU7000 (2022) only. |

## How it works

At a high level:

1. `mirrorling` enumerates AirPlay receivers on the LAN with `avahi-browse`, probes the advertised
   endpoint, and resolves the TV's current IP and dynamic AirPlay port.
2. For the first connection to a TV, the engine performs SRP-6a PIN pairing; the TV shows a PIN.
   The resulting credentials are cached, so later casts reconnect without a prompt.
3. `mirrorling` starts the engine as a transient systemd user unit named `mirrorling`, then talks
   to it through `$XDG_RUNTIME_DIR/doubletake.sock` using `doubletake-ctl`.
4. On Wayland, the engine captures through `org.freedesktop.portal.ScreenCast`, receiving a
   PipeWire file descriptor; on X11 it uses the X11 capture path. GStreamer encodes H.264 (or
   HEVC when asked) with VAAPI/NVENC/OpenH264/x264/x265 as available.
5. The stream is sent to the TV over TCP with ChaCha20-Poly1305 encryption; timing, audio control
   and audio data use UDP in `MIRRORLING_PORT_RANGE` (default `60000-60010`).

The TV sees the selected monitor as an AirPlay mirroring source. This is mirroring, not a virtual
desktop or a secondary display.

### Layout

```
mirrorling              bash CLI: discover / tvs / tv / pair / start / on / mirror / off / toggle /
                     status / log / ui / tray / version
mirrorling-picker       headless monitor picker that XDPH runs for each Wayland cast
ui/                  Go control panel: HTTP+JSON API, embedded page, native Wails window
tray/                Go tray helper: StatusNotifierItem + dbusmenu over the session bus
app/                 Go single-file launcher (embeds the CLI, picker, engine binaries, panel, tray)
Makefile             builds the fork from ../doubletake-samsung, then ui/ and tray/; `make app`
                     builds the single-file image; `make release` adds checksums
docs/                protocol findings and other documentation
LICENSE              MIT license for this project's own code
LICENSES/            LGPL-3.0 and GPL-3.0 texts for the bundled engine, plus
                     THIRD-PARTY.txt with verbatim Go module/stdlib licenses
THIRD-PARTY-NOTICES.md  third-party components, versions and licenses
SECURITY.md          threat model, private vulnerability reporting and scope
CHANGELOG.md         release notes
bin/                 build output/symlinks (gitignored)
dist/                release image and checksum (gitignored)
```

### Build from source

Requirements:

- **Go 1.25 or newer** (`go version`).
- `make`, a C compiler, `pkg-config`.
- GTK 3 and WebKitGTK 4.1 development files for the control panel.
- The engine fork checked out as a **sibling** directory named `doubletake-samsung` (or point
  `VENDOR=` at it).

```sh
git clone https://github.com/andrew-anter/mirrorling.git
cd mirrorling
git clone https://github.com/andrew-anter/doubletake-samsung.git ../doubletake-samsung
git -C ../doubletake-samsung checkout ebf5946c991d60d196e323e318cd4cca553afd5d
# optional but recommended for reproducibility:
#   VENDOR=/path/to/doubletake-samsung make
```

Arch build dependencies:

```sh
sudo pacman -S --needed go gcc make pkgconf gtk3 webkit2gtk-4.1
```

Debian build dependencies (documented, untested):

```sh
sudo apt install build-essential pkg-config libgtk-3-dev libwebkit2gtk-4.1-dev
# Debian's Go may be older than 1.25; if so, install Go from https://go.dev/dl/.
```

Then:

```sh
make                 # builds the engine, bin/doubletake[-ctl] symlinks, panel and tray
./mirrorling --help
make app             # optional: builds dist/mirrorling (the single-file image)
make release         # optional: make app + dist/SHA256SUMS
make licenses-check  # verify LICENSES/THIRD-PARTY.txt against the module cache
```

`make` alone builds a development tree that runs from the checkout. `make app` additionally
stages the license files and builds the release image; it requires the UI and tray targets to
succeed first.

## Install

### Runtime dependencies

The engine shells out to system tools at cast time, and the panel links the system GTK/WebKit
libraries. These are **not** bundled.

If `avahi-daemon` is not already running, enable and start it (system service):

```sh
sudo systemctl enable --now avahi-daemon
```

Arch:

```sh
sudo pacman -S --needed \
  avahi \
  gstreamer gst-plugins-base gst-plugins-good gst-plugins-bad gst-plugins-ugly gst-libav \
  gst-plugin-pipewire pipewire pipewire-pulse wireplumber \
  xdg-desktop-portal xdg-desktop-portal-hyprland hyprland \
  gtk3 webkit2gtk-4.1 \
  xorg-xrandr
```

- `gst-plugin-pipewire` provides `pipewiresrc`; `pipewire-pulse` provides the PulseAudio-compatible
  server used for audio capture. If you use PulseAudio instead of PipeWire, install PulseAudio and
  its GStreamer plugin instead.
- `xdg-desktop-portal-hyprland` **must be 1.4.1 or newer** for `custom_picker_binary` support.
  It provides `hyprland-share-picker`; `hyprland` provides `hyprctl`.
- `gtk3` and `webkit2gtk-4.1` are needed only for the control panel (window or its dynamically
  linked headless server), not for casting.
- `xorg-xrandr` is only needed on X11.

Debian 13 (documented, untested):

```sh
sudo apt install \
  gstreamer1.0-tools gstreamer1.0-plugins-base gstreamer1.0-plugins-good \
  gstreamer1.0-plugins-bad gstreamer1.0-plugins-ugly gstreamer1.0-libav \
  gstreamer1.0-pipewire pipewire pipewire-pulse wireplumber \
  avahi-daemon avahi-utils \
  xdg-desktop-portal \
  libgtk-3-0t64 libwebkit2gtk-4.1-0 \
  x11-xserver-utils
```

Debian 13 does not ship Hyprland or XDPH in its stable suite at the time of writing; they are in
`trixie-backports`. This project has not tested that combination:

```sh
sudo apt install -t trixie-backports hyprland xdg-desktop-portal-hyprland
```

PipeWire, the portal and XDPH run as user services. Check them with:

```sh
systemctl --user status xdg-desktop-portal xdg-desktop-portal-hyprland
```

### Release image

Download `mirrorling` and `SHA256SUMS` from the
[GitHub release](https://github.com/andrew-anter/mirrorling/releases) into the same directory,
verify, and install:

```sh
sha256sum -c SHA256SUMS
mkdir -p ~/.local/bin
install -m755 mirrorling ~/.local/bin/mirrorling
```

`SHA256SUMS` is produced by `make checksums` and records the bare filename
`mirrorling`, so `sha256sum -c SHA256SUMS` works in the download directory. If the
downloaded asset was renamed, rename it back to `mirrorling` first.

If `~/.local/bin` is not on your `PATH`, add it (for bash/zsh, in `~/.bashrc` or `~/.zshrc`):

```sh
export PATH="$HOME/.local/bin:$PATH"
command -v mirrorling      # should now print the installed path
```

Open a new shell (or source your shell's rc file) after adding the line permanently.

A successful `mirrorling ui` run also writes
`~/.local/share/applications/mirrorling.desktop`, so launchers list it as **Mirrorling**. It
is only needed for the panel; the CLI works without it.

Check the installation and the image identity:

```sh
mirrorling --help
mirrorling version
```

## First pairing

Power the TV on, make sure it is on the same LAN, and use the **exact** name that mDNS
advertises:

```sh
mirrorling tvs                 # live Samsung TVs and previously saved ones, numbered
mirrorling tv "Living Room TV" # quote names with spaces
mirrorling pair
```

The TV displays a PIN. Type it at the `mirrorling pair` prompt promptly — the code is short-lived
(about 17 seconds on the CU7000; entering it after ~45 seconds fails). Pairing is per TV, so a
second TV needs its own `mirrorling pair`. A first cast can also pair in one step by passing the PIN
to `mirrorling on` (`mirrorling on 1234`).

Credentials are stored at `~/.config/doubletake/credentials.json` (directory `0700`, file
`0600`). Re-pair only if the TV forgets the machine or you delete the credentials. There is no
default TV and no hardcoded name: until you choose one, `pair`/`on`/`target` report:

```text
mirrorling: no TV selected — run 'mirrorling tvs' to list Samsung TVs, then 'mirrorling tv <exact name>'
```

## Daily use

| Command | What it does |
|---|---|
| `mirrorling on` | Start the daemon if needed and mirror the current screen to the configured TV. Optional argument: a PIN for the initial connect. |
| `mirrorling off` | Stop mirroring; the daemon keeps running. |
| `mirrorling toggle` | Mirror on ↔ off. If already active (streaming, connecting, or waiting for a PIN), stops immediately even if mDNS is unavailable; otherwise resolves the TV and starts, failing cleanly if the TV is off. |
| `mirrorling start` / `stop` | Start/stop the background daemon (transient systemd unit `mirrorling`). |
| `mirrorling status` | Daemon and stream status (requires a running daemon). |
| `mirrorling devices` | Devices the running daemon can see. |
| `mirrorling pair` | First-time pairing; the PIN appears on the TV. |
| `mirrorling log [-f]` | Daemon logs through `journalctl --user -u mirrorling`. |
| `mirrorling ui` | Open the control panel (see [Control panel](#control-panel)). |
| `mirrorling tray …` | Manage the tray icon (see [Tray icon](#tray-icon)). |
| `mirrorling version` | Print the version: `MIRRORLING_VERSION` if set, otherwise `dev` in a source checkout. The single-file image instead prints its image hash, version, commit, extraction dir and license dir. |
| `mirrorling --help` | Usage summary. |

`mirrorling mirror` is an alias for `mirrorling on`.

Discovery and diagnostics:

| Command | What it does |
|---|---|
| `mirrorling discover` | Raw mDNS listing of Samsung AirPlay receivers with model/firmware. No liveness probe. |
| `mirrorling target` | Resolve and print the configured TV's `IP:port` (fails if it is absent). |
| `mirrorling tvs` | Castable view: live TVs plus saved/offline ones; the configured one is marked. |
| `mirrorling tv [NAME]` | Show or set the TV by its mDNS service name. |
| `mirrorling monitors` / `monitor` | List or pin the capture monitor. |

Bind the toggle to a key in Hyprland:

```ini
# ~/.config/hypr/hyprland.conf
bind = SUPER, D, exec, ~/.local/bin/mirrorling toggle
```

`mirrorling` also accepts `--raw` on `tvs`, `tv`, `monitors` and `monitor` for scripts and the panel:

- `mirrorling tvs --raw` → `name<TAB>ip<TAB>model<TAB>state` per line; saved rows leave ip/model empty.
- `mirrorling tv --raw` → the configured name (empty if none).
- `mirrorling monitors --raw` → one monitor name per line.
- `mirrorling monitor --raw` → the pinned monitor, or `auto`.

## Monitor selection

Wayland/Hyprland is the primary target. `mirrorling` replaces XDPH's interactive share dialog with a
headless picker and a persisted monitor choice:

```sh
mirrorling monitors          # list capture-capable outputs; the configured one is marked
mirrorling monitor eDP-1     # pin the capture monitor; subsequent casts start silently
mirrorling monitor           # show the current choice
mirrorling monitor auto      # revert to XDPH's stock share-picker
```

Output names on Wayland are Hyprland output names (`eDP-1`, `DP-1`, …); on X11 they come from
`xrandr`. The CLI accepts only letters, digits, `-` and `_` in a monitor name. Pinning a
currently disconnected monitor is allowed with a note; it applies when the monitor returns.

With more than one monitor connected and none configured, the first interactive `mirrorling on`
asks once and persists the pick. A keybound `mirrorling toggle` or a script never prompts.

With `auto`, no monitor is pinned: XDPH runs `hyprland-share-picker`, which prompts unless you
have configured its `allow_token_by_default` option. Pinning a monitor avoids the prompt
entirely through mirrorling's own picker.

### Why a custom picker on Wayland

XDPH **does** implement portal restore tokens. However, its stock picker only requests a token
when `screencopy:allow_token_by_default = true` is set in `~/.config/hypr/xdph.conf` (the option
defaults to off); without that setting the dialog would return on every cast. Instead of
depending on that global setting, `mirrorling` installs its own headless picker:

- On `mirrorling start`/`mirrorling on` in a working Hyprland session (probed with `hyprctl`; other
  Wayland compositors are left alone), it sets
  `screencopy:custom_picker_binary = <mirrorling-picker path>` in `~/.config/hypr/xdph.conf`,
  creating the file if needed. If the file is not writable, it warns and casting continues with
  XDPH's stock picker.
- It restarts `xdg-desktop-portal-hyprland.service` only when the file changed. XDPH reads its
  config at portal startup; the picker itself is spawned fresh for each cast, so a changed
  `MIRRORLING_MONITOR` applies from the next cast without another restart.
- A `custom_picker_binary` pointing at a foreign picker is left alone with a warning. A stale
  entry pointing at an old `mirrorling-picker` extraction directory is reclaimed and repointed.
- The picker reads `MIRRORLING_MONITOR` from `~/.config/mirrorling/config`, answers XDPH's
  `[SELECTION]` protocol with `screen:<monitor>`, and falls back to `hyprland-share-picker` when
  `auto` is configured or the pinned monitor is disconnected.

`xdg-desktop-portal-hyprland` 1.4.1 or newer is required for `custom_picker_binary`. On other
Wayland compositors this integration is untested and is skipped when `hyprctl` cannot talk to
Hyprland.

### X11

On X11 the engine chooses its capture monitor from the `DOUBLETAKE_MONITOR` environment variable;
`mirrorling start` passes the configured `MIRRORLING_MONITOR` to the daemon as that variable. A
daemon's environment is fixed at spawn, so changing the monitor requires a restart. `mirrorling on`
detects the drift in the systemd unit's environment and restarts the daemon automatically.
Wayland ignores `DOUBLETAKE_MONITOR`; on Hyprland its selection is the XDPH picker above.

## TV selection

A TV's identity is its mDNS service name. Names are free-form and may contain spaces — quote them
in the shell. Tabs, newlines and other control characters are rejected, as are `|`, `"`, `\`,
`&`, `$` and backtick, because the name is persisted in a sourced config and a `sed` rewrite.

```sh
mirrorling tvs                # live + saved, numbered; configured TV marked
mirrorling tv "Living Room TV"
mirrorling tv                 # show the configured name (or "(none)")
mirrorling tv --forget "Old TV"
```

Setting a name that is currently unreachable is allowed: `mirrorling` warns and pins it for when
the TV wakes. A TV is paired on first use: run `mirrorling pair`, or pass the PIN shown on the TV
as the argument to the first `mirrorling on` (for example, `mirrorling on 1234`). Later casts reuse
the stored credentials.

Picked names are remembered in `~/.config/mirrorling/tvs` (one per line, most-recently-used
first). `mirrorling tvs` keeps listing saved names while the TV is off the network as
`saved — not on the network`; selecting one just pins it for later. `mirrorling tv --forget NAME`
removes a name from that saved list only — it does not change the configured TV, and it notes
when you forgot the name that is currently configured.

When the configured TV is unreachable but other Samsung receivers are live, an interactive
`mirrorling on`/`toggle`/`pair`/`target` shows the rescue prompt: it lists the live TVs, numbered,
and offers to switch. The prompt has **no default**: empty or invalid input falls back to the
usual “not found” failure, and non-interactive runs (keybindings, scripts) never prompt. A valid
pick is persisted and the cast proceeds against it.

Discovery details:

- `mirrorling tvs` lists only services whose mDNS `model` starts with `U` or `Q`, using IPv4
  records (IPv6 advertisement blocks are ignored). Other vendors' AirPlay receivers are not
  shown.
- A stale avahi record can outlive a TV that went into standby. `mirrorling` probes the advertised
  endpoint over TCP before reporting a TV as live; stale records fall through to the saved list.
- The TV's AirPlay port is dynamic, so `mirrorling target`/`on` resolve it from mDNS every time.
- No TV is configured on a fresh install; there is no auto-selection.

## Control panel

`mirrorling ui` serves a local control panel and opens it in a native Wails/WebKitGTK window. It
manages the same config, daemon and ctl socket as the CLI.

```sh
mirrorling ui                  # native window on http://127.0.0.1:8791
mirrorling ui --port 8800      # listen elsewhere
mirrorling ui --browser        # start/reuse the server, open the URL with xdg-open
mirrorling ui --no-open        # start/reuse the server, print the URL only
```

Window and attach semantics:

- If nothing healthy is on the port, the process owns the server and the window. Closing the
  owning window stops the server.
- If a healthy `mirrorling-ui` already answers `GET /api/health`, a new invocation opens a window
  attached to it. Closing an attached window leaves the owner running.
- If the owner closes, an attached window shows a persistent
  `Control panel server closed — run: mirrorling ui` banner and stops showing live data; rerun the
  command to attach to a new server.
- If the port is held by something that is not a healthy `mirrorling ui`, it fails with
  `port … is in use by something else (try: mirrorling ui --port N)`. It never kills the process.
- With **neither `DISPLAY` nor `WAYLAND_DISPLAY` set**, `mirrorling ui` falls back to the headless
  server, prints the panel URL and keeps serving (same as `--no-open`, no browser). The panel
  binary still needs GTK 3 and WebKitGTK 4.1 installed to load, even for its headless server. The
  underlying `mirrorling-ui --window` binary, invoked directly without a graphical session, exits
  with a one-line actionable error instead.
- The window is titled **Mirrorling** and advertises the Wayland app class `mirrorling`.
  Optional Hyprland rules:

  ```ini
  windowrulev2 = float, class:^(mirrorling)$
  windowrulev2 = size 480 720, class:^(mirrorling)$
  windowrulev2 = center, class:^(mirrorling)$
  ```

The panel is also listed in desktop launchers as **Mirrorling** because each successful
`mirrorling ui` run writes `~/.local/share/applications/mirrorling.desktop` when it is missing or
stale. `Exec` carries an absolute path, preferring `mirrorling` found on `PATH` over a rotating
extraction directory. The panel shows live and saved TVs, lets you forget saved ones, switches
TV/monitor, starts/stops the daemon and mirroring, and can submit the TV PIN for a first-time
cast.

The panel API is **localhost-only, unauthenticated and same-user trusted**: anything that can
reach `127.0.0.1:8791` can control it. See [Security and privacy](#security-and-privacy) and
[`SECURITY.md`](SECURITY.md), and do not expose or port-forward it.

## Tray icon

`mirrorling tray` adds a StatusNotifierItem icon to the bar; it shows mirroring state (grey idle,
amber connecting, green casting), names the TV in its tooltip, opens the panel on click, and
offers Open control panel / Start mirroring / Stop mirroring / Quit from its right-click menu.
It works even when the panel window is closed.

```sh
mirrorling tray enable     # install + start ~/.config/systemd/user/mirrorling-tray.service
mirrorling tray status     # unit state, enabled state, and tray-host/registration state
mirrorling tray disable    # stop + disable the unit (the icon disappears)
mirrorling tray            # run the helper in the foreground (what the unit calls)
```

The bar must host a StatusNotifierItem tray. In waybar, enable its `tray` module; merge this into
your existing `modules-right`/`modules-left` list rather than duplicating the key:

```jsonc
"modules-right": ["tray"],
"tray": {
  "icon-size": 18,
  "spacing": 8
}
```

Without an SNI host the helper keeps retrying every 3 seconds; it logs the first failure of a
streak (and the first successful registration) instead of repeating the message, and
`mirrorling tray status` reports that the icon is not registered. Only one tray helper runs at a
time: a second instance sees the fixed bus name already owned, logs
`… already owned — another tray is running, exiting`, and exits 0 without adding a second icon.

The systemd unit uses `PartOf=graphical-session.target` and `Restart=on-failure`, so a crash
comes back, a menu-Quit stays quit, and logging out takes it down. The helper re-registers when
the bar restarts. It speaks StatusNotifierItem + dbusmenu directly over the session bus; no tray
toolkit or icon theme is required (icons are drawn procedurally).

## Single-file image

`make app` produces `dist/mirrorling`, a static x86-64 Go launcher embedding the CLI scripts,
`mirrorling-picker`, the engine's static `doubletake`/`doubletake-ctl`, the panel server, the tray
helper and the license files. Copy it anywhere:

```sh
make app
install -m755 dist/mirrorling ~/.local/bin/mirrorling
mirrorling version
```

On first run it extracts to `$XDG_CACHE_HOME/mirrorling/app-<hash>/` (default
`~/.cache/mirrorling/app-<hash>/`) and executes the CLI from there. The hash covers every
embedded path and byte, so a rebuilt image extracts to a fresh directory and never reuses a
stale tree; old image directories are garbage-collected after a day. With **no arguments**, the
image runs `mirrorling ui` (the file *is* the app), so it opens the control panel or, with no
display, falls back to the headless panel URL.

`mirrorling version` in the image prints the image hash, version (`0.1.0-beta.1` for this release),
commit, extraction directory and license directory. `mirrorling --licenses` prints the absolute
paths of the shipped `LICENSE`, `LICENSES/` (the LGPL/GPL texts plus the verbatim third-party
bundle `THIRD-PARTY.txt`) and `THIRD-PARTY-NOTICES.md` inside the extraction directory, then
exits 0. `--licenses` is only available in the single-file image, not in a source checkout.

**Not one static executable.** The launcher is static, but the embedded panel is a CGO build
dynamically linked against your system's GTK 3 and WebKitGTK 4.1, and the scripts still use
system tools. Runtime dependencies are listed under [Runtime dependencies](#runtime-dependencies)
and are not bundled. The tray helper and the engine binaries are static.

## Configuration

`~/.config/mirrorling/config` is created on first run and sourced by the `mirrorling` bash CLI.
Prefer the commands above; edit the file only for tuning. All values can also be supplied as
environment variables, which override the file, and missing keys fall back to these defaults:

| Variable | Default | Meaning |
|---|---|---|
| `MIRRORLING_NAME` | empty | TV mDNS service name. No TV is selected by default; use `mirrorling tv`. |
| `MIRRORLING_FPS` | `60` | Capture frame rate. Try `30` on weak Wi-Fi or for 4K. |
| `MIRRORLING_HWACCEL` | `auto` | Encoder: `auto`, `vaapi`, `nvenc`, `openh264`, or `none` (software x264/x265). |
| `MIRRORLING_PORT_RANGE` | `60000-60010` | Local UDP range for timing, audio control and audio data. |
| `MIRRORLING_TIMING` | `NTP` | `NTP` or `PTP`. The CU7000 advertises PTP but omits `timingPeerInfo.ClockID` in SETUP, so it needs `NTP`. |
| `MIRRORLING_EXTRA_FLAGS` | empty | Extra doubletake flags, e.g. `"-bitrate 4500 -no-audio"` or `"-video-codec hevc"`. |
| `MIRRORLING_MONITOR` | empty | Pinned capture monitor; empty means the picker/dialog. Prefer `mirrorling monitor`. |

Notes:

- The config is a shell file. Keep it owned and writable only by you; a writable config is
  equivalent to code execution as your user. `mirrorling` validates names before persisting them.
- `MIRRORLING_EXTRA_FLAGS` is word-split by the shell, so quote the whole value and use spaces
  between flags.
- Tuning is applied when the daemon starts. `mirrorling on` starts the daemon if needed and restarts
  it for an X11 monitor change; `mirrorling start` deliberately leaves an already-running daemon
  alone. After changing FPS, encoder or extra flags, run `mirrorling stop` and then `mirrorling on`.

## Troubleshooting

### `mirrorling tvs` finds nothing, or the TV is missing

- Power the TV on first; standby drops its Wi-Fi and mDNS advertisement.
- Confirm the laptop and TV are on the same LAN/VLAN and that multicast/mDNS is not blocked by
  “AP isolation” or a guest network.
- Check avahi:

  ```sh
  systemctl status avahi-daemon
  avahi-browse -rt _airplay._tcp -t
  ```

  Install `avahi` on Arch (`avahi-browse` is included) or `avahi-daemon` + `avahi-utils` on
  Debian, then `sudo systemctl enable --now avahi-daemon`.
- A stale avahi record is not reported as live; `mirrorling tvs` probes it and, if the name was
  saved earlier, falls back to `saved` until the TV is reachable again.
- `mirrorling tvs` shows only Samsung `U`/`Q` model receivers. Use `mirrorling discover` for the raw
  mDNS view.

### Casting fails with a GStreamer or PipeWire error

- Missing `pipewiresrc` is the usual cause: install `gst-plugin-pipewire` (Arch) or
  `gstreamer1.0-pipewire` (Debian). Verify:

  ```sh
  gst-inspect-1.0 pipewiresrc
  gst-inspect-1.0 pulsesrc
  ```

- Make sure PipeWire and the portal are running in your user session:

  ```sh
  systemctl --user status pipewire pipewire-pulse xdg-desktop-portal xdg-desktop-portal-hyprland
  ```

- Ensure `XDG_RUNTIME_DIR` is set and that the portal can reach the compositor.
- `mirrorling log -f` (or `journalctl --user -u mirrorling -f`) shows the engine's error.

### The control panel does not open

- The window needs GTK 3 and WebKitGTK 4.1. Install `gtk3` + `webkit2gtk-4.1` on Arch, or
  `libgtk-3-0t64` + `libwebkit2gtk-4.1-0` on Debian 13. Without a graphical session, `mirrorling ui`
  falls back to the headless server and prints the URL; it keeps serving until stopped. Use
  `--no-open` in scripts.
- If port 8791 is busy:

  ```sh
  ss -ltnp 'sport = :8791'
  mirrorling ui --port 8800
  ```

  The panel never kills the process holding the port. If it is a stale `mirrorling ui`, stop it or
  use another port.
- A `Control panel server closed — run: mirrorling ui` banner means the instance that owned the
  server exited; start it again.
- Health check (with `curl` installed): `curl http://127.0.0.1:8791/api/health`.

### Pairing fails or the PIN is rejected

- The on-TV PIN expires quickly on the CU7000: about 17 seconds works, ~45 seconds fails with an
  SRP authentication error. Re-run `mirrorling pair` and enter it immediately.
- Make sure the TV name is exact (`mirrorling tvs`), the TV is awake, and you are pairing the TV you
  intend to cast to. Credentials are per TV.
- If the TV forgot the machine, re-pair. If credentials are corrupt, remove
  `~/.config/doubletake/credentials.json` and pair again.

### No TV audio, or audio comes from the wrong place

- The engine captures the sound server's **default output** (`device=@DEFAULT_MONITOR@`),
  resolved at each stream start. Set the default sink you want (for example in `pavucontrol`),
  then start a new cast; changing it mid-stream does not move an existing stream.
- If the TV is silent, check the TV's input/volume and that the cast is not muted in the panel.
- The sender never writes the TV's volume: it used to command full scale (0 dB) at session
  start, and Tizen restores its start-of-session snapshot when the session closes — which is why
  the TV used to jump to 100% after a cast. The TV keeps whatever you set with the remote.

### 4K is not used, or the stream stutters

- The default stream canvas is the receiver's 1920×1080 H.264 group. HEVC with the TV's maximum
  canvas is opt-in through extra flags and requires a fresh daemon so the new encoder arguments
  are applied:

  ```sh
  mirrorling stop
  MIRRORLING_EXTRA_FLAGS="-video-codec hevc" MIRRORLING_FPS=30 mirrorling on
  ```

- On the CU7000, 4K60 stalls the TV's Wi-Fi link; 4K30 HEVC was stable. Keep `MIRRORLING_FPS=30`
  for 4K.
- DRM-protected surfaces (for example Netflix in a browser) are captured as black; this is a
  portal/capture limitation, not a setting.
- A stream that drops seconds after start with `broadcast sink backlog limit exceeded` hit a
  >250 ms send stall (Wi-Fi hiccup). Rerun `mirrorling on`.

### `xdg-desktop-portal-hyprland.service` did not restart

`mirrorling` warns when the restart fails. Log out and back in, or restart it manually:

```sh
systemctl --user restart xdg-desktop-portal-hyprland.service
```

### The tray icon is missing

- Install/enable a StatusNotifierItem host and its tray module (waybar's `tray`, see
  [Tray icon](#tray-icon)).
- `mirrorling tray status` prints the unit state, whether it is enabled, and whether a
  StatusNotifierWatcher (tray host) is present. A helper without a host keeps retrying every 3
  seconds and logs the first failure of a streak (not every attempt).
- Check the unit logs: `journalctl --user -u mirrorling-tray -f`.

## Security and privacy

- **No telemetry.** mirrorling has no analytics, accounts, update checks or phone-home. It
  talks to the TV over your LAN and to its own loopback panel; the page loads only embedded
  assets. See [`SECURITY.md`](SECURITY.md) for the full policy and private reporting channel.
- **The panel is unauthenticated but loopback-only.** It binds `127.0.0.1` (default port 8791).
  Any local process or local user that can reach loopback can control the daemon, rewrite the
  config and read logs. Requests with a non-loopback `Host` header are rejected (DNS-rebinding
  guard), state-changing POSTs require `Content-Type: application/json` (CSRF guard), and bodies
  are limited to 8 KiB. Those guards stop browser-based cross-site attacks, not local programs.
  Do not expose or port-forward the panel; the raw `mirrorling-ui` binary's `--allow-nonlocal` flag
  is not used by `mirrorling` and is unsupported.
- **Credentials.** `~/.config/doubletake/credentials.json` contains per-TV pairing secrets, mode
  `0600` in a `0700` directory, not encrypted at rest. Protect backups, and delete it plus the
  pairing on the TV when decommissioning a machine.
- **Network.** Discovery uses mDNS; pairing and streaming use outbound TCP to the TV's dynamic
  AirPlay port; timing/audio use outbound UDP `60000-60010` by default. There is no inbound TCP
  listener for casting and no port forwarding. The engine's control socket
  (`$XDG_RUNTIME_DIR/doubletake.sock`) is a same-user Unix socket. By default the engine encrypts
  the AirPlay stream; the TV and its firmware remain outside this project's boundary.
- **Local data.** Config (`~/.config/mirrorling/config`) is sourced shell code; saved TV names
  live beside it; the single-file image is unpacked and executed from
  `~/.cache/mirrorling/app-<hash>/` as you. Tray actions go over the session D-Bus and expose
  only start/stop mirroring, open-panel and quit.
- **Beta maturity.** The bundled engine's upstream project states much of its code was written
  with LLM assistance and has not had a security audit; we are not aware of an independent audit
  of this project's code either. Use it on a trusted LAN and keep the panel on loopback.

## Known limits

- **One TV and one monitor at a time.** Mirroring is to the configured TV from the pinned
  monitor (or the portal's picker). Monitor pinning and the headless picker are Hyprland-specific;
  other Wayland compositors are untested.
- **Pin expiry.** The CU7000's on-TV PIN is valid for only about 17 seconds; after ~45 seconds
  pairing fails with an SRP authentication error. Re-run `mirrorling pair`.
- **Standby kills casting.** A TV in standby has no Wi-Fi and no mDNS; power it on first.
- **Dynamic AirPlay port.** The TV picks its port (51820 was observed); `mirrorling` resolves it
  from mDNS. A freshly started daemon needs a few seconds of discovery before `connect`; `on`
  waits for that.
- **Latency.** The automatic wireless profile adds roughly 75 ms video lead / 85 ms audio lead.
  Fine for video and browsing, not for gaming. `-target-latency-ms` (via
  `MIRRORLING_EXTRA_FLAGS`) can override it if lip-sync drifts.
- **Resolution and codec.** Default is the receiver's 1920×1080 H.264 canvas. `-video-codec hevc`
  unlocks the TV's 4K canvas, but 4K60 stalls this TV's Wi-Fi (`writev i/o timeout`); 4K30 HEVC
  was verified stable. `MIRRORLING_FPS=30` reduces bandwidth.
- **DRM content is black.** Portal capture cannot capture protected surfaces.
- **TV volume is not controllable from the sender.** Mid-session `SET_PARAMETER volume` is
  rejected by CU7000 firmware (HTTP 500), and the sender no longer writes the volume at session
  start either — doing so set the TV to full scale, and Tizen restores its start-of-session
  snapshot at close, which left the TV at 100% after every cast. The TV keeps the volume you set
  with the remote; see the fork's PATCHES.md patch 7.
- **Audio follows the default output.** Captured audio is the sound server's current default
  output, resolved at each stream start; change the default sink before casting. If TV audio is
  silent, check routing in `pavucontrol`.
- **Wi-Fi stalls drop streams.** A >250 ms send stall can trigger `broadcast sink backlog limit
  exceeded`; the engine disconnects by design. Rerun `mirrorling on`.
- **Beta coverage.** Only the CU7000 (2022) is author-verified; the TU8300 is vendor-tested only;
  Debian 13, X11 and non-Hyprland Wayland are not author-verified.

## Uninstall

```sh
mirrorling off
mirrorling tray disable
mirrorling stop
rm -f ~/.local/bin/mirrorling
rm -rf ~/.config/mirrorling ~/.cache/mirrorling
rm -f ~/.local/share/applications/mirrorling.desktop
rm -f ~/.config/systemd/user/mirrorling-tray.service
systemctl --user daemon-reload
```

Pairing credentials live outside the mirrorling config directory because they belong to the
engine and may be shared with other doubletake use:

```sh
# Only if nothing else uses doubletake on this machine:
rm -f ~/.config/doubletake/credentials.json
```

To remove the Wayland picker integration, edit `~/.config/hypr/xdph.conf` and delete the
`screencopy:custom_picker_binary` line that points at `mirrorling-picker` (if the file contains
only mirrorling's comment and that line, you may delete the file). Then restart the portal:

```sh
systemctl --user restart xdg-desktop-portal-hyprland.service
```

Old extraction directories are cleaned automatically, but `rm -rf ~/.cache/mirrorling` removes
them immediately. Daemon logs remain in your user journal until journald rotates them; remove
the transient unit with `mirrorling stop` (and optionally `systemctl --user reset-failed mirrorling`).

## License

- **This project's own code is MIT-licensed** — the `mirrorling` CLI and picker scripts, `ui/`,
  `tray/`, `app/`, build files and documentation. See [`LICENSE`](LICENSE).
- **The bundled engine is LGPL-3.0-or-later.** `bin/doubletake` and `bin/doubletake-ctl` are a
  modified fork of [omarroth/doubletake](https://github.com/omarroth/doubletake), maintained at
  [andrew-anter/doubletake-samsung](https://github.com/andrew-anter/doubletake-samsung) at commit
  `ebf5946c991d60d196e323e318cd4cca553afd5d`. They are shipped as separate embedded executables,
  not linked into the MIT code, and they keep their own license. The fork's modifications are
  documented in its `PATCHES.md`.
- `LICENSES/LGPL-3.0.txt`, `LICENSES/GPL-3.0.txt` (the GPL text incorporated by reference),
  `LICENSES/THIRD-PARTY.txt` (verbatim Go module and Go standard library license texts),
  [`THIRD-PARTY-NOTICES.md`](THIRD-PARTY-NOTICES.md) and the MIT `LICENSE` are staged into the
  single-file image. In that image, `mirrorling --licenses` prints their extracted paths.
- **Do not relicense the combined distribution.** If you redistribute the image, keep the license
  and notice files with it and comply with the LGPL-3.0-or-later corresponding-source
  requirements for the bundled engine. UI/tray Go dependencies and runtime system libraries are
  itemized in `THIRD-PARTY-NOTICES.md` and remain under their own licenses.

## Support and issues

This is a volunteer beta. Bug reports and questions go to
[GitHub Issues](https://github.com/andrew-anter/mirrorling/issues). Please include:

- `mirrorling version` (and whether you use the release image or a source checkout);
- distribution, session (Hyprland/Wayland or X11), PipeWire and XDPH versions;
- TV model and firmware (the `model`/`fv` fields from `mirrorling discover` are useful);
- the failing command and relevant `mirrorling log` output, with credentials, MAC addresses and
  other identifiers removed.

Report security problems privately via
[GitHub security advisories](https://github.com/andrew-anter/mirrorling/security/advisories/new),
not in a public issue. See [`SECURITY.md`](SECURITY.md).

The tested-devices list is deliberately small. If you verify another Samsung model, a report
with the model/firmware and what worked is welcome; please do not present an untested model as
supported in the meantime.
