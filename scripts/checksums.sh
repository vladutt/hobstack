#!/usr/bin/env bash
# checksums.txt (sha256) pentru tot ce e de publicat in dist/ (update-ul din aplicatie il verifica).
set -euo pipefail
cd "$(dirname "$0")/../dist"
files=$(ls hobstack_* HobStack-* 2>/dev/null)
if command -v sha256sum >/dev/null; then sha256sum $files > checksums.txt; else shasum -a 256 $files > checksums.txt; fi
cat checksums.txt
