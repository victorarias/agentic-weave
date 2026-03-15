#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/remotecontrol-pty-smoke.sh

Runs a relay-backed PTY smoke flow using the tracked echo helper at
`testdata/pty/echo.py` to validate initial PTY transport plumbing:
  1. human attaches in takeover mode
  2. attached human receives PTY output
  3. same identity can send `pty-input`
  4. `pty-resize` is accepted while takeover is active
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
require python3
require rg

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
echo_helper="$repo_root/testdata/pty/echo.py"
if [[ ! -f "$echo_helper" ]]; then
  echo "missing PTY echo helper: $echo_helper" >&2
  exit 1
fi

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
session="pty-demo-$(date +%s)-$$"
attach_out="$tmpdir/attach.out"
status_out="$tmpdir/status.out"

echo "==> starting weave-relay on ${relay_url}"
(
  cd "$repo_root"
  exec "$relay_bin" --addr "127.0.0.1:${port}" --public-url "$relay_url" --token dev-token >"$tmpdir/relay.log" 2>"$tmpdir/relay.err"
) &
relay_pid=$!
sleep 2

echo "==> starting PTY-backed wrapper for session ${session}"
(
  cd "$repo_root"
  exec "$wrapper_bin" \
    --relay "$relay_url" \
    --token dev-token \
    --session "$session" \
    --pty-bin python3 \
    --pty-arg "$echo_helper" >"$tmpdir/wrapper.log" 2>"$tmpdir/wrapper.err"
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

echo "==> attaching human takeover client"
(
  cd "$repo_root"
  exec "$inspect_bin" relay \
    --relay "$relay_url" \
    --token dev-token \
    --identity human-1 \
    --session "$session" \
    attach takeover >"$attach_out" 2>"$tmpdir/attach.err"
) &
attach_pid=$!

sleep 2

echo "==> sending pty input"
(
  cd "$repo_root"
  "$inspect_bin" relay \
    --relay "$relay_url" \
    --token dev-token \
    --identity human-1 \
    --session "$session" \
    pty-input "HELLO_PTY"
)

for _ in $(seq 1 20); do
  if rg 'HELLO_PTY' "$attach_out" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
rg 'HELLO_PTY' "$attach_out" >/dev/null

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
    --identity human-1 \
    --session "$session" \
    detach >"$tmpdir/detach.out"
)

echo "PTY smoke passed."
