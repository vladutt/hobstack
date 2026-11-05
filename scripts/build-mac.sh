#!/usr/bin/env bash
# Construieste pe macOS (cu cgo, pentru bara de meniu): hobstack_<v>_darwin_<arch>.tar.gz (arm64, amd64; update-ul
# din aplicatie le foloseste) si HobStack-<v>.dmg cu HobStack.app universal (Apple Silicon + Intel).
#   scripts/build-mac.sh 1.0.5
set -euo pipefail
version="${1:?usage: scripts/build-mac.sh <version>}"
version="${version#v}"
cd "$(dirname "$0")/.."
[ "$(uname)" = Darwin ] || { echo "scripts/build-mac.sh needs macOS (cgo + Cocoa)" >&2; exit 1; }
mkdir -p dist
rm -f dist/hobstack_*_darwin_* dist/HobStack-*.dmg
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
export MACOSX_DEPLOYMENT_TARGET=11.0
bins=()
for arch in arm64 amd64; do
  cc_arch="$arch"; [ "$arch" = amd64 ] && cc_arch=x86_64
  name="hobstack_${version}_darwin_${arch}"
  dir="$work/$name"
  mkdir -p "$dir"
  echo "building $name"
  CGO_ENABLED=1 GOOS=darwin GOARCH="$arch" CC="clang -arch $cc_arch" \
    go build -trimpath -ldflags "-s -w -X hobstack/internal/config.Version=${version}" -o "$dir/hobstack" ./cmd/hobstack
  codesign --force --sign - "$dir/hobstack" # semnatura ad-hoc (Apple Silicon nu ruleaza binare nesemnate)
  cp README.md LICENSE "$dir/"
  tar -C "$dir" -czf "dist/$name.tar.gz" .
  bins+=("$dir/hobstack")
done

echo "building HobStack.app"
app="$work/dmg/HobStack.app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
lipo -create -output "$app/Contents/MacOS/hobstack" "${bins[@]}"
cp assets/hobstack.icns "$app/Contents/Resources/hobstack.icns"
sed "s/VERSION/${version}/g" installer/macos/Info.plist > "$app/Contents/Info.plist"
codesign --force --deep --sign - "$app"
codesign --verify --deep --strict "$app"
ln -s /Applications "$work/dmg/Applications"
echo "building HobStack-${version}.dmg"
rm -f "dist/HobStack-${version}.dmg"
hdiutil create -quiet -volname "HobStack" -srcfolder "$work/dmg" -ov -format UDZO "dist/HobStack-${version}.dmg"
ls -la dist
