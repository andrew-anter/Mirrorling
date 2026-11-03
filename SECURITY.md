# Security policy

Mirrorling is a beta, single-user desktop tool that mirrors a Linux desktop to a Samsung
AirPlay 2 TV on the local network. It is not a hardened or multi-tenant service. This file
describes what is supported, how to report problems privately, and the threat model you are
accepting.

## Supported versions

| Version | Supported |
|---|---|
| `0.1.0-beta.1` (current beta) | ✅ security fixes |
| older betas, pre-release snapshots, arbitrary `git` revisions | ❌ |

Only the latest published beta is supported. Fixes land on the default branch and in the next
release. There are no LTS branches.

## Reporting a vulnerability

Please **do not open a public issue** for an exploitable problem. Use GitHub's private
vulnerability reporting:

<https://github.com/andrew-anter/mirrorling/security/advisories/new>

Include:

- the mirrorling version (`mirrorling version`), install method (release image or source tree), and
  commit if known;
- distribution/version, session (Hyprland/Wayland or X11), and init system;
- a minimal reproduction and the impact you can demonstrate;
- relevant logs with credentials, IP addresses and TV identifiers removed.

You can expect an acknowledgement within a few days. This is a volunteer beta; there is no
bug bounty and no guaranteed response time. Please allow a reasonable disclosure window before
publishing details. Credit is given on request.

Vulnerabilities in the bundled AirPlay engine belong to the separate
[doubletake-samsung](https://github.com/andrew-anter/doubletake-samsung) project — report them
via [its security advisories](https://github.com/andrew-anter/doubletake-samsung/security/advisories/new).
Problems in Wails, GTK/WebKitGTK, PipeWire, GStreamer, xdg-desktop-portal(-hyprland), avahi or
your distribution's packages should go to those projects.

## Local control panel threat model

`mirrorling ui` serves an HTTP control panel and JSON API on `127.0.0.1:8791` by default.

- **No authentication.** The API can read state and daemon logs, rewrite the persisted config,
  start/stop the daemon and mirroring, and submit a pairing PIN. It trusts the local user.
- **Any local process or local user can call it.** Loopback binding protects against the network,
  not against other processes on the same machine. Do not run this on a multi-user host where
  other users are untrusted.
- **Loopback-only default.** Binding a non-loopback address is refused unless the underlying
  `mirrorling-ui` binary is given `--allow-nonlocal`; mirrorling never passes that flag. Do not expose
  or port-forward the panel.
- **DNS-rebinding guard.** When bound to loopback, requests whose `Host` header is not
  `localhost` or a loopback IP are rejected with 403. A web page cannot forge that header.
- **CSRF guard.** State-changing POSTs require `Content-Type: application/json`; a cross-site
  HTML form cannot send that type. Request bodies are limited to 8 KiB and unknown JSON fields
  are rejected. This stops a visited web page from driving the panel through the browser; it
  does not stop a local program that speaks the API directly.
- **PIN submission.** A first-time cast can be left waiting for the TV's PIN; the panel forwards
  the PIN over the same unauthenticated loopback API to the daemon's control socket.

The panel API has no TLS and no tokens; that is deliberate for a same-user desktop tool. If your
threat model includes hostile local users or local processes, do not run the panel.

## Credentials and local data

- Pairing credentials: `~/.config/doubletake/credentials.json`, created by the engine with
  directory mode `0700` and file mode `0600`. It contains the per-TV pairing keys/secrets needed
  to reconnect without a PIN. It is not encrypted at rest; its protection is the filesystem
  permission and your user account. Back it up accordingly, and delete it if you want to force a
  fresh PIN pairing. Also remove the pairing on the TV if you are decommissioning a machine.
- Config: `~/.config/mirrorling/config` is a **sourced shell file**. Anyone who can write it
  can execute code as you on the next `mirrorling` run. The CLI validates TV and monitor names
  before writing them, but the file is otherwise trusted user input; keep normal permissions on
  your home directory.
- Saved TVs: `~/.config/mirrorling/tvs` (names only).
- Single-file launcher: the image is unpacked and executed from
  `~/.cache/mirrorling/app-<hash>/` as your user. Treat a downloaded image exactly like the
  programs it contains; verify the release checksum.
- Logs: daemon output goes to your systemd user journal (`journalctl --user -u mirrorling`).
  Logs can contain TV names, IP addresses and protocol errors. They are not uploaded anywhere.

## Network exposure

- mDNS discovery (`_airplay._tcp`) and avahi-browse on the local network.
- Outbound TCP to the TV's advertised AirPlay port (dynamic; e.g. 51820) for pairing and the
  encrypted H.264/HEVC media stream.
- Outbound UDP in `MIRRORLING_PORT_RANGE` (default `60000-60010`) for timing, audio control and
  audio data to the TV.
- The only **network** listener is the control panel on loopback (`127.0.0.1:8791` by default).
  The engine also creates a same-user Unix socket at `$XDG_RUNTIME_DIR/doubletake.sock` (mode
  `0700`) for `doubletake-ctl`. There is no inbound TCP listener for casting, and the tool
  requires no router port forwarding.
- The tray helper exposes only session D-Bus methods (StatusNotifierItem/dbusmenu); its menu
  actions invoke the same `mirrorling` CLI as the user to start or stop mirroring, open the panel, or
  quit the helper.

Casting inherently sends screen contents and system audio to the selected TV. Do not cast
sensitive material to a TV you do not trust, and remember that the TV and its firmware are
outside this project's security boundary.

## No telemetry

Mirrorling has no analytics, crash reporting, update checks, accounts or phone-home. It
contacts the TV over the LAN and nothing else. The UI loads only its embedded local page; no
remote assets or CDNs.

## Scope

In scope:

- the `mirrorling` CLI, `mirrorling-picker`, control-panel server (`ui/`), tray helper (`tray/`) and
  single-file launcher (`app/`);
- command/path injection through TV names, monitor names, config values or environment
  variables; unsafe file permissions; the panel's loopback, Host-header and Content-Type guards;
- extraction or symlink issues in the single-file launcher.

Out of scope:

- the bundled LGPL-3.0-or-later `doubletake` engine (report to doubletake-samsung);
- upstream Wails/GTK/WebKitGTK, GStreamer, PipeWire, xdg-desktop-portal, XDPH, avahi, systemd;
- vulnerabilities that require an already-compromised local account, root, or physical access;
- a malicious or compromised TV, and protocol weaknesses inherent in AirPlay mirroring;
- denial of service from a local user or from the network against a deliberately exposed panel;
- social-engineering, spam and dependency-version reports without a demonstrated impact.

## Maturity

This is a public beta. The bundled engine's upstream project states that much of its code was
written with LLM assistance and has not had a security audit; we are not aware of an independent
audit of mirrorling's own code either. Run it on a trusted LAN, keep the panel on loopback, and
review the source if your environment is sensitive.
