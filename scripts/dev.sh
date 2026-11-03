#!/usr/bin/env bash
# Runs the development instance in one terminal. First it rebuilds the web
# app the Go binary embeds (scripts/build-web.sh, both builds), so the API
# never serves an old build. Then it runs the Go API on API_ADDR
# (127.0.0.1:8080), Vite on http://localhost:3000, which forwards /api to the
# API and reloads the page on save, and the admin build's Vite on
# 127.0.0.1:3002, which :3000 forwards /admin and /admin-assets to. Output is
# prefixed [build] / [api] / [web] / [admin].
# Ctrl-C stops all three; if any one exits, the others are stopped too.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

if [[ ! -f .env ]]; then
  echo "No .env. Create it with: cp .env.example .env" >&2
  exit 1
fi
if [[ ! -d apps/web/node_modules ]]; then
  echo "Dependencies are missing. Run: make install" >&2
  exit 1
fi
set -a
# shellcheck source=/dev/null
source .env
set +a

# Every port must be free: a second API or Vite would fail to start anyway.
in_use() { (exec 3<>"/dev/tcp/127.0.0.1/$1") 2>/dev/null; }
for port in "${API_ADDR##*:}" 3000 3002; do
  if in_use "$port"; then
    echo "Port $port is already in use. Stop the other server (make api, make web, make web-admin or make serve) first." >&2
    exit 1
  fi
done

label() { while IFS= read -r line; do printf '%s %s\n' "$1" "$line"; done; }

# go run below embeds whatever apps/api/internal/web/dist holds.
echo "Building the web app the API embeds…"
./scripts/build-web.sh 2>&1 | label "[build]"

# Every process started here shares this script's process group, so one
# signal to the group stops the API, both Vites and the output prefixers.
trap 'trap - INT TERM EXIT; kill 0 2>/dev/null || true' INT TERM EXIT

(cd apps/api && exec go run ./cmd/server) > >(label "[api]") 2>&1 &
api=$!
(cd apps/web && exec npm run dev) > >(label "[web]") 2>&1 &
web=$!
(cd apps/web && exec npm run dev:admin) > >(label "[admin]") 2>&1 &
admin=$!

echo "${APP_NAME:-lewcontext} dev: http://localhost:3000 (API on $API_ADDR, admin pages from 127.0.0.1:3002). Ctrl-C stops all three."
while kill -0 "$api" 2>/dev/null && kill -0 "$web" 2>/dev/null && kill -0 "$admin" 2>/dev/null; do
  sleep 1
done
echo "A server stopped; stopping the others." >&2
