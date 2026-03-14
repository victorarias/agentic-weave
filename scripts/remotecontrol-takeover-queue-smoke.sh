#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/remotecontrol-takeover-queue-smoke.sh

Runs a real pi -> weave-wrapper -> weave-relay -> weave-inspect smoke flow for
initial Tier 5 takeover groundwork:
  1. a human attaches in `takeover` mode
  2. orchestrator prompts are acknowledged as queued instead of delivered immediately
  3. queued prompt count appears in session status
  4. detaching the human flushes the queued prompt back to the runtime
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
relay_bin="$tmpdir/weave-relay"
wrapper_bin="$tmpdir/weave-wrapper"
inspect_bin="$tmpdir/weave-inspect"
relay_pid=""
wrapper_pid=""
watch_pid=""
takeover_pid=""

cleanup() {
  for pid in "$takeover_pid" "$watch_pid" "$wrapper_pid" "$relay_pid"; do
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
session="takeover-demo-$(date +%s)-$$"
status_out="$tmpdir/status.out"
watch_out="$tmpdir/watch.out"
watch_err="$tmpdir/watch.err"
prompt_out="$tmpdir/prompt.out"

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
sleep 1

echo "==> attaching human in takeover mode"
(
  cd "$repo_root"
  exec "$inspect_bin" relay \
    --relay "$relay_url" \
    --token dev-token \
    --identity human-1 \
    --session "$session" \
    attach takeover >"$tmpdir/takeover.out" 2>"$tmpdir/takeover.err"
) &
takeover_pid=$!

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
    if rg '^attached_client_id=human-1$' "$status_out" >/dev/null 2>&1 && rg '^attached_mode=takeover$' "$status_out" >/dev/null 2>&1; then
      break
    fi
  fi
  sleep 1
done
rg '^attached_client_id=human-1$' "$status_out" >/dev/null
rg '^attached_mode=takeover$' "$status_out" >/dev/null

echo "==> sending orchestrator prompt during takeover"
(
  cd "$repo_root"
  "$inspect_bin" relay \
    --relay "$relay_url" \
    --token dev-token \
    --identity orch-1 \
    --session "$session" \
    prompt "Reply with exactly TAKEOVER_QUEUE_OK." >"$prompt_out"
)
rg '^queued=true$' "$prompt_out" >/dev/null
rg '^queued_prompts=1$' "$prompt_out" >/dev/null

(
  cd "$repo_root"
  "$inspect_bin" relay \
    --relay "$relay_url" \
    --token dev-token \
    --identity orch-1 \
    --session "$session" \
    status >"$status_out"
)
rg '^queued_prompts=1$' "$status_out" >/dev/null

echo "==> detaching takeover human"
(
  cd "$repo_root"
  "$inspect_bin" relay \
    --relay "$relay_url" \
    --token dev-token \
    --identity human-1 \
    --session "$session" \
    detach >"$tmpdir/detach.out"
)

for _ in $(seq 1 30); do
  if rg 'TAKEOVER_QUEUE_OK' "$watch_out" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
rg 'TAKEOVER_QUEUE_OK' "$watch_out" >/dev/null

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
    if rg '^queued_prompts=0$' "$status_out" >/dev/null 2>&1 && ! rg '^attached_client_id=' "$status_out" >/dev/null 2>&1; then
      break
    fi
  fi
  sleep 1
done
rg '^queued_prompts=0$' "$status_out" >/dev/null
if rg '^attached_client_id=' "$status_out" >/dev/null 2>&1; then
  echo "FAILED: expected session to be detached after takeover release" >&2
  cat "$status_out" >&2
  exit 1
fi

echo "Takeover queue smoke passed."
