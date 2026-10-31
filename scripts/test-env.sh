#!/usr/bin/env bash
# Runs a command with the test settings in .env.testing and nothing else. The
# environment is emptied first, so values from .env (which make exports to
# every target) never reach a test. Kept from the caller: what finds the tools
# (PATH, HOME, Go's GO* settings, CC), CI, CHROMIUM_PATH, FAKER_SEED and
# PLAYWRIGHT_*, and the run's own E2E_PORT, E2E_REUSE, E2E_LOCK_HELD,
# E2E_SETTINGS_PORT (e2e-preflight.sh reserves it), E2E_ADMIN_ROUTE_KEY and
# E2E_APP_KEY (playwright.config.ts makes one of each per run).
# Used by make test, make test-e2e and make serve-test.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
if [[ $# -eq 0 ]]; then
  echo "Usage: scripts/test-env.sh <command> [args…]" >&2
  exit 2
fi

settings=()
while IFS= read -r line || [[ -n $line ]]; do
  [[ -z $line || $line == \#* ]] && continue
  settings+=("$line")
done < "$root/.env.testing"

kept=()
for name in $(compgen -e); do
  case "$name" in
    PATH | HOME | USER | TMPDIR | TERM | CI | CC | CHROMIUM_PATH | FAKER_SEED | GO* | PLAYWRIGHT_*) ;;
    E2E_PORT | E2E_REUSE | E2E_LOCK_HELD | E2E_SETTINGS_PORT | E2E_ADMIN_ROUTE_KEY | E2E_APP_KEY) [[ -n ${!name} ]] || continue ;;
    *) continue ;;
  esac
  kept+=("$name=${!name}")
done

# Later assignments win, so a caller's E2E_PORT replaces the file's.
exec env -i "${settings[@]}" "${kept[@]}" "$@"
