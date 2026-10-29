# Fork patches in vendor/doubletake

This repo vendors omarroth/doubletake at commit `5e2f1cb` (2026-10-29) with two small local
patches. Both are probe hooks of the `DOUBLETAKE_*` env-var style; neither changes default
behavior when unset. Upstreaming both (as a receiver-gated fix rather than env vars) would be
welcome — see upstream issue #30.

## 1. `DOUBLETAKE_TIMING=NTP|PTP` — internal/airplay/mirror.go

`setupMirrorSession` reads `policy.timing` from the receiver-compatibility table: PTP is chosen
when the session is encrypted, the receiver advertises feature 41, and `srcvers >= 354.54.6`.
The Samsung 58CU7000 (`srcvers=377.30.02`, Tizen) satisfies all three, but its SETUP response
omits `timingPeerInfo.ClockID`, so `mediaClock.configureFromSetup` fails hard
(`configure PTP media clock: SETUP response omitted timingPeerInfo.ClockID`) and no frame is
ever sent. `configureFromLocalClock` also requires the receiver ClockID, so there is no fallback.

The env override forces the timing protocol after the policy decision, before the media clock
allocation, making the whole downstream setup consistent (UDP timing probes, audio/video plists).

Observed with NTP: control/audio/video SETUP all 200, RECORD 200, NTP timing pings answered on
UDP 40924, IDR + delta frames streamed on TCP data port, `mirror session ready`.

Same root cause as upstream issue #30 Finding 3 (Roku TVs); the reporter's suggested upstream
fix — keep requesting PTP and fall back to NTP when SETUP omits ClockID — would subsume this
hook.

## 2. `DOUBLETAKE_PAIR_TYPE=<int>` — internal/airplay/pairing.go

Overrides `effectivePairType()` (the `X-Apple-HKP` header value; default 5 = screen-capture
pairing, 3 = SystemPairing, legacy = 0). Used for differential testing against receivers that
reject SRP under a specific pairing type (upstream #30 Finding 1). The CU7000 pairs fine with
the stock decision path (type 3, selected by `PrefersLegacyPairing()`), so this hook is unused
for this TV and kept for future receivers.

## 3. Debug instrumentation (same commit, harmless)

- `exchangePairSetupM1`: under `-debug`, dumps the M2 TLV inventory (`0x02 salt`, `0x03
  server public key`, lengths). Used to rule out TLV mis-parsing (CU7000 M2 is a plain 409-byte
  HAP M2: salt 16 B + pubkey 384 B + one 1-byte flag TLV).
- `completeSRPExchange`: under `-debug`, logs salt/pubkey lengths, PIN length, effective pair
  type. Used to pin down the PIN-expiry failure mode: PIN entered ~45 s after `pair-pin-start`
  → M4 error 2 (authentication); ~17 s → success. Samsung codes are short-lived; enter fast.

## Not patched (observed, non-fatal)

- `SET_PARAMETER volume` → HTTP 500 on this firmware; doubletake logs it as non-fatal.
- H.264 NAL type 12 (filler) units are ignored by the stream packer (expected).
- `[EVENT] channel` quirks seen on other receivers were not observed here.
