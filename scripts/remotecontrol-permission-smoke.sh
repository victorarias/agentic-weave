#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/remotecontrol-permission-smoke.sh [allow|deny|both]

Runs a real pi -> weave-wrapper -> weave-relay -> weave-inspect permission smoke
flow using testdata/pi/permission_fixture.ts.

Examples:
  scripts/remotecontrol-permission-smoke.sh
  scripts/remotecontrol-permission-smoke.sh allow
  scripts/remotecontrol-permission-smoke.sh deny
EOF
}

mode="${1:-both}"
case "$mode" in
  allow|deny|both) ;;
  -h|--help|help)
    usage
    exit 0
    ;;
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
fixture="$repo_root/testdata/pi/permission_fixture.ts"
if [[ ! -f "$fixture" ]]; then
  echo "missing fixture extension: $fixture" >&2
  exit 1
fi

tmpdir="$(mktemp -d)"
wrapper_bin="$tmpdir/weave-wrapper"
relay_log="$tmpdir/relay.log"
relay_err="$tmpdir/relay.err"
wrapper_pid=""
relay_pid=""

cleanup() {
  if [[ -n "$wrapper_pid" ]]; then
    kill "$wrapper_pid" >/dev/null 2>&1 || true
  fi
  if [[ -n "$relay_pid" ]]; then
    kill "$relay_pid" >/dev/null 2>&1 || true
  fi
  rm -rf "$tmpdir"
}
trap cleanup EXIT

echo "==> building weave-wrapper"
(
  cd "$repo_root"
  go build -o "$wrapper_bin" ./cmd/weave-wrapper
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

echo "==> starting weave-relay on ${relay_url}"
(
  cd "$repo_root"
  go run ./cmd/weave-relay --addr "127.0.0.1:${port}" --public-url "$relay_url" --token dev-token >"$relay_log" 2>"$relay_err"
) &
relay_pid=$!
sleep 2

run_case() {
  local decision="$1"
  local expected="$2"
  local session="perm-${decision}-$(date +%s)-$$"
  local wrapper_log="$tmpdir/${decision}-wrapper.log"
  local wrapper_err="$tmpdir/${decision}-wrapper.err"
  local prompt_out="$tmpdir/${decision}-prompt.out"
  local prompt_err="$tmpdir/${decision}-prompt.err"
  local req=""
  local prompt_pid=""

  echo
  echo "==> starting wrapper for session ${session}"
  (
    cd "$repo_root"
    go run ./cmd/weave-wrapper \
      --relay "$relay_url" \
      --token dev-token \
      --session "$session" \
      --pi-arg=--extension \
      --pi-arg="$fixture" >"$wrapper_log" 2>"$wrapper_err"
  ) &
  wrapper_pid=$!
  sleep 2

  echo "==> sending /permtest (${decision})"
  (
    cd "$repo_root"
    go run ./cmd/weave-inspect relay \
      --relay "$relay_url" \
      --token dev-token \
      --session "$session" \
      prompt "/permtest" >"$prompt_out" 2>"$prompt_err"
  ) &
  prompt_pid=$!

  for _ in $(seq 1 60); do
    if rg -o 'id=([^ ]+)' "$prompt_err" >/dev/null 2>&1; then
      req="$(rg -o 'id=([^ ]+)' "$prompt_err" | head -n1 | cut -d= -f2)"
      break
    fi
    sleep 1
  done

  if [[ -z "$req" ]]; then
    echo "FAILED: no permission request observed for ${decision}" >&2
    echo "--- prompt stderr ---" >&2
    cat "$prompt_err" >&2 || true
    echo "--- wrapper stderr ---" >&2
    cat "$wrapper_err" >&2 || true
    echo "--- relay stderr ---" >&2
    cat "$relay_err" >&2 || true
    exit 1
  fi

  echo "==> observed permission request id=${req}"

  local status_out="$tmpdir/${decision}-status.out"
  (
    cd "$repo_root"
    go run ./cmd/weave-inspect relay \
      --relay "$relay_url" \
      --token dev-token \
      --session "$session" \
      status >"$status_out"
  )
  if ! rg '^state=waiting_permission$' "$status_out" >/dev/null 2>&1; then
    echo "FAILED: expected waiting_permission state during pending permission" >&2
    cat "$status_out" >&2 || true
    exit 1
  fi
  if ! rg "^pending_permission_id=${req} " "$status_out" >/dev/null 2>&1; then
    echo "FAILED: expected pending permission id ${req} in status output" >&2
    cat "$status_out" >&2 || true
    exit 1
  fi

  (
    cd "$repo_root"
    go run ./cmd/weave-inspect relay \
      --relay "$relay_url" \
      --token dev-token \
      --session "$session" \
      "$decision" "$req"
  )

  wait "$prompt_pid"

  local actual
  actual="$(tr -d '\r' <"$prompt_out" | rg -o 'PERMISSION_(ALLOWED|DENIED)' | tail -n1 || true)"
  if [[ "$actual" != "$expected" ]]; then
    echo "FAILED: expected ${expected}, got ${actual:-<empty>}" >&2
    echo "--- prompt stdout ---" >&2
    cat "$prompt_out" >&2 || true
    echo "--- prompt stderr ---" >&2
    cat "$prompt_err" >&2 || true
    exit 1
  fi

  echo "==> ${decision} flow passed (${actual})"
  echo "--- prompt stderr (${decision}) ---"
  cat "$prompt_err"

  kill "$wrapper_pid" >/dev/null 2>&1 || true
  wait "$wrapper_pid" 2>/dev/null || true
  wrapper_pid=""
}

case "$mode" in
  allow)
    run_case allow PERMISSION_ALLOWED
    ;;
  deny)
    run_case deny PERMISSION_DENIED
    ;;
  both)
    run_case allow PERMISSION_ALLOWED
    run_case deny PERMISSION_DENIED
    ;;
esac

echo
echo "All requested permission smoke flows passed."
