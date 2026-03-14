#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/remotecontrol-attach-smoke.sh

Runs a real pi -> weave-wrapper -> weave-relay -> weave-inspect smoke flow for
Tier 4 ergonomics:
  1. read-only watch streaming without taking the attach lock
  2. observe attachment visibility in session status
  3. same-identity observe -> inject escalation across reconnects
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

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmpdir="$(mktemp -d)"
wrapper_bin="$tmpdir/weave-wrapper"
relay_bin="$tmpdir/weave-relay"
inspect_bin="$tmpdir/weave-inspect"
relay_pid=""
wrapper_pid=""
watch_pid=""
observe_pid=""

cleanup() {
  for pid in "$observe_pid" "$watch_pid" "$wrapper_pid" "$relay_pid"; do
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
  go build -o "$wrapper_bin" ./cmd/weave-wrapper
  go build -o "$relay_bin" ./cmd/weave-relay
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
session="attach-demo-$(date +%s)-$$"
status_out="$tmpdir/status.out"
watch_out="$tmpdir/watch.out"
watch_err="$tmpdir/watch.err"
observe_out="$tmpdir/observe.out"
observe_err="$tmpdir/observe.err"
inject_out="$tmpdir/inject.out"

echo "==> starting weave-relay on ${relay_url}"
(
  cd "$repo_root"
  exec "$relay_bin" --addr "127.0.0.1:${port}" --public-url "$relay_url" --token dev-token >"$tmpdir/relay.log" 2>"$tmpdir/relay.err"
) &
relay_pid=$!
sleep 2

echo "==> starting weave-wrapper for session ${session}"
(
  cd "$repo_root"
  exec "$wrapper_bin" --relay "$relay_url" --token dev-token --session "$session" >"$tmpdir/wrapper.log" 2>"$tmpdir/wrapper.err"
) &
wrapper_pid=$!
sleep 3

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

echo "==> starting passive watch"
(
  cd "$repo_root"
  exec "$inspect_bin" relay \
    --relay "$relay_url" \
    --token dev-token \
    --identity watcher-1 \
    --session "$session" \
    watch >"$watch_out" 2>"$watch_err"
) &
watch_pid=$!
sleep 2

echo "==> attaching observe client"
(
  cd "$repo_root"
  exec "$inspect_bin" relay \
    --relay "$relay_url" \
    --token dev-token \
    --identity human-1 \
    --session "$session" \
    attach observe >"$observe_out" 2>"$observe_err"
) &
observe_pid=$!

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
    if rg '^attached_client_id=human-1$' "$status_out" >/dev/null 2>&1 && rg '^attached_mode=observe$' "$status_out" >/dev/null 2>&1; then
      break
    fi
  fi
  sleep 1
done
rg '^attached_client_id=human-1$' "$status_out" >/dev/null
rg '^attached_mode=observe$' "$status_out" >/dev/null

if ! kill -0 "$watch_pid" >/dev/null 2>&1; then
  echo "FAILED: watch process exited unexpectedly" >&2
  cat "$watch_err" >&2 || true
  exit 1
fi

echo "==> escalating to inject with same identity"
(
  cd "$repo_root"
  "$inspect_bin" relay \
    --relay "$relay_url" \
    --token dev-token \
    --identity human-1 \
    --session "$session" \
    inject "Reply with exactly INJECT_OK." >"$inject_out"
)
rg 'INJECT_OK' "$inject_out" >/dev/null

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
    if ! rg '^attached_client_id=' "$status_out" >/dev/null 2>&1; then
      break
    fi
  fi
  sleep 1
done
if rg '^attached_client_id=' "$status_out" >/dev/null 2>&1; then
  echo "FAILED: expected no attached client after one-shot inject" >&2
  cat "$status_out" >&2
  exit 1
fi

for _ in $(seq 1 20); do
  if rg 'attachment] action=mode_changed client=human-1 mode=inject previous_mode=observe' "$watch_err" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
rg 'attachment] action=mode_changed client=human-1 mode=inject previous_mode=observe' "$watch_err" >/dev/null

echo "Observe/watch/inject smoke passed."
