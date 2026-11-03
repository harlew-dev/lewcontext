#!/bin/sh
# Tests scripts/check-app-build.sh: a clean build passes, and each admin
# marker, in another case too, in a script or a binary file, fails it.
# Run by make check.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT INT TERM
failures=0

# fresh: an app build without admin code.
fresh() {
  rm -rf "$work/build"
  mkdir -p "$work/build/assets"
  printf 'const routes={"pages/home":{}};fetch("/api/auth/me")' > "$work/build/assets/home.js"
  printf '<html><body><a href="/app/settings">Settings</a></body></html>' > "$work/build/index.html"
}

expect() {
  want=$1 name=$2
  if sh "$here/check-app-build.sh" "$work/build" > /dev/null 2>&1; then got=0; else got=1; fi
  if [ "$got" != "$want" ]; then
    echo "check-app-build.sh, $name: exit $got, want $want" >&2
    failures=$((failures + 1))
  fi
}

fresh
expect 0 "a clean build"
for marker in /admin/ /ADMIN/x /api/admin /Api/Admin X-Admin-Key x-admin-key pages/admin Pages/Admin admin-assets ADMIN-ASSETS; do
  fresh
  printf 'const a="%s";' "$marker" >> "$work/build/assets/home.js"
  expect 1 "$marker in a script"
  fresh
  printf '\000\001%s\002' "$marker" > "$work/build/assets/font.woff2"
  expect 1 "$marker in a binary file"
done
rm -rf "$work/build"
expect 1 "no build at all"

if [ "$failures" -gt 0 ]; then
  exit 1
fi
echo "check-app-build.sh: every marker fails the build."
