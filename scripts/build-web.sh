#!/usr/bin/env bash
# Builds the web app (react-router build, prerendering the public pages) and
# copies it into apps/api/internal/web/dist, where the Go build embeds it.
# Used by `make build` (through make web-build) and scripts/dev.sh.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
dist=apps/api/internal/web/dist

(cd apps/web && npm run build)
# Replace the previous build; .gitkeep keeps the directory in git.
find "$dist" -mindepth 1 ! -name .gitkeep -exec rm -rf {} +
cp -R apps/web/build/client/. "$dist/"
# The build manifest is for tooling, not for serving.
rm -rf "$dist/.vite"
