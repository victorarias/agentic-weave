#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/remotecontrol-attach-smoke.sh

Runs a real pi -> weave-wrapper -> weave-relay -> weave-inspect smoke flow for
Tier 4 observe/inject behavior.
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
relay_log="$tmpdir/relay.log"
relay_err="$tmpdir/relay.err"
relay_pid=""
wrapper_pid=""
observer_pid=""

cleanup() {
  if [[ -n "$observer_pid" ]]; then
    kill -INT "$observer_pid" >/dev/null 2>&1 || true
    for _ in $(seq 1 10); do
      if ! kill -0 "$observer_pid" >/dev/null 2>&1; then
        break
      fi
      sleep 1
    done
    kill "$observer_pid" >/dev/null 2>&1 || true
    wait "$observer_pid" 2>/dev/null || true
  fi
  if [[ -n "$wrapper_pid" ]]; then
    kill "$wrapper_pid" >/dev/null 2>&1 || true
    wait "$wrapper_pid" 2>/dev/null || true
  fi
  if [[ -n "$relay_pid" ]]; then
    kill "$relay_pid" >/dev/null 2>&1 || true
    wait "$relay_pid" 2>/dev/null || true
  fi
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

echo "==> starting weave-relay on ${relay_url}"
(
  cd "$repo_root"
  exec "$relay_bin" --addr "127.0.0.1:${port}" --public-url "$relay_url" --token dev-token >"$relay_log" 2>"$relay_err"
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

observer_out="$tmpdir/observer.out"
observer_err="$tmpdir/observer.err"
status_out="$tmpdir/status.out"
prompt_out="$tmpdir/prompt.out"
inject_out="$tmpdir/inject.out"

echo "==> attaching observer"
(
  cd "$repo_root"
  exec "$inspect_bin" relay \
    --relay "$relay_url" \
    --token dev-token \
    --identity observer-1 \
    --session "$session" \
    attach observe >"$observer_out" 2>"$observer_err"
) &
observer_pid=$!
sleep 3

echo "==> checking attached observer in status"
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
    if rg '^attached_client_id=observer-1$' "$status_out" >/dev/null 2>&1 && rg '^attached_mode=observe$' "$status_out" >/dev/null 2>&1; then
      break
    fi
  fi
  sleep 1
done
rg '^attached_client_id=observer-1$' "$status_out" >/dev/null
rg '^attached_mode=observe$' "$status_out" >/dev/null

echo "==> sending orchestrator prompt while observer watches"
(
  cd "$repo_root"
  "$inspect_bin" relay \
    --relay "$relay_url" \
    --token dev-token \
    --identity orch-1 \
    --session "$session" \
    prompt "Reply with exactly OBSERVE_OK." >"$prompt_out"
)
rg 'OBSERVE_OK' "$prompt_out" >/dev/null

for _ in $(seq 1 30); do
  if rg 'OBSERVE_OK' "$observer_out" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
rg 'OBSERVE_OK' "$observer_out" >/dev/null

echo "==> stopping observer"
kill -INT "$observer_pid" >/dev/null 2>&1 || true
for _ in $(seq 1 10); do
  if ! kill -0 "$observer_pid" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
kill "$observer_pid" >/dev/null 2>&1 || true
wait "$observer_pid" 2>/dev/null || true
observer_pid=""

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
  echo "FAILED: expected observer attachment to clear before inject" >&2
  cat "$status_out" >&2 || true
  exit 1
fi

echo "==> running human inject flow"
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

echo "Observe/inject smoke passed."
