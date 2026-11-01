#!/usr/bin/env bash
# Builds the web app twice and copies both builds where the Go build embeds
# them: the app build (react-router build, prerendering the public pages)
# into apps/api/internal/web/dist, and the admin build (WEB_BUILD=admin) into
# apps/api/internal/web/admin, which the server hands out only behind
# ADMIN_ROUTE_KEY (docs/api/admin.md). Then it checks that no admin code
# reached the app build, and fails if any did.
# Used by `make build` (through make web-build) and scripts/dev.sh.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
web=apps/api/internal/web

# Replaces a directory's previous build; .gitkeep keeps the directory in git.
# The build manifest (.vite) is for tooling, not for serving.
install_build() {
  local from=$1 to=$2
  find "$to" -mindepth 1 ! -name .gitkeep -exec rm -rf {} +
  cp -R "$from/." "$to/"
  rm -rf "$to/.vite"
}

(cd apps/web && WEB_BUILD=app npm run build)
(cd apps/web && WEB_BUILD=admin npm run build)
./scripts/check-app-build.sh apps/web/build/client
install_build apps/web/build/client "$web/dist"
install_build apps/web/build/admin/client "$web/admin"
