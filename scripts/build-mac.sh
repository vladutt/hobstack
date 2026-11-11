#!/usr/bin/env bash
# Construieste pe macOS (cu cgo, pentru bara de meniu): hobstack_<v>_darwin_<arch>.tar.gz (arm64, amd64; update-ul
# din aplicatie le foloseste) si HobStack-<v>.dmg cu HobStack.app universal (Apple Silicon + Intel).
#   scripts/build-mac.sh 1.0.5
#
# Semnarea (docs/macos-signing.md):
#   - fara MACOS_SIGN_IDENTITY: semnatura ad-hoc (`--sign -`), fara notarizare; asa merge local, fara nimic de configurat.
#   - cu MACOS_SIGN_IDENTITY (de ex. "Developer ID Application: Nume (TEAMID)"): semnatura Developer ID cu hardened
#     runtime si timestamp, apoi notarizare la Apple. Atunci sunt obligatorii si MACOS_NOTARY_APPLE_ID,
#     MACOS_NOTARY_TEAM_ID si MACOS_NOTARY_PASSWORD (parola specifica aplicatiei); daca lipseste una, scriptul se opreste.
# Fara `set -x`: parola de notarizare nu trebuie sa apara in log.
set -euo pipefail
version="${1:?usage: scripts/build-mac.sh <version>}"
version="${version#v}"
cd "$(dirname "$0")/.."
[ "$(uname)" = Darwin ] || { echo "scripts/build-mac.sh needs macOS (cgo + Cocoa)" >&2; exit 1; }

identity="${MACOS_SIGN_IDENTITY:-}"
if [ -n "$identity" ]; then
  # cu identitate, notarizarea e obligatorie: verificam variabilele inainte de build, nu dupa 5 minute
  missing=()
  [ -n "${MACOS_NOTARY_APPLE_ID:-}" ] || missing+=(MACOS_NOTARY_APPLE_ID)
  [ -n "${MACOS_NOTARY_TEAM_ID:-}" ] || missing+=(MACOS_NOTARY_TEAM_ID)
  [ -n "${MACOS_NOTARY_PASSWORD:-}" ] || missing+=(MACOS_NOTARY_PASSWORD)
  if [ ${#missing[@]} -gt 0 ]; then
    echo "MACOS_SIGN_IDENTITY is set, but notarization needs: ${missing[*]}. Set them, or unset MACOS_SIGN_IDENTITY for an ad-hoc build." >&2
    exit 1
  fi
  echo "signing with: $identity"
else
  echo "MACOS_SIGN_IDENTITY not set: ad-hoc signing, no notarization"
fi

# sign semneaza un fisier (binar sau .app): ad-hoc fara identitate, altfel Developer ID + hardened runtime + timestamp.
# Primul argument e calea; restul (de ex. --deep) merg la codesign.
sign() {
  local path="$1"
  shift
  if [ -z "$identity" ]; then
    codesign --force "$@" --sign - "$path"
  else
    codesign --force "$@" --options runtime --timestamp --sign "$identity" "$path"
  fi
}

# notarize trimite un fisier (.dmg sau .zip) la Apple si asteapta verdictul. La esec scrie log-ul notarizarii si iese
# cu eroare. Parola merge doar ca argument catre notarytool, nu se afiseaza.
notarize() {
  local file="$1" out id status rc=0
  out="$work/notary-$(basename "$file").json"
  echo "notarizing $(basename "$file") (this can take a few minutes)"
  xcrun notarytool submit "$file" --apple-id "$MACOS_NOTARY_APPLE_ID" --team-id "$MACOS_NOTARY_TEAM_ID" \
    --password "$MACOS_NOTARY_PASSWORD" --wait --output-format json > "$out" || rc=$?
  # codul de iesire nu ajunge (poate fi 0 si cu statusul Invalid), deci citim si statusul din JSON (plutil citeste JSON)
  id="$(plutil -extract id raw -o - "$out" 2>/dev/null || true)"
  status="$(plutil -extract status raw -o - "$out" 2>/dev/null || true)"
  echo "notarization of $(basename "$file"): ${status:-unknown} (id ${id:-?}, exit $rc)"
  if [ "$rc" -ne 0 ] || [ "$status" != "Accepted" ]; then
    cat "$out" >&2
    if [ -n "$id" ]; then
      xcrun notarytool log "$id" --apple-id "$MACOS_NOTARY_APPLE_ID" --team-id "$MACOS_NOTARY_TEAM_ID" \
        --password "$MACOS_NOTARY_PASSWORD" >&2 || true
    fi
    exit 1
  fi
}

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
  sign "$dir/hobstack" # ad-hoc cel putin: Apple Silicon nu ruleaza binare nesemnate
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
sign "$app" --deep # --deep e ok: bundle-ul are un singur binar
codesign --verify --deep --strict "$app"
ln -s /Applications "$work/dmg/Applications"
echo "building HobStack-${version}.dmg"
dmg="dist/HobStack-${version}.dmg"
rm -f "$dmg"
hdiutil create -quiet -volname "HobStack" -srcfolder "$work/dmg" -ov -format UDZO "$dmg"

if [ -n "$identity" ]; then
  # .dmg-ul se semneaza si el (fara hardened runtime, care e doar pentru cod), se notarizeaza si primeste tichetul
  codesign --force --timestamp --sign "$identity" "$dmg"
  notarize "$dmg"
  xcrun stapler staple "$dmg"

  # Binarele din tar.gz: un binar simplu nu poate primi tichet (staple), dar notarizat trece verificarea online a
  # Gatekeeper. Le trimitem impreuna intr-un zip; tar.gz-urile raman exact cum sunt.
  mkdir -p "$work/notary-bins"
  for arch in arm64 amd64; do
    cp "$work/hobstack_${version}_darwin_${arch}/hobstack" "$work/notary-bins/hobstack_${arch}"
  done
  ditto -c -k --keepParent "$work/notary-bins" "$work/hobstack-bins.zip"
  notarize "$work/hobstack-bins.zip"

  # verificarea finala, cum o face Gatekeeper; un esec aici opreste release-ul
  echo "Gatekeeper check (.dmg):"
  spctl -a -t open --context context:primary-signature -v "$dmg"
  echo "Gatekeeper check (.app):"
  spctl -a -vv "$app"
fi
ls -la dist
