#!/bin/sh
# Fails if the app build (the files every visitor can download) holds any
# admin code: an admin page's URL (/admin/…) or route, the admin API's paths
# or header, or the admin build's file path, in any case. ESLint stops the
# imports that would pull it in (apps/web/eslint.config.mjs) and the app
# build itself fails on any admin module (no-admin-modules, vite.config.ts);
# this reads the output for anything that isn't a module, such as a string
# pasted somewhere. Run by scripts/build-web.sh (make build,
# make dev, the e2e server) and the Dockerfile, so a leak fails the build.
# POSIX sh, for the Docker image's Alpine stage.
#   scripts/check-app-build.sh apps/web/build/client
set -eu

build=${1:?usage: scripts/check-app-build.sh <app build directory>}
if [ ! -d "$build" ]; then
  echo "No app build at $build." >&2
  exit 1
fi

# -l lists matching files, binary ones (fonts) included; -F matches
# literally, -i in any case. /admin/ covers /api/admin/… too.
if leaks=$(grep -rliF -e /admin/ -e /api/admin -e X-Admin-Key -e pages/admin -e admin-assets "$build"); then
  echo "Admin code reached the app build. These files mention /admin/, /api/admin, X-Admin-Key, pages/admin or admin-assets (in any case):" >&2
  echo "$leaks" | sed 's/^/  /' >&2
  echo "Keep admin code in the admin modules (plans/features/admin/README.md, rule 7)." >&2
  exit 1
fi
echo "App build checked: no admin code."
