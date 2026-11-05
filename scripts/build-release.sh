#!/usr/bin/env bash
# Construieste arhivele unui release in dist/: hobstack_<versiune>_<os>_<arch>.tar.gz (.zip pe Windows)
# si checksums.txt (sha256). Il ruleaza GitHub Actions la push-ul unui tag v*; merge si local.
#   scripts/build-release.sh 1.0.0
set -euo pipefail
version="${1:?usage: scripts/build-release.sh <version, e.g. 1.0.0>}"
version="${version#v}"
cd "$(dirname "$0")/.."
rm -rf dist && mkdir -p dist
targets="darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64"
for t in $targets; do
  os="${t%/*}"; arch="${t#*/}"
  name="hobstack_${version}_${os}_${arch}"
  dir="dist/$name"
  mkdir -p "$dir"
  bin="hobstack"; [ "$os" = windows ] && bin="hobstack.exe"
  echo "building $name"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath \
    -ldflags "-s -w -X hobstack/internal/config.Version=${version}" -o "$dir/$bin" ./cmd/hobstack
  cp README.md LICENSE "$dir/"
  if [ "$os" = windows ]; then
    (cd "$dir" && zip -q -r "../$name.zip" .)
  else
    tar -C "$dir" -czf "dist/$name.tar.gz" .
  fi
  rm -rf "$dir"
done
cd dist
if command -v sha256sum >/dev/null; then sha256sum hobstack_* > checksums.txt; else shasum -a 256 hobstack_* > checksums.txt; fi
cat checksums.txt
