#!/usr/bin/env bash
# The end-to-end tests' server (make serve-test, which Playwright starts):
# bin/api on E2E_PORT against its own database, lewcontext_test_<port>,
# created if missing and rebuilt from scratch on every start, so each run
# begins empty and a changed create migration always applies. Run through
# scripts/test-env.sh, so its settings come from .env.testing. Reserves the
# port itself unless make test-e2e already did (E2E_LOCK_HELD).
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
# shellcheck source=scripts/e2e-lib.sh
source scripts/e2e-lib.sh

port="${E2E_PORT:-3100}"
if ! e2e_valid_port "$port"; then
  echo "E2E_PORT=$port: give a port from 1024 to 65535 (make test-e2e E2E_PORT=auto picks one)." >&2
  exit 1
fi

# Only ever rebuild a local *_test_<port> database, never the Go tests'
# lewcontext_test or anything else.
database_url="$(e2e_database_url "$DATABASE_URL" "$port")"
database="$(e2e_database_name "$database_url")"
read -r host _ <<< "$(e2e_database_host "$database_url")"
if [[ ! $database =~ ^[a-z0-9_]+_test_[0-9]+$ ]]; then
  echo "Refusing to rebuild $database: the e2e database must be named *_test_<port>." >&2
  exit 1
fi
case "$host" in
  127.0.0.1 | ::1 | localhost | postgres) ;;
  *)
    echo "Refusing to rebuild a database on $host: the e2e database must be on this machine." >&2
    exit 1
    ;;
esac

if [[ ${E2E_LOCK_HELD:-} != 1 ]]; then
  e2e_require_flock
  e2e_reserve "$port" || e2e_clash "$port"
fi
if e2e_port_in_use "$port"; then
  e2e_clash "$port"
fi
e2e_require_database
if [[ ! -x bin/api ]]; then
  echo "bin/api is missing: run make serve-test, which builds it first." >&2
  exit 1
fi
if ! command -v psql > /dev/null; then
  echo "psql is missing: the e2e tests need the PostgreSQL client (Ubuntu: sudo apt install postgresql-client)." >&2
  exit 1
fi

# $database matched the pattern above, so it's safe in the statement.
if ! psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -tAc "SELECT 1 FROM pg_database WHERE datname = '$database'" | grep -q 1; then
  psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -qc "CREATE DATABASE $database"
fi
(cd apps/api && DATABASE_URL="$database_url" go run ./cmd/migrate fresh -confirm)

export DATABASE_URL="$database_url" API_ADDR="127.0.0.1:$port" APP_ORIGIN="http://localhost:$port"
echo "E2E server: http://localhost:$port · database $database · mailbox $MAIL_FILE"
exec ./bin/api
