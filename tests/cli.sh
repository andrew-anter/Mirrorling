#!/usr/bin/env bash
# Regression tests for the mirrorling CLI (mirrorling + mirrorling-picker integration).
#
# Plain bash; no root, no network, no TV. Everything runs against a private
# HOME/XDG tree and stub binaries: avahi-browse, hyprctl, systemctl,
# systemd-run, journalctl, busctl (PATH) plus doubletake-ctl (app/bin, which is
# where mirrorling looks for it). python3 is used only for localhost TCP listeners
# (so tv_alive can succeed) and a one-shot HTTP health server.
#
# Run: tests/cli.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TEST_DIR="$(mktemp -d "${TMPDIR:-/tmp}/mirrorling-cli-test.XXXXXX")"
APP="$TEST_DIR/app"
STUB_BIN="$TEST_DIR/bin"
LISTENER_PID=""
HEALTH_SRV_PID=""
CHECKS=0

cleanup() {
    if [ -n "$LISTENER_PID" ]; then kill "$LISTENER_PID" 2>/dev/null || true; fi
    if [ -n "$HEALTH_SRV_PID" ]; then kill "$HEALTH_SRV_PID" 2>/dev/null || true; fi
    if [ -f "${STUB_DIR:-}/ui.pid" ]; then
        local uipid=""
        uipid="$(cat "$STUB_DIR/ui.pid" 2>/dev/null || true)"
        [ -z "$uipid" ] || kill "$uipid" 2>/dev/null || true
    fi
    rm -rf "$TEST_DIR"
}
trap cleanup EXIT

ok() {
    CHECKS=$((CHECKS + 1))
    printf 'ok %d - %s\n' "$CHECKS" "$*"
}
fail() {
    printf 'not ok - %s\n' "$*" >&2
    printf 'test dir kept: %s\n' "$TEST_DIR" >&2
    if [ -n "$LISTENER_PID" ]; then kill "$LISTENER_PID" 2>/dev/null || true; fi
    if [ -n "$HEALTH_SRV_PID" ]; then kill "$HEALTH_SRV_PID" 2>/dev/null || true; fi
    trap - EXIT
    exit 1
}
assert_eq() {
    local got="$1" want="$2" what="$3"
    [ "$got" = "$want" ] || fail "$what: got '$got', want '$want'"
    ok "$what"
}
assert_contains() {
    local haystack="$1" needle="$2" what="$3"
    case "$haystack" in
        *"$needle"*) ;;
        *) fail "$what: missing '$needle' in: $haystack" ;;
    esac
    ok "$what"
}
assert_not_contains() {
    local haystack="$1" needle="$2" what="$3"
    case "$haystack" in
        *"$needle"*) fail "$what: unexpected '$needle' in: $haystack" ;;
    esac
    ok "$what"
}
conf_path() { printf '%s\n' "$XDG_CONFIG_HOME/mirrorling/config"; }
free_port() {
    python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()'
}

# One clean state per test: config, runtime socket, saved TV list, stub files.
reset_state() {
    rm -rf "$XDG_CONFIG_HOME" "$XDG_RUNTIME_DIR" "$XDG_DATA_HOME" "$HOME" "$STUB_DIR"
    mkdir -p "$XDG_CONFIG_HOME" "$XDG_RUNTIME_DIR" "$XDG_DATA_HOME" "$HOME" "$STUB_DIR"
    unset MIRRORLING_NAME MIRRORLING_FPS MIRRORLING_HWACCEL MIRRORLING_PORT_RANGE \
        MIRRORLING_TIMING MIRRORLING_EXTRA_FLAGS MIRRORLING_MONITOR MIRRORLING_VERSION \
        DISPLAY WAYLAND_DISPLAY 2>/dev/null || true
}

# --- sandbox -----------------------------------------------------------------
mkdir -p "$APP/bin" "$STUB_BIN"
cp "$ROOT/mirrorling" "$APP/mirrorling"
cp "$ROOT/mirrorling-picker" "$APP/mirrorling-picker"
chmod +x "$APP/mirrorling" "$APP/mirrorling-picker"

# A UI binary that records its argv and refuses to serve. Used only to show
# which path cmd_ui chose; the health-reuse test never starts it.
cat > "$APP/bin/mirrorling-ui" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$@" > "${STUB_DIR}/ui.args"
exit 1
EOF
chmod +x "$APP/bin/mirrorling-ui"

export HOME="$TEST_DIR/home"
export XDG_CONFIG_HOME="$TEST_DIR/config"
export XDG_RUNTIME_DIR="$TEST_DIR/run"
export XDG_DATA_HOME="$TEST_DIR/data"
export STUB_DIR="$TEST_DIR/stub"
mkdir -p "$HOME" "$XDG_CONFIG_HOME" "$XDG_RUNTIME_DIR" "$XDG_DATA_HOME" "$STUB_DIR"
export PATH="$STUB_BIN:$PATH"

cat > "$STUB_BIN/avahi-browse" <<'EOF'
#!/usr/bin/env bash
if [ -f "${STUB_DIR}/avahi.fail" ]; then exit 1; fi
[ -f "${STUB_DIR}/avahi.txt" ] && cat "${STUB_DIR}/avahi.txt"
exit 0
EOF

cat > "$STUB_BIN/hyprctl" <<'EOF'
#!/usr/bin/env bash
[ -f "${STUB_DIR}/hyprctl.ok" ] || exit 1
if [ "${1:-}" = "monitors" ]; then
    if [ -f "${STUB_DIR}/hyprctl.monitors" ]; then
        cat "${STUB_DIR}/hyprctl.monitors"
    else
        printf 'Monitor eDP-1 (id 0):\n\t1920x1080@60.000 at 0x0\n'
        printf 'Monitor HDMI-A-1 (id 1):\n\t2560x1440@60.000 at 1920x0\n'
    fi
fi
exit 0
EOF

cat > "$STUB_BIN/systemctl" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "${STUB_DIR}/systemctl.calls"
verb=""; quiet=0; unit=""
for a in "$@"; do
    case "$a" in
        --user) ;;
        --quiet) quiet=1 ;;
        -*) ;;
        *)
            if [ -z "$verb" ]; then verb="$a"
            elif [ -z "$unit" ]; then unit="$a"
            fi
            ;;
    esac
done
case "$verb" in
    is-active)
        if [ -f "${STUB_DIR}/active" ] && grep -qx -e "$unit" "${STUB_DIR}/active"; then
            [ "$quiet" -eq 1 ] || echo active
            exit 0
        fi
        [ "$quiet" -eq 1 ] || echo inactive
        exit 3
        ;;
    is-enabled)
        if [ -f "${STUB_DIR}/enabled" ] && grep -qx -e "$unit" "${STUB_DIR}/enabled"; then
            echo enabled
            exit 0
        fi
        echo disabled
        exit 1
        ;;
    show)
        [ -f "${STUB_DIR}/systemctl.show" ] && cat "${STUB_DIR}/systemctl.show"
        ;;
esac
exit 0
EOF

cat > "$STUB_BIN/systemd-run" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$@" > "${STUB_DIR}/systemd-run.args"
if [ -f "${STUB_DIR}/systemd-run.fail" ]; then
    cat "${STUB_DIR}/systemd-run.fail" >&2
    exit 1
fi
if [ -e "$XDG_RUNTIME_DIR/doubletake.sock" ]; then
    echo present > "${STUB_DIR}/socket-at-spawn"
else
    echo absent > "${STUB_DIR}/socket-at-spawn"
fi
# Leave a Unix socket behind, exactly like the daemon would (`-S` succeeds).
python3 - "$XDG_RUNTIME_DIR/doubletake.sock" <<'PY'
import os, socket, sys
path = sys.argv[1]
try:
    os.unlink(path)
except FileNotFoundError:
    pass
s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
s.bind(path)
s.close()
PY
# From here on the daemon answers status, so cmd_start's final status succeeds.
printf '{"state":"idle"}\n' > "${STUB_DIR}/status.json"
EOF

cat > "$STUB_BIN/journalctl" <<'EOF'
#!/usr/bin/env bash
[ -f "${STUB_DIR}/journal.txt" ] && cat "${STUB_DIR}/journal.txt"
exit 0
EOF

cat > "$STUB_BIN/busctl" <<'EOF'
#!/usr/bin/env bash
sub="${1:-}"
if [ "$sub" = "--user" ]; then sub="${2:-}"; fi
case "$sub" in
    status)
        [ -f "${STUB_DIR}/watcher" ] && exit 0
        exit 1
        ;;
    get-property)
        if [ -f "${STUB_DIR}/watcher.items" ]; then
            cat "${STUB_DIR}/watcher.items"
        else
            printf 'as 0\n'
        fi
        exit 0
        ;;
esac
exit 1
EOF

cat > "$APP/bin/doubletake-ctl" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "${STUB_DIR}/ctl.calls"
case "${1:-}" in
    status)
        if [ -f "${STUB_DIR}/status.json" ]; then cat "${STUB_DIR}/status.json"; exit 0; fi
        exit 1
        ;;
    devices)
        if [ -f "${STUB_DIR}/devices.txt" ]; then cat "${STUB_DIR}/devices.txt"; exit 0; fi
        exit 1
        ;;
esac
exit 0
EOF

chmod +x "$STUB_BIN/avahi-browse" "$STUB_BIN/hyprctl" "$STUB_BIN/systemctl" \
    "$STUB_BIN/systemd-run" "$STUB_BIN/journalctl" "$STUB_BIN/busctl" \
    "$APP/bin/doubletake-ctl"

# --- listeners ---------------------------------------------------------------
# Two localhost TCP listeners give tv_alive something to connect to without
# touching the network. Their ports go into the fake avahi records.
start_listeners() {
    local dir="$TEST_DIR/listen" portfile="$TEST_DIR/listen/ports"
    mkdir -p "$dir"
    python3 - "$portfile" <<'PY' &
import socket, sys, time

out = sys.argv[1]
socks = []
ports = []
for _ in range(2):
    s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    s.bind(("127.0.0.1", 0))
    s.listen(32)
    socks.append(s)
    ports.append(str(s.getsockname()[1]))
with open(out, "w") as f:
    f.write(" ".join(ports) + "\n")
while True:
    time.sleep(3600)
PY
    LISTENER_PID=$!
    local i
    for i in $(seq 1 100); do
        [ -s "$portfile" ] && break
        sleep 0.05
    done
    [ -s "$portfile" ] || fail "listener did not start"
    read -r TV1_PORT TV2_PORT < "$portfile"
}

write_avahi_live() {
    cat > "$STUB_DIR/avahi.txt" <<EOF
= wlan0 IPv4 Living Room TV                              _airplay._tcp        local
   hostname = [living.local]
   address = [127.0.0.1]
   port = [$TV1_PORT]
   txt = ["model=QN65Q80TAFXZA" "fv=2.1.0"]
= wlan0 IPv6 Living Room TV                              _airplay._tcp        local
   hostname = [living.local]
   address = [fe80::1234]
   port = [$TV1_PORT]
   txt = ["model=QN65Q80TAFXZA" "fv=2.1.0"]
= wlan0 IPv4 Samsung Bedroom                             _airplay._tcp        local
   hostname = [bedroom.local]
   address = [127.0.0.1]
   port = [$TV2_PORT]
   txt = ["model=UE43AU7100" "fv=1.2"]
= wlan0 IPv4 Kitchen Speaker                             _airplay._tcp        local
   hostname = [speaker.local]
   address = [127.0.0.1]
   port = [$TV2_PORT]
   txt = ["model=LG-OLED"]
EOF
}

# --- tests -------------------------------------------------------------------
test_help_version() {
    reset_state
    "$APP/mirrorling" help >/dev/null
    ok "help exits 0"
    "$APP/mirrorling" --help >/dev/null
    ok "--help exits 0"
    "$APP/mirrorling" -h >/dev/null
    ok "-h exits 0"
    assert_eq "$("$APP/mirrorling" version)" "dev" "version defaults to dev"
    assert_eq "$(MIRRORLING_VERSION=9.9.9 "$APP/mirrorling" version)" "9.9.9" "version honors MIRRORLING_VERSION"
    local out
    if out="$("$APP/mirrorling" nope 2>&1)"; then fail "unknown command should fail"; fi
    assert_contains "$out" "unknown command" "unknown command message"
}

test_empty_default_and_guidance() {
    reset_state
    "$APP/mirrorling" version >/dev/null
    local conf
    conf="$(conf_path)"
    [ -f "$conf" ] || fail "first run did not write $conf"
    grep -Eq '^MIRRORLING_NAME=""([[:space:]]|$)' "$conf" || fail "generated config must default MIRRORLING_NAME to empty"
    if grep -q 'Sam58' "$conf"; then fail "generated config still hardcodes Sam58"; fi
    ok "first run writes MIRRORLING_NAME=\"\" and no Sam58"

    local out
    out="$("$APP/mirrorling" tv)"
    assert_contains "$out" "(none)" "tv with no name says (none)"
    assert_contains "$out" "mirrorling tv <NAME>" "tv with no name prints a hint"
    assert_eq "$("$APP/mirrorling" tv --raw)" "" "tv --raw is empty with no TV"

    local c
    for c in pair on target; do
        if out="$("$APP/mirrorling" "$c" 2>&1)"; then fail "$c must fail without a selected TV"; fi
        assert_contains "$out" "no TV selected" "$c without a TV: clear error"
        assert_contains "$out" "mirrorling tvs" "$c without a TV: points at mirrorling tvs"
        assert_contains "$out" "mirrorling tv <exact name>" "$c without a TV: points at mirrorling tv"
    done
}

test_tv_state_spaces_models() {
    reset_state
    write_avahi_live

    local raw
    raw="$("$APP/mirrorling" tvs --raw)"
    local name ip model state extra seen_living=0 seen_bedroom=0
    while IFS=$'\t' read -r name ip model state extra; do
        [ -n "$name" ] || continue
        [ -z "$extra" ] || fail "raw row has more than 4 columns: $name|$ip|$model|$state|$extra"
        case "$state" in
            live|saved) ;;
            *) fail "raw row state is not live/saved: $name -> '$state'" ;;
        esac
        case "$name" in
            "Living Room TV")
                seen_living=1
                assert_eq "$ip" "127.0.0.1" "live row keeps the IPv4 address"
                assert_eq "$model" "QN65Q80TAFXZA" "live row model is the model, not firmware"
                assert_eq "$state" "live" "live row state is live"
                ;;
            "Samsung Bedroom")
                seen_bedroom=1
                assert_eq "$state" "live" "U-model TV is live"
                ;;
            *Kitchen*) fail "non-Samsung LG service leaked into tvs" ;;
        esac
    done <<<"$raw"
    [ "$seen_living" = 1 ] || fail "Living Room TV missing from tvs --raw: $raw"
    [ "$seen_bedroom" = 1 ] || fail "Samsung Bedroom missing from tvs --raw: $raw"
    assert_not_contains "$raw" "fe80::" "IPv6 link-local does not win over IPv4"
    assert_not_contains "$raw" "2.1.0" "firmware never appears in the state column"

    local out
    out="$("$APP/mirrorling" tvs)"
    assert_contains "$out" "Living Room TV" "tvs display keeps the full name with spaces"
    assert_contains "$out" "127.0.0.1" "tvs display shows the live IP"
    assert_contains "$out" "model=QN65Q80TAFXZA" "tvs display shows the QLED model"
    assert_contains "$out" "model=UE43AU7100" "tvs display shows the U model"
    assert_not_contains "$out" "saved — not on the network" "live TVs are not reported as saved"

    out="$("$APP/mirrorling" discover)"
    assert_contains "$out" "model=QN65Q80TAFXZA" "discover accepts Q models"
    assert_contains "$out" "model=UE43AU7100" "discover accepts U models"
    assert_contains "$out" "fw=2.1.0" "discover still prints firmware"
    assert_not_contains "$out" "LG-OLED" "discover filters other vendors"

    # Setting, listing, resolving and casting with a spaced name.
    "$APP/mirrorling" tv "Living Room TV" >/dev/null
    grep -Fxq 'MIRRORLING_NAME="Living Room TV"' "$(conf_path)" ||
        fail "config did not persist the spaced TV name"
    ok "tv sets a spaced name in the config"
    local tvout
    tvout="$("$APP/mirrorling" tv)"
    assert_eq "${tvout%%$'\n'*}" "Living Room TV" "tv prints the full spaced name"
    assert_eq "$("$APP/mirrorling" tv --raw)" "Living Room TV" "tv --raw prints the full spaced name"
    assert_eq "$("$APP/mirrorling" target)" "127.0.0.1:$TV1_PORT" "target resolves the spaced name"
    out="$("$APP/mirrorling" tvs)"
    assert_contains "$out" "(configured)" "tvs marks the spaced configured name"

    printf '{"state":"idle"}\n' > "$STUB_DIR/status.json"
    printf '127.0.0.1\n' > "$STUB_DIR/devices.txt"
    out="$("$APP/mirrorling" on 2>&1)"
    assert_contains "$out" "Mirroring to Living Room TV" "on casts to the spaced name"
    assert_contains "$(cat "$STUB_DIR/ctl.calls")" "connect 127.0.0.1" "on calls doubletake-ctl connect"

    # Once the TV disappears, the saved list still yields a 4-column row.
    rm -f "$STUB_DIR/avahi.txt"
    raw="$("$APP/mirrorling" tvs --raw)"
    assert_contains "$raw" "Living Room TV"$'\t\t\t'"saved" "saved row keeps 4 columns with empty ip/model"
    assert_eq "$(printf '%s\n' "$raw" | awk -F'\t' 'NF != 4')" "" "saved raw rows have exactly 4 fields"
    out="$("$APP/mirrorling" tvs)"
    assert_contains "$out" "saved — not on the network" "saved TV is marked offline"

    if out="$("$APP/mirrorling" tv "Bad"$'\t'"Name" 2>&1)"; then fail "tab in TV name must be rejected"; fi
    assert_contains "$out" "invalid TV name" "tv_name_ok rejects control characters"
}

test_rescue_menu() {
    reset_state
    cat > "$STUB_DIR/avahi.txt" <<EOF
= wlan0 IPv4 Living Room TV                              _airplay._tcp        local
   hostname = [living.local]
   address = [127.0.0.1]
   port = [$TV1_PORT]
   txt = ["model=QN65Q80TAFXZA" "fv=2.1.0"]
EOF
    mkdir -p "$(dirname "$(conf_path)")"
    printf 'MIRRORLING_NAME="Gone TV"\n' > "$(conf_path)"
    command -v script >/dev/null 2>&1 || { ok "rescue menu skipped (no script(1))"; return; }
    local out
    if ! out="$(printf '1\n' | script -qec "$APP/mirrorling target" /dev/null 2>&1)"; then
        fail "rescue menu target failed: $out"
    fi
    assert_contains "$out" "other Samsung receivers" "rescue menu lists candidates"
    assert_contains "$out" "Living Room TV" "rescue menu shows the spaced name"
    assert_contains "$out" "127.0.0.1:$TV1_PORT" "rescue pick resolves and prints the target"
    grep -Fxq 'MIRRORLING_NAME="Living Room TV"' "$(conf_path)" ||
        fail "rescue pick did not persist the spaced name"
    ok "rescue menu persists a spaced name"
}

test_config_missing_keys() {
    reset_state
    mkdir -p "$(dirname "$(conf_path)")"
    printf 'MIRRORLING_NAME="Sam58"' > "$(conf_path)"   # no trailing newline, keys missing
    assert_eq "$("$APP/mirrorling" tv --raw)" "Sam58" "config missing keys sources without unbound variable"
    local out
    out="$("$APP/mirrorling" monitor)"
    assert_contains "$out" "auto" "missing MIRRORLING_MONITOR defaults to auto"
    "$APP/mirrorling" monitor HDMI-A-1 >/dev/null 2>&1
    [ -z "$(tail -c 1 "$(conf_path)")" ] || fail "append did not add a trailing newline first"
    ok "append adds the missing trailing newline"
    grep -Fxq 'MIRRORLING_MONITOR="HDMI-A-1"' "$(conf_path)" ||
        fail "missing key was not appended"
    ok "missing key is appended"
    assert_eq "$("$APP/mirrorling" monitor --raw)" "HDMI-A-1" "rewritten config sources cleanly"
}

test_toggle_off() {
    reset_state
    mkdir -p "$(dirname "$(conf_path)")"
    printf 'MIRRORLING_NAME="Old TV"\n' > "$(conf_path)"
    printf '{"state":"streaming","device":"Old TV"}\n' > "$STUB_DIR/status.json"

    local out
    out="$("$APP/mirrorling" toggle 2>&1)" || fail "toggle off with streaming state failed: $out"
    assert_contains "$out" "mirroring stopped" "toggle disconnects an active stream"
    assert_contains "$(cat "$STUB_DIR/ctl.calls" 2>/dev/null || true)" "disconnect" "toggle calls disconnect"
    if grep -q '^connect' "$STUB_DIR/ctl.calls" 2>/dev/null; then fail "toggle connected instead of disconnecting"; fi
    ok "toggle does not connect while streaming"
    assert_not_contains "$out" "not found on the network" "toggle-off never resolves mDNS"

    # mDNS completely broken (avahi-browse exits 1): still turns off.
    : > "$STUB_DIR/ctl.calls"
    : > "$STUB_DIR/avahi.fail"
    out="$("$APP/mirrorling" toggle 2>&1)" || fail "toggle off with broken avahi failed: $out"
    assert_contains "$out" "mirroring stopped" "toggle-off works with broken avahi"
    assert_contains "$(cat "$STUB_DIR/ctl.calls")" "disconnect" "broken-avahi toggle still disconnects"
}

# The state must come from the JSON "state" field only. The old whole-blob
# regex also matched error text ("error: connecting to the daemon failed"),
# which turned a keybound ON into a disconnect.
test_toggle_state() {
    reset_state
    write_avahi_live
    mkdir -p "$(dirname "$(conf_path)")"
    printf 'MIRRORLING_NAME="Living Room TV"\n' > "$(conf_path)"
    printf '127.0.0.1\n' > "$STUB_DIR/devices.txt"
    local st out calls
    for st in streaming connecting pin_required; do
        : > "$STUB_DIR/ctl.calls"
        printf '{"state":"%s","device":"Living Room TV"}\n' "$st" > "$STUB_DIR/status.json"
        out="$("$APP/mirrorling" toggle </dev/null 2>&1)" || fail "toggle with state=$st failed: $out"
        assert_contains "$out" "mirroring stopped" "state=$st turns mirroring off"
        calls="$(cat "$STUB_DIR/ctl.calls")"
        assert_contains "$calls" "disconnect" "state=$st calls disconnect"
        if grep -q '^connect' <<<"$calls"; then fail "state=$st also connected"; fi
        ok "state=$st never connects"
    done

    # Pretty-printed JSON (ctl's indented output) must work the same way.
    : > "$STUB_DIR/ctl.calls"
    printf '{\n  "state": "connecting",\n  "device": "Living Room TV"\n}\n' > "$STUB_DIR/status.json"
    out="$("$APP/mirrorling" toggle </dev/null 2>&1)" || fail "toggle with pretty connecting JSON failed: $out"
    assert_contains "$(cat "$STUB_DIR/ctl.calls")" "disconnect" "pretty JSON state=connecting disconnects"

    # idle: the ON path (resolve + start + connect).
    : > "$STUB_DIR/ctl.calls"
    printf '{"state":"idle"}\n' > "$STUB_DIR/status.json"
    out="$("$APP/mirrorling" toggle </dev/null 2>&1)" || fail "toggle on from idle failed: $out"
    calls="$(cat "$STUB_DIR/ctl.calls")"
    assert_contains "$calls" "connect 127.0.0.1" "state=idle connects"
    if grep -q '^disconnect' <<<"$calls"; then fail "state=idle disconnected"; fi
    ok "state=idle never disconnects"

    # Malformed status whose free-form error text says "connecting": the ON
    # toggle must survive it (this is the regression the old regex caused).
    : > "$STUB_DIR/ctl.calls"
    printf 'error: connecting to the daemon failed\n' > "$STUB_DIR/status.json"
    out="$("$APP/mirrorling" toggle </dev/null 2>&1)" || fail "toggle with error-text status failed: $out"
    calls="$(cat "$STUB_DIR/ctl.calls")"
    assert_contains "$calls" "connect 127.0.0.1" "error text mentioning 'connecting' does not disconnect"
    if grep -q '^disconnect' <<<"$calls"; then fail "malformed status triggered a disconnect"; fi
    ok "malformed status never disconnects"

    # Truncated JSON: no state value -> treat as not streaming.
    : > "$STUB_DIR/ctl.calls"
    printf '{"state":\n' > "$STUB_DIR/status.json"
    out="$("$APP/mirrorling" toggle </dev/null 2>&1)" || fail "toggle with truncated status failed: $out"
    assert_contains "$(cat "$STUB_DIR/ctl.calls")" "connect 127.0.0.1" "truncated JSON does not disconnect"
}

test_env_fps_start() {
    reset_state
    mkdir -p "$(dirname "$(conf_path)")"
    # Hardcoded 60 in the config; env must win. Missing keys take defaults.
    printf 'MIRRORLING_NAME="Sam58"\nMIRRORLING_FPS=60\nMIRRORLING_HWACCEL=auto' > "$(conf_path)"
    printf '{"state":"idle"}\n' > "$STUB_DIR/status.json"
    local out
    out="$(MIRRORLING_FPS=24 "$APP/mirrorling" start 2>&1)" || fail "start failed: $out"
    local joined
    joined="$(awk 'BEGIN { sep = "" } { printf "%s%s", sep, $0; sep = " " } END { print "" }' \
        "$STUB_DIR/systemd-run.args")"
    assert_contains "$joined" "-fps 24" "env MIRRORLING_FPS overrides the config file"
    assert_not_contains "$joined" "-fps 60" "config FPS is not used when env overrides it"
    assert_contains "$joined" "-port-range 60000-60010" "missing MIRRORLING_PORT_RANGE uses its default"
    assert_contains "$joined" "-hwaccel auto" "hwaccel is passed"
    assert_contains "$joined" "--collect" "systemd-run uses --collect"
    assert_contains "$joined" "--setenv=DOUBLETAKE_TIMING=NTP" "timing default reaches the daemon env"
    assert_contains "$(cat "$STUB_DIR/systemctl.calls")" "reset-failed mirrorling" "start resets a failed unit first"
    assert_contains "$out" '"state":"idle"' "start prints daemon status after the socket appears"
}

test_systemd_run_failure() {
    reset_state
    mkdir -p "$(dirname "$(conf_path)")"
    printf 'MIRRORLING_NAME="Sam58"\n' > "$(conf_path)"
    printf 'Failed to start transient service unit: Unit mirrorling.service already exists.\n' \
        > "$STUB_DIR/systemd-run.fail"
    local out
    if out="$("$APP/mirrorling" start 2>&1)"; then fail "start must fail when systemd-run fails"; fi
    assert_contains "$out" "could not start the daemon" "systemd-run failure gets the friendly prefix"
    assert_contains "$out" "Unit mirrorling.service already exists" "systemd-run stderr is preserved"
    assert_contains "$(cat "$STUB_DIR/systemctl.calls")" "reset-failed mirrorling" "reset-failed runs before start"
}

test_stale_socket() {
    reset_state
    mkdir -p "$(dirname "$(conf_path)")"
    printf 'MIRRORLING_NAME="Sam58"\n' > "$(conf_path)"
    python3 - "$XDG_RUNTIME_DIR/doubletake.sock" <<'PY'
import socket, sys
s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
s.bind(sys.argv[1])
s.close()
PY
    [ -S "$XDG_RUNTIME_DIR/doubletake.sock" ] || fail "stale socket fixture was not created"
    local out
    out="$("$APP/mirrorling" start 2>&1)" || fail "start with a stale socket failed: $out"
    assert_eq "$(cat "$STUB_DIR/socket-at-spawn")" "absent" "inactive unit + dead ctl status removes the stale socket"
    assert_contains "$out" '"state":"idle"' "start succeeds after clearing the stale socket"
}

test_picker_setup() {
    reset_state
    mkdir -p "$(dirname "$(conf_path)")"
    printf 'MIRRORLING_NAME="Sam58"\n' > "$(conf_path)"
    printf '{"state":"idle"}\n' > "$STUB_DIR/status.json"

    # WAYLAND_DISPLAY alone, hyprctl failing: nothing may be written.
    WAYLAND_DISPLAY=wayland-1 "$APP/mirrorling" start >/dev/null 2>&1 || fail "start should not abort outside Hyprland"
    [ ! -e "$XDG_CONFIG_HOME/hypr/xdph.conf" ] ||
        fail "xdph.conf was touched without a working Hyprland session"
    ok "picker setup is skipped when hyprctl fails even with WAYLAND_DISPLAY set"

    reset_state
    mkdir -p "$(dirname "$(conf_path)")" "$XDG_CONFIG_HOME/hypr"
    printf 'MIRRORLING_NAME="Sam58"\n' > "$(conf_path)"
    printf '{"state":"idle"}\n' > "$STUB_DIR/status.json"
    : > "$STUB_DIR/hyprctl.ok"
    : > "$XDG_CONFIG_HOME/hypr/xdph.conf"
    chmod 0444 "$XDG_CONFIG_HOME/hypr/xdph.conf"
    if [ "$(id -u)" -eq 0 ]; then
        ok "unwritable xdph.conf test skipped as root"
        return
    fi
    local out
    out="$(WAYLAND_DISPLAY=wayland-1 "$APP/mirrorling" start 2>&1)" ||
        fail "start aborted on an unwritable xdph.conf: $out"
    assert_contains "$out" "not writable" "unwritable xdph.conf warns"
    [ ! -s "$XDG_CONFIG_HOME/hypr/xdph.conf" ] || fail "unwritable xdph.conf was modified"
    ok "unwritable xdph.conf is left alone and start continues"
}

# A read-only ~/.config/hypr (writable file or not) makes both the create and
# the rewrite branch fail. Bash reports a failed group-level redirection as
# status 0, so the create branch used to claim success, restart the portal and
# leak the raw "Permission denied" — the rewrite branch silently rewrote
# nothing. Neither may happen: warn, leave the file alone, no restart.
test_picker_write_failure() {
    reset_state
    if [ "$(id -u)" -eq 0 ]; then
        ok "picker write-failure tests skipped as root"
        return
    fi
    local hypr="$XDG_CONFIG_HOME/hypr" out calls
    mkdir -p "$(dirname "$(conf_path)")" "$hypr"
    printf 'MIRRORLING_NAME="Sam58"\n' > "$(conf_path)"
    printf '{"state":"idle"}\n' > "$STUB_DIR/status.json"
    : > "$STUB_DIR/hyprctl.ok"

    # Read-only dir, xdph.conf missing: the create branch.
    chmod 0555 "$hypr"
    out="$(WAYLAND_DISPLAY=wayland-1 "$APP/mirrorling" start 2>&1)" ||
        fail "start aborted on a read-only hypr dir: $out"
    assert_contains "$out" "cannot write" "read-only dir + missing xdph.conf warns about the write"
    assert_not_contains "$out" "Permission denied" "read-only dir + missing file: no raw shell error"
    assert_not_contains "$out" "restarting xdg-desktop-portal-hyprland" "failed write does not claim the picker was installed"
    calls="$(cat "$STUB_DIR/systemctl.calls" 2>/dev/null || true)"
    assert_not_contains "$calls" "restart xdg-desktop-portal-hyprland" "failed create does not restart the portal"
    [ ! -e "$hypr/xdph.conf" ] || fail "xdph.conf appeared inside a read-only dir"
    ok "failed create leaves no xdph.conf behind"

    # Read-only dir, existing xdph.conf (writable file): the rewrite branch —
    # the tmp file cannot be created, so mv never happens either.
    chmod 0755 "$hypr"
    printf 'screencopy:custom_picker_binary = /old/dir/mirrorling-picker\nfont=1\n' > "$hypr/xdph.conf"
    chmod 0644 "$hypr/xdph.conf"
    : > "$STUB_DIR/systemctl.calls"
    chmod 0555 "$hypr"
    out="$(WAYLAND_DISPLAY=wayland-1 "$APP/mirrorling" start 2>&1)" ||
        fail "start aborted on a read-only hypr dir with xdph.conf: $out"
    assert_contains "$out" "cannot write" "read-only dir + existing xdph.conf warns about the rewrite"
    assert_not_contains "$out" "Permission denied" "read-only rewrite: no raw shell error"
    calls="$(cat "$STUB_DIR/systemctl.calls" 2>/dev/null || true)"
    assert_not_contains "$calls" "restart xdg-desktop-portal-hyprland" "failed rewrite does not restart the portal"
    assert_contains "$(cat "$hypr/xdph.conf")" "/old/dir/mirrorling-picker" "failed rewrite leaves xdph.conf untouched"
    assert_eq "$(ls "$hypr" | grep -c 'xdph.conf.tmp' || true)" "0" "failed rewrite leaves no tmp file"
    chmod 0755 "$hypr"
}

test_tray_status() {
    reset_state
    local out
    out="$("$APP/mirrorling" tray status 2>&1)"
    assert_contains "$out" "no StatusNotifierWatcher" "tray status names the missing watcher"
    local duplicates
    duplicates="$(printf '%s\n' "$out" | grep -c 'inactive' || true)"
    assert_eq "${duplicates:-0}" "1" "tray status prints inactive exactly once"

    if out="$("$APP/mirrorling" tray status extra 2>&1)"; then fail "tray status must reject extra args"; fi
    assert_contains "$out" "takes no arguments" "tray status rejects extra args"

    : > "$STUB_DIR/watcher"
    printf 'as 2 "org.kde.StatusNotifierItem-1-1" "org.kde.StatusNotifierItem-2-1"\n' \
        > "$STUB_DIR/watcher.items"
    out="$("$APP/mirrorling" tray status 2>&1)"
    assert_contains "$out" "StatusNotifierWatcher active, 2 item(s)" "tray status reports watcher items"
}

test_ui() {
    reset_state
    local out p
    for p in 0 65536 abc; do
        if out="$("$APP/mirrorling" ui --port "$p" 2>&1)"; then fail "ui --port $p must fail"; fi
        case "$p" in
            abc) assert_contains "$out" "--port wants a number" "ui rejects a non-numeric port" ;;
            *) assert_contains "$out" "between 1 and 65535" "ui rejects port $p" ;;
        esac
    done

    # Health probe must carry Host: a server that 403s without it proves the
    # header (the real UI hostGuard behaves the same way).
    reset_state
    local portfile="$TEST_DIR/health.port"
    python3 - "$portfile" <<'PY' &
import http.server, socketserver, sys

portfile = sys.argv[1]
PORT = None

class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        ok = self.headers.get("Host") == "127.0.0.1:%s" % PORT
        self.send_response(200 if ok else 403)
        self.end_headers()
        self.wfile.write(b"ok\n" if ok else b"forbidden\n")

    def log_message(self, *args):
        pass

srv = socketserver.TCPServer(("127.0.0.1", 0), Handler)
PORT = srv.server_address[1]
with open(portfile, "w") as f:
    f.write(str(PORT))
srv.serve_forever()
PY
    HEALTH_SRV_PID=$!
    local i
    for i in $(seq 1 100); do
        [ -s "$portfile" ] && break
        sleep 0.05
    done
    [ -s "$portfile" ] || fail "health server did not start"
    local hport
    hport="$(cat "$portfile")"
    out="$("$APP/mirrorling" ui --no-open --port "$hport" 2>&1)" ||
        fail "ui --no-open failed against a healthy server: $out"
    assert_contains "$out" "mirrorling UI: http://127.0.0.1:$hport" "ui reuses a healthy server"
    kill "$HEALTH_SRV_PID" 2>/dev/null || true
    HEALTH_SRV_PID=""
    ok "health probe sends Host and reused server stays healthy"

    # No display: app mode must fall back to the headless path, never exec
    # --window.
    reset_state
    local freeport
    freeport="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()')"
    if out="$(env -u DISPLAY -u WAYLAND_DISPLAY "$APP/mirrorling" ui --port "$freeport" 2>&1)"; then
        fail "ui with no display and a dead UI binary should fail, got: $out"
    fi
    assert_contains "$out" "no graphical session" "ui without a display explains the fallback"
    assert_contains "$out" "serving the panel headless" "ui without a display uses the headless path"
    local uiargs
    uiargs="$(cat "$STUB_DIR/ui.args" 2>/dev/null || true)"
    assert_not_contains "$uiargs" "--window" "ui never execs --window without a display"
    assert_contains "$uiargs" "--addr" "ui starts the headless server"

    # Missing GTK/WebKit: ldd preflight dies with the install hint.
    reset_state
    cat > "$STUB_BIN/ldd" <<'EOF'
#!/usr/bin/env bash
printf 'libwebkit2gtk-4.1.so.0 => not found\n'
printf 'libgtk-3.so.0 => not found\n'
exit 1
EOF
    chmod +x "$STUB_BIN/ldd"
    if out="$("$APP/mirrorling" ui --no-open --port 12345 2>&1)"; then fail "ui must fail on missing libraries"; fi
    rm -f "$STUB_BIN/ldd"
    assert_contains "$out" "missing shared libraries" "ui preflight reports missing libraries"
    assert_contains "$out" "install GTK3 and WebKit2GTK" "ui preflight gives the install hint"
    assert_contains "$out" "libgtk-3-0t64" "ui preflight names the Debian 13 GTK package"
    assert_contains "$out" "bookworm: libgtk-3-0" "ui preflight still names the bookworm package"
}

# --port must reject anything whose decimal value cannot fit 1..65535 BEFORE
# bash's arithmetic sees it: $((10#N)) wraps modulo 2^64, so
# 18446744073709560407 silently became 8791 and started a server there.
test_ui_port_validation() {
    reset_state
    local out p freeport
    for p in 0 65536 99999 \
        18446744073709560407 18446744073709560408 999999999999999999999 \
        11111111111111111111111111 000000000000000000000000; do
        if out="$("$APP/mirrorling" ui --port "$p" 2>&1)"; then
            fail "ui --port $p must fail, got: $out"
        fi
        assert_contains "$out" "between 1 and 65535" "ui rejects overflowing/out-of-range port $p"
        assert_not_contains "$out" "starting UI server" "port $p never reaches the server path"
    done

    # Leading zeros stay legal; the stripped value is what gets used.
    freeport="$(free_port)"
    if out="$("$APP/mirrorling" ui --no-open --port "000$freeport" 2>&1)"; then
        fail "ui with a dead UI binary should still fail after parsing: $out"
    fi
    assert_not_contains "$out" "between 1 and 65535" "leading zeros on a valid port are accepted"
    assert_contains "$out" "http://127.0.0.1:$freeport" "leading zeros are stripped before use"
    if out="$("$APP/mirrorling" ui --no-open --port 08 2>&1)"; then
        ok "ui accepted port 08 as 8 (a server answered there)"
    else
        assert_not_contains "$out" "between 1 and 65535" "bare 08 is accepted as port 8"
        assert_not_contains "$out" "wants a number" "bare 08 parses as a number"
    fi
}

# Ctrl-C / SIGTERM / the server exiting on its own must all end with the
# server gone. The old trap read a function-local under `set -u` at global
# scope (so it aborted with "unbound variable" and killed nothing), and no
# TERM handling existed, so `systemctl --user stop` orphaned the panel.
test_ui_signal() {
    reset_state
    cat > "$APP/bin/mirrorling-ui" <<'EOF'
#!/usr/bin/env bash
# Stand-in panel: answers /api/health 200 so cmd_ui's readiness probe passes.
port=""
while [ $# -gt 0 ]; do
    case "$1" in --addr) port="${2##*:}" ;; esac
    shift
done
printf '%s\n' "$$" > "${STUB_DIR}/ui.pid"
exec python3 -c '
import http.server, sys

class H(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.end_headers()
        self.wfile.write(b"ok\n")
    def log_message(self, *args):
        pass

http.server.HTTPServer(("127.0.0.1", int(sys.argv[1])), H).serve_forever()
' "$port"
EOF
    chmod +x "$APP/bin/mirrorling-ui"
    local freeport sam_pid uipid i out_file="$TEST_DIR/ui.out"
    freeport="$(free_port)"

    env -u DISPLAY -u WAYLAND_DISPLAY "$APP/mirrorling" ui --no-open --port "$freeport" \
        > "$out_file" 2>&1 &
    sam_pid=$!
    for i in $(seq 1 200); do
        if grep -q "mirrorling UI: http://127.0.0.1:$freeport" "$out_file" 2>/dev/null; then break; fi
        sleep 0.05
    done
    uipid="$(cat "$STUB_DIR/ui.pid" 2>/dev/null || true)"
    [ -n "$uipid" ] || fail "UI stub did not start: $(cat "$out_file")"
    kill -0 "$uipid" 2>/dev/null || fail "UI stub exited before the signal test"

    kill -TERM "$sam_pid" 2>/dev/null || true
    wait "$sam_pid" 2>/dev/null || true
    for i in $(seq 1 100); do
        kill -0 "$uipid" 2>/dev/null || break
        sleep 0.02
    done
    if kill -0 "$uipid" 2>/dev/null; then
        kill "$uipid" 2>/dev/null || true
        fail "SIGTERM to mirrorling ui left the server running"
    fi
    ok "SIGTERM to mirrorling ui takes the server down"
    assert_not_contains "$(cat "$out_file")" "unbound variable" "SIGTERM path has no unbound-variable error"

    # Server exits on its own (window closed): the EXIT trap runs after
    # cmd_ui's locals are gone — it must not read an out-of-scope variable.
    rm -f "$STUB_DIR/ui.pid"
    env -u DISPLAY -u WAYLAND_DISPLAY "$APP/mirrorling" ui --no-open --port "$freeport" \
        > "$out_file" 2>&1 &
    sam_pid=$!
    for i in $(seq 1 200); do
        if grep -q "mirrorling UI: http://127.0.0.1:$freeport" "$out_file" 2>/dev/null; then break; fi
        sleep 0.05
    done
    uipid="$(cat "$STUB_DIR/ui.pid" 2>/dev/null || true)"
    [ -n "$uipid" ] || fail "second UI stub did not start: $(cat "$out_file")"
    kill "$uipid" 2>/dev/null || true
    wait "$sam_pid" 2>/dev/null || true
    assert_not_contains "$(cat "$out_file")" "unbound variable" "server exit path has no unbound-variable error"
    kill -0 "$uipid" 2>/dev/null && fail "server exit left a live process" || true
    ok "mirrorling ui exits once its server is gone"
}

# --- run ---------------------------------------------------------------------
start_listeners

test_help_version
test_empty_default_and_guidance
test_tv_state_spaces_models
test_rescue_menu
test_config_missing_keys
test_toggle_off
test_toggle_state
test_env_fps_start
test_systemd_run_failure
test_stale_socket
test_picker_setup
test_picker_write_failure
test_tray_status
test_ui
test_ui_port_validation
test_ui_signal

printf '\nall %d checks passed\n' "$CHECKS"
