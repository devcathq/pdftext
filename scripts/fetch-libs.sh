#!/usr/bin/env bash
# Downloads this repo's prebuilt MuPDF for one platform from a release and
# checks it against the release's SHA256SUMS.
#
#   bash scripts/fetch-libs.sh linux_amd64              # into lib/linux_amd64
#   bash scripts/fetch-libs.sh windows_amd64 /opt/mupdf # anywhere; then CGO_LDFLAGS=-L/opt/mupdf
#
# PDFTEXT_RELEASE picks the release (default below).
set -euo pipefail

RELEASE="${PDFTEXT_RELEASE:-v0.3.0}"
MUPDF=1.28.6
BASE="https://github.com/devcathq/pdftext/releases/download/$RELEASE"

target="${1:?usage: fetch-libs.sh <goos_goarch> [dir]}"
root="$(cd "$(dirname "$0")/.." && pwd)"
dest="${2:-$root/lib/$target}"
asset="mupdf-$MUPDF-$target.tar.gz"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

curl -fsSL -o "$work/$asset" "$BASE/$asset"
curl -fsSL -o "$work/SHA256SUMS" "$BASE/SHA256SUMS"
(cd "$work" && grep " $asset\$" SHA256SUMS > want && test -s want &&
  if command -v sha256sum >/dev/null; then sha256sum -c want; else shasum -a 256 -c want; fi)

mkdir -p "$dest"
tar -xzf "$work/$asset" -C "$dest"
ls -l "$dest"
