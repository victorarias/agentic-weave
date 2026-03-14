#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/remotecontrol-real-pi-pty-smoke.sh

Runs a relay-backed takeover smoke flow against a real interactive `pi` process:
  1. starts a PTY-backed wrapper with `--pty-bin pi --pty-arg=--no-session`
  2. verifies the wrapper actually launched a `pi` child process
  3. attaches a human in `takeover` mode
  4. sends raw `pty-input` (`/session` + Enter)
  5. verifies the attached stream receives the echoed PTY bytes
  6. verifies `pty-resize` updates session status
EOF
}

case "${1:-}" in
  -h|--help|help)
    usage
    exit 0
    ;;
  "") ;;
  *)
    usage >&2
    exit 2
    ;;
esac

require() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required command: $1" >&2
    exit 1
  }
}

require go
require pi
require python3
require rg
require ps

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmpdir="$(mktemp -d)"
relay_bin="$tmpdir/weave-relay"
wrapper_bin="$tmpdir/weave-wrapper"
inspect_bin="$tmpdir/weave-inspect"
relay_pid=""
wrapper_pid=""
attach_pid=""

cleanup() {
  for pid in "$attach_pid" "$wrapper_pid" "$relay_pid"; do
    if [[ -n "$pid" ]]; then
      kill -INT "$pid" >/dev/null 2>&1 || true
      sleep 1
      kill "$pid" >/dev/null 2>&1 || true
      wait "$pid" 2>/dev/null || true
    fi
  done
  rm -rf "$tmpdir"
}
trap cleanup EXIT

strip_ansi() {
  python3 - "$1" <<'PY'
import pathlib, re, sys
path = pathlib.Path(sys.argv[1])
data = path.read_text(errors='replace') if path.exists() else ''
data = re.sub(r'\x1b\[[0-9;?]*[ -/]*[@-~]', '', data)
data = re.sub(r'\x1b\].*?(\x07|\x1b\\)', '', data, flags=re.S)
data = data.replace('\r', '\n')
print(data)
PY
}

echo "==> building weave binaries"
(
  cd "$repo_root"
  go build -o "$relay_bin" ./cmd/weave-relay
  go build -o "$wrapper_bin" ./cmd/weave-wrapper
  go build -o "$inspect_bin" ./cmd/weave-inspect
)

port="$(${PYTHON:-python3} - <<'PY'
import socket
s = socket.socket()
s.bind(('127.0.0.1', 0))
print(s.getsockname()[1])
s.close()
PY
)"
relay_url="ws://127.0.0.1:${port}/ws"
session="real-pi-pty-demo-$(date +%s)-$$"
status_out="$tmpdir/status.out"
attach_out="$tmpdir/attach.out"
attach_err="$tmpdir/attach.err"

echo "==> starting weave-relay on ${relay_url}"
(
  cd "$repo_root"
  exec "$relay_bin" --addr "127.0.0.1:${port}" --public-url "$relay_url" --token dev-token >"$tmpdir/relay.log" 2>"$tmpdir/relay.err"
) &
relay_pid=$!
sleep 2

echo "==> starting PTY-backed wrapper with real pi for session ${session}"
(
  cd "$repo_root"
  exec "$wrapper_bin" \
    --relay "$relay_url" \
    --token dev-token \
    --session "$session" \
    --pty-bin pi \
    --pty-arg=--no-session >"$tmpdir/wrapper.log" 2>"$tmpdir/wrapper.err"
) &
wrapper_pid=$!

for _ in $(seq 1 20); do
  if (
    cd "$repo_root" &&
    "$inspect_bin" relay \
      --relay "$relay_url" \
      --token dev-token \
      --identity orch-1 \
      --session "$session" \
      status >"$status_out"
  ); then
    break
  fi
  sleep 1
done

ps -o command= --ppid "$wrapper_pid" >"$tmpdir/children.out"
rg '(^|[ /])pi([ =]|$)' "$tmpdir/children.out" >/dev/null

echo "==> attaching human takeover client"
(
  cd "$repo_root"
  exec "$inspect_bin" relay \
    --relay "$relay_url" \
    --token dev-token \
    --identity human-1 \
    --session "$session" \
    attach takeover >"$attach_out" 2>"$attach_err"
) &
attach_pid=$!
sleep 2

echo "==> sending /session through pty-input"
(
  cd "$repo_root"
  "$inspect_bin" relay \
    --relay "$relay_url" \
    --token dev-token \
    --identity human-1 \
    --session "$session" \
    pty-input $'/session\r'
)

for _ in $(seq 1 20); do
  stripped="$(strip_ansi "$attach_out")"
  if printf '%s' "$stripped" | rg '/session' >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
stripped="$(strip_ansi "$attach_out")"
printf '%s' "$stripped" | rg '/session' >/dev/null

echo "==> resizing PTY"
(
  cd "$repo_root"
  "$inspect_bin" relay \
    --relay "$relay_url" \
    --token dev-token \
    --identity human-1 \
    --session "$session" \
    pty-resize 30 90
)

(
  cd "$repo_root"
  "$inspect_bin" relay \
    --relay "$relay_url" \
    --token dev-token \
    --identity orch-1 \
    --session "$session" \
    status >"$status_out"
)
rg '^pty_rows=30$' "$status_out" >/dev/null
rg '^pty_cols=90$' "$status_out" >/dev/null

echo "==> detaching takeover client"
(
  cd "$repo_root"
  "$inspect_bin" relay \
    --relay "$relay_url" \
    --token dev-token \
    --identity human-1 \
    --session "$session" \
    detach >"$tmpdir/detach.out"
)

echo "Real interactive pi PTY smoke passed."
