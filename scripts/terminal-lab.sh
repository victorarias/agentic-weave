#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/terminal-lab.sh [quick|full|scenario <regex>]

Runs the terminal lab regression suite.

Modes:
  quick                Run the current terminaltests package.
  full                 Same as quick today; reserved for larger matrix expansion.
  scenario <regex>     Run matching terminaltests names only.

Environment:
  WEAVE_TERMINAL_ARTIFACTS_DIR   Where failure artifacts and transcripts are written.
                                 Default: .artifacts/terminaltests

Examples:
  scripts/terminal-lab.sh quick
  scripts/terminal-lab.sh scenario TestTUITakeoverDisconnectEncodings/csi-u
EOF
}

mode="${1:-quick}"
case "$mode" in
  -h|--help|help)
    usage
    exit 0
    ;;
  quick)
    shift || true
    export WEAVE_TERMINAL_TESTS=1
    exec go test -v ./terminaltests
    ;;
  full)
    shift || true
    export WEAVE_TERMINAL_TESTS=1
    exec go test -v ./terminaltests
    ;;
  scenario)
    shift
    if [[ $# -lt 1 ]]; then
      echo "scenario mode requires a regex" >&2
      exit 2
    fi
    export WEAVE_TERMINAL_TESTS=1
    exec go test -v ./terminaltests -run "$1"
    ;;
  *)
    usage >&2
    exit 2
    ;;
esac
