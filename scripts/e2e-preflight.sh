#!/usr/bin/env bash
# The first step of make test-e2e, run before anything is built: checks the
# test database and Mailpit are up, reserves the run's port (E2E_PORT, 3100 by
# default, or auto for the first free one from 3101), then runs the command
# it's given (Playwright) holding that reservation. A port another run has stops it at
# once, with a free port to use instead. With E2E_REUSE=1 it reserves nothing:
# Playwright tests the server you already started on E2E_PORT.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=scripts/e2e-lib.sh
source "$root/scripts/e2e-lib.sh"

port="${E2E_PORT:-3100}"
if [[ ${E2E_REUSE:-} == 1 ]]; then
  if ! e2e_valid_port "$port"; then
    echo "E2E_REUSE=1 tests a server you started, so give its port: make test-e2e E2E_REUSE=1 E2E_PORT=3100" >&2
    exit 1
  fi
  echo "E2E run: reusing the server on http://localhost:$port"
  E2E_PORT=$port exec "$@"
fi

e2e_require_flock
e2e_require_database
e2e_require_mailpit

if [[ $port == auto ]]; then
  port=""
  for candidate in $(seq 3101 3199); do
    if e2e_reserve "$candidate"; then
      if ! e2e_port_in_use "$candidate"; then
        port=$candidate
        break
      fi
      exec {E2E_LOCK_FD}>&-
    fi
  done
  if [[ -z $port ]]; then
    echo "No free port between 3101 and 3199 for an e2e run." >&2
    exit 1
  fi
elif ! e2e_valid_port "$port"; then
  echo "E2E_PORT=$port: give a port from 1024 to 65535, or auto." >&2
  exit 1
elif ! e2e_reserve "$port" || e2e_port_in_use "$port"; then
  e2e_clash "$port"
fi

echo "E2E run: app http://localhost:$port · database $(e2e_database_name "$(e2e_database_url "$DATABASE_URL" "$port")")"
E2E_PORT=$port E2E_LOCK_HELD=1 exec "$@"
