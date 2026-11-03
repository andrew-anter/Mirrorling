# Third-party notices

This repository is licensed under the MIT license (see `LICENSE`).

**Carve-out:** the MIT license covers only the code written for this project
(`mirrorling`, `mirrorling-picker`, `ui/`, `tray/`, `app/`). It does not cover the
third-party components embedded in the single-file `mirrorling` image, which keep
their own licenses — notably `bin/doubletake` and `bin/doubletake-ctl`, which
are LGPL-3.0-or-later.

The **verbatim** copyright and permission text of every license listed below
is included in [`LICENSES/THIRD-PARTY.txt`](LICENSES/THIRD-PARTY.txt): every Go
module required by `ui/go.mod` and `tray/go.mod`, every Go module recorded in
the bundled `doubletake` binaries, and the Go standard library. Each block
starts with a `MODULE:` line (module path and version) and a `LICENSE:` line
(SPDX identifier); the text after it is reproduced byte for byte from the
module cache, never summarized or reconstructed. `make licenses-check` verifies
every block and fails if a module has no license file instead of omitting it.

The build stages `LICENSE`, `LICENSES/` (including `THIRD-PARTY.txt`) and this
file into the image. Run `mirrorling --licenses` to print their extracted
locations; they sit next to the extracted binaries in
`$XDG_CACHE_HOME/mirrorling/app-<hash>/` (default
`~/.cache/mirrorling/app-<hash>/`).

## Bundled binaries

### doubletake, doubletake-ctl — LGPL-3.0-or-later

- What: the AirPlay 2 mirroring engine, embedded as the two standalone static
  executables `bin/doubletake` and `bin/doubletake-ctl`.
- License: GNU Lesser General Public License, version 3 or (at your option) any
  later version. Full text in `LICENSES/LGPL-3.0.txt`; the GPL-3.0 text that
  LGPL-3.0 incorporates by reference is in `LICENSES/GPL-3.0.txt`.
- Upstream project: <https://github.com/omarroth/doubletake> (LGPL-3.0).
- This copy is a modified fork: <https://github.com/andrew-anter/doubletake-samsung>
  at commit `ebf5946c991d60d196e323e318cd4cca553afd5d`. The modifications are
  documented in that repository's `PATCHES.md`.
- Corresponding source for the exact binaries shipped: that repository at that
  commit. Clone it anonymously over HTTPS; `make doubletake-release
  doubletake-ctl-release` rebuilds both binaries. Its build metadata records the
  module pseudo-version `v0.0.0-20261030130444-ebf5946c991d`.
- The launcher and the `mirrorling` scripts invoke these two binaries as separate
  programs; they are not linked into the MIT-licensed code, so you may replace
  them with your own builds of the same command-line interface.

Go modules compiled into those two binaries (licenses determined from the
module sources in the fork's `go.mod`/`go.sum`):

| Module | Version | License |
| --- | --- | --- |
| github.com/aead/chacha20 | v0.0.0-20180709150244-8b13a72661da | MIT |
| github.com/aead/chacha20poly1305 | v0.0.0-20201124145622-1a5aba2a8b29 | MIT |
| github.com/aead/poly1305 | v0.0.0-20180717145839-3fee0db0b635 | MIT |
| github.com/cenkalti/backoff | v2.2.1+incompatible | MIT |
| github.com/godbus/dbus/v5 | v5.2.2 | BSD-2-Clause |
| github.com/grandcat/zeroconf | v1.0.0 | MIT |
| github.com/miekg/dns | v1.1.62 | BSD-3-Clause |
| github.com/zalando/go-keyring | v0.2.8 | MIT |
| golang.org/x/crypto | v0.31.0 | BSD-3-Clause |
| golang.org/x/net | v0.33.0 | BSD-3-Clause |
| golang.org/x/sys | v0.28.0 | BSD-3-Clause |
| howett.net/plist | v1.0.1 | BSD-3-Clause |

## Go standard library

The `mirrorling` launcher, `mirrorling-ui`, `mirrorling-tray` and the bundled
`doubletake` binaries include code from the Go standard library, distributed
under the BSD-3-Clause license.

## mirrorling-ui (`ui/go.mod`)

Wails v2.17.0 is the only direct dependency. Modules below are listed with the
versions in `ui/go.mod`; the licenses were read from each module's own
`LICENSE`/`COPYING` file. `go.mod` covers every platform, so a module that is
Windows-only (or otherwise unused by the Linux build) may not appear in
`go version -m bin/mirrorling-ui`.

| Module | Version | License |
| --- | --- | --- |
| github.com/wailsapp/wails/v2 | v2.17.0 | MIT |
| git.sr.ht/~jackmordaunt/go-toast/v2 | v2.0.3 | Unlicense OR MIT |
| github.com/bep/debounce | v1.2.1 | MIT |
| github.com/gabriel-vasile/mimetype | v1.4.16 | MIT |
| github.com/go-ole/go-ole | v1.3.0 | MIT |
| github.com/godbus/dbus/v5 | v5.1.0 | BSD-2-Clause |
| github.com/google/uuid | v1.6.0 | BSD-3-Clause |
| github.com/gorilla/websocket | v1.5.3 | BSD-2-Clause |
| github.com/jchv/go-winloader | v0.0.0-20210711035445-715c2860da7e | ISC |
| github.com/labstack/echo/v4 | v4.15.3 | MIT |
| github.com/labstack/gommon | v0.5.0 | MIT |
| github.com/leaanthony/go-ansi-parser | v1.6.1 | MIT |
| github.com/leaanthony/gosod | v1.0.4 | MIT |
| github.com/leaanthony/slicer | v1.6.0 | MIT |
| github.com/leaanthony/u | v1.1.1 | MIT |
| github.com/mattn/go-colorable | v0.1.14 | MIT |
| github.com/mattn/go-isatty | v0.0.22 | MIT |
| github.com/pkg/browser | v0.0.0-20240102092130-5ac0b6a4141c | BSD-2-Clause |
| github.com/pkg/errors | v0.9.1 | BSD-2-Clause |
| github.com/rivo/uniseg | v0.4.7 | MIT |
| github.com/samber/lo | v1.49.1 | MIT |
| github.com/tkrajina/go-reflector | v0.5.8 | Apache-2.0 |
| github.com/valyala/bytebufferpool | v1.0.0 | MIT |
| github.com/valyala/fasttemplate | v1.2.2 | MIT |
| github.com/wailsapp/go-webview2 | v1.0.22 | MIT |
| golang.org/x/crypto | v0.53.0 | BSD-3-Clause |
| golang.org/x/net | v0.56.0 | BSD-3-Clause |
| golang.org/x/sys | v0.46.0 | BSD-3-Clause |
| golang.org/x/text | v0.39.0 | BSD-3-Clause |

## mirrorling-tray (`tray/go.mod`)

| Module | Version | License |
| --- | --- | --- |
| github.com/godbus/dbus/v5 | v5.2.4 | BSD-2-Clause |
| golang.org/x/sys | v0.27.0 (indirect) | BSD-3-Clause |

## Runtime libraries and tools — not bundled

`mirrorling-ui` is a CGO build that is dynamically linked at run time against the
user's system libraries, including GTK 3 and WebKitGTK 4.1. The mirroring
engine also shells out to system tools such as `gst-launch-1.0` (GStreamer) and
`avahi-browse`, and relies on PipeWire and xdg-desktop-portal for screen
capture. None of these libraries, plugins or tools are included in this
repository or in the `mirrorling` image: they are installed and licensed by the
user's distribution, under their own terms.
