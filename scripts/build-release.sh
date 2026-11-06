#!/usr/bin/env bash
# Construieste un release in dist/: hobstack_<versiune>_<os>_<arch>.tar.gz pentru Linux (.zip pe Windows),
# installerul Windows HobStack-Setup-<versiune>.exe (cu makensis) si checksums.txt (sha256). Versiunea de macOS
# (tar.gz-urile darwin si HobStack-<versiune>.dmg) cere un Mac: scripts/build-mac.sh, chemat de aici pe macOS.
# In GitHub Actions macOS se face pe un runner separat (SKIP_MAC=1 si SKIP_CHECKSUMS=1 aici, checksums.txt la final).
#   scripts/build-release.sh 1.0.0
set -euo pipefail
version="${1:?usage: scripts/build-release.sh <version, e.g. 1.0.0>}"
version="${version#v}"
cd "$(dirname "$0")/.."
rm -rf dist && mkdir -p dist
targets="linux/amd64 linux/arm64 windows/amd64"
for t in $targets; do
  os="${t%/*}"; arch="${t#*/}"
  name="hobstack_${version}_${os}_${arch}"
  dir="dist/$name"
  mkdir -p "$dir"
  bin="hobstack"; [ "$os" = windows ] && bin="hobstack.exe"
  echo "building $name"
  # Windows: aplicatie GUI (-H windowsgui), fara fereastra de consola; vezi cmd/hobstack/console_windows.go
  gui=""; [ "$os" = windows ] && gui="-H windowsgui"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath \
    -ldflags "-s -w $gui -X hobstack/internal/config.Version=${version}" -o "$dir/$bin" ./cmd/hobstack
  cp README.md LICENSE "$dir/"
  if [ "$os" = windows ]; then
    (cd "$dir" && zip -q -r "../$name.zip" .)
    # installerul (NSIS): in CI e obligatoriu (REQUIRE_INSTALLER=1), local doar daca exista makensis
    if command -v makensis >/dev/null; then
      echo "building HobStack-Setup-${version}.exe"
      makensis -V2 -DVERSION="$version" -DSRC="$PWD/$dir" -DOUTFILE="$PWD/dist/HobStack-Setup-${version}.exe" installer/hobstack.nsi
    elif [ "${REQUIRE_INSTALLER:-}" = 1 ]; then
      echo "makensis is required for the Windows installer" >&2; exit 1
    else
      echo "makensis not found: skipping the Windows installer"
    fi
  else
    tar -C "$dir" -czf "dist/$name.tar.gz" .
  fi
  rm -rf "$dir"
done
if [ "$(uname)" = Darwin ] && [ "${SKIP_MAC:-}" != 1 ]; then
  scripts/build-mac.sh "$version"
fi
[ "${SKIP_CHECKSUMS:-}" = 1 ] || scripts/checksums.sh
