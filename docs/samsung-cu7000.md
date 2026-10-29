# Samsung 58CU7000 ("Sam58") — protocol findings

Recorded 2026-10-29 on REDACTED.

## Identity (from live mDNS TXT, `_airplay._tcp`)

```
name      = Sam58
model     = UAU7000            → Samsung 58" Crystal UHD CU7000 (2022)
firmware  = fv=p20.T-KSU2EUABC-2301.1  (Tizen)
features  = 0x7F8AD0,0x38BCF46
flags     = 0x244
deviceid  = REDACTED
srcvers   = 377.30.02
```

## What this means for casting from Linux

| Protocol | Available on TV | Linux sender |
|---|---|---|
| AirPlay 2 (HAP pairing + mirroring) | ✅ advertised; used by iPhone/Mac mirroring | ✅ `doubletake` |
| AirPlay video push (URL/media) | ✅ ("AirPlay Remote Video") | partial (pyatv), not used here |
| Miracast / Wi-Fi Direct sink | ❌ dropped by Tizen 2022 | — |
| Chromecast built-in | ❌ (Samsung TVs don't ship it) | — |
| SmartThings/Tap View mirroring | ✅ but Samsung-device-only | ❌ no Linux client |

Conclusion: AirPlay 2 mirroring via doubletake is the only wireless-desktop path.

## Sender side (doubletake, verified in source)

- Pairing: SRP-6a with on-TV PIN, credentials cached at `~/.config/doubletake/credentials.json`
  (`-creds` to relocate; `-cred-backend keyring` optional).
- Stream: RTSP/HTTP setup, ChaCha20-Poly1305 encrypted H.264 over TCP, timing via receiver-initiated
  NTP on local UDP `N`, audio control `N+1`, audio data `N+2`; `-port-range 60000-60010` pinned here.
- Audio: receiver's advertised `supportedFormats.screenStream` mask decides ALAC (built-in) vs
  AAC-ELD (needs libfdk-aac build tag). Default build is correct for this TV if it advertises ALAC.
- Capture: Wayland via `org.freedesktop.portal.ScreenCast` → PipeWire fd; portal restore token is
  requested and persisted, so the Hyprland picker appears once.
- Daemon mode (`-daemonize`) runs in the foreground and serves `$XDG_RUNTIME_DIR/doubletake.sock`;
  `samcast start` wraps it in a transient user unit for journal logs.

## Quirks

- Standby removes the TV from the network (no mDNS, no casting). Power it on first.
- Receiver canvas in auto mode: 1920x1080 H.264 group even though the panel is 4K
  (doubletake README: automatic mode joins the nominal canvas group; HEVC + max canvas is opt-in).
- Latency: auto profile ≈ 75 ms video lead / 85 ms audio lead on wireless; override with
  `-target-latency-ms` if lipsync drifts.

## References

- https://github.com/omarroth/doubletake (sender; Samsung TU8300 in tested list)
- https://github.com/FDH2/UxPlay (receiver; protocol reference both projects derive from)
