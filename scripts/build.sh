#!/usr/bin/env bash
# Build local cu versiunea din git (git describe): de ex. 1.0.0 pe tag, 1.0.0-3-g657c1cf dupa el.
# Fara versiune (go build simplu) binarul e "dev" si nu propune singur update-uri.
set -euo pipefail
cd "$(dirname "$0")/.."
version="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
version="${version#v}"
go build -ldflags "-X hobstack/internal/config.Version=${version}" -o "${1:-hobstack}" ./cmd/hobstack
echo "built ${1:-hobstack} ${version}"
