#!/usr/bin/env bash
# Builds MuPDF's static libraries for one platform into lib/<goos>_<goarch>/,
# from MuPDF's released source (checked by sha256).
#
#   bash scripts/build-mupdf.sh linux_amd64     # in Docker (golang image)
#   bash scripts/build-mupdf.sh linux_arm64     # in Docker (native on an arm64 host, emulated elsewhere)
#   bash scripts/build-mupdf.sh windows_amd64   # in Docker, cross-compiled with MinGW-w64 for UCRT
#   bash scripts/build-mupdf.sh darwin_arm64    # natively, on a Mac of that architecture
#   bash scripts/build-mupdf.sh darwin_amd64
#
# MUPDF_SRC=<path to mupdf-$VERSION-source.tar.gz> skips the download.
# Why each flag: README.md, "Building MuPDF".
set -euo pipefail

VERSION=1.28.6
URL="https://github.com/ArtifexSoftware/mupdf-downloads/releases/download/$VERSION/mupdf-$VERSION-source.tar.gz"
SHA256=dc85000640a45d90ca9b77b061f6ca67cce0c4d4fbecf8819647baf573eef101
# Linux builds on Debian 12 (glibc 2.36): libraries compiled against a newer
# glibc call its C23 functions (__isoc23_strtol) and cannot link on older
# systems. Windows builds on Debian 13, for its MinGW-w64 for UCRT.
LINUX_IMAGE=golang:1.27-bookworm
CROSS_IMAGE=golang:1.27
MAKE_FLAGS="build=release HAVE_X11=no HAVE_GLUT=no HAVE_CURL=no"

target="${1:?usage: build-mupdf.sh <goos_goarch>}"
root="$(cd "$(dirname "$0")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
mount="$work" # the folder as Docker sees it: Git Bash's /tmp is not a Windows path
if command -v cygpath >/dev/null; then mount="$(cygpath -m "$work")"; fi

if [ -n "${MUPDF_SRC:-}" ]; then
  cp "$MUPDF_SRC" "$work/src.tar.gz"
else
  curl -fsSL -o "$work/src.tar.gz" "$URL"
fi
if command -v sha256sum >/dev/null; then
  echo "$SHA256  $work/src.tar.gz" | sha256sum -c -
else
  echo "$SHA256  $work/src.tar.gz" | shasum -a 256 -c -
fi

# build runs the make in a shell: in Docker for Linux and Windows, here on a Mac.
# Docker's files belong to root; they are handed back to this user, so the
# clean-up at exit can remove them.
owner="$(id -u):$(id -g)"
in_docker() {
  MSYS_NO_PATHCONV=1 docker run --rm --platform "$1" -v "$mount:/work" "$2" \
    bash -c "( $3 ); s=\$?; chown -R $owner /work 2>/dev/null; exit \$s"
}
case "$target" in
  linux_amd64 | linux_arm64)
    platform="linux/${target#linux_}"
    build() { in_docker "$platform" "$LINUX_IMAGE" "$1"; }
    make_cmd="make -j\$(nproc) $MAKE_FLAGS XCFLAGS=-fPIC libs"
    dir=/work
    ;;
  windows_amd64)
    # MinGW-w64 for the UCRT runtime, the default of today's Windows
    # toolchains (MSYS2 UCRT64, MinGW-Builds): setjmp is a different symbol
    # there than in msvcrt, so the two cannot link each other's libraries.
    # MinGW's headers define _M_AMD64, which turns on MuPDF's SSE4.1 code
    # without its compiler flags; ARCH_HAS_SSE=0 keeps the code paths of the
    # Linux build (only image deskewing uses them).
    build() { in_docker linux/amd64 "$CROSS_IMAGE" "$1"; }
    cross=x86_64-w64-mingw32ucrt
    make_cmd="apt-get update -qq >/dev/null && apt-get install -y -qq gcc-mingw-w64-ucrt64 g++-mingw-w64-ucrt64 >/dev/null && \
      make -j\$(nproc) $MAKE_FLAGS OS=MINGW CC=$cross-gcc CXX=$cross-g++ \
      AR=$cross-ar HAVE_OBJCOPY=no XCFLAGS=-DARCH_HAS_SSE=0 libs"
    dir=/work
    ;;
  darwin_amd64 | darwin_arm64)
    want="${target#darwin_}"; [ "$want" = amd64 ] && want=x86_64
    [ "$(uname -s)" = Darwin ] && [ "$(uname -m)" = "$want" ] || { echo "build $target on a Mac of that architecture" >&2; exit 1; }
    build() { bash -c "$1"; }
    make_cmd="make -j\$(sysctl -n hw.ncpu) $MAKE_FLAGS libs"
    dir="$work"
    ;;
  *)
    echo "unknown target $target" >&2; exit 1
    ;;
esac

build "set -e; cd $dir && tar -xzf src.tar.gz && cd mupdf-$VERSION-source && $make_cmd && \
  mkdir -p $dir/out && cp build/release/libmupdf.a build/release/libmupdf-third.a $dir/out/"

rm -rf "$root/lib/$target"
mkdir -p "$root/lib/$target/licenses"
cp "$work"/out/*.a "$root/lib/$target/"

# The licences of everything in these libraries travel with them
# (THIRD_PARTY.md); a file missing from a new MuPDF release stops the build.
src="$work/mupdf-$VERSION-source"
while read -r from to; do
  cp "$src/$from" "$root/lib/$target/licenses/$to"
done <<'EOF'
COPYING MuPDF-COPYING
CONTRIBUTORS MuPDF-CONTRIBUTORS
thirdparty/brotli/LICENSE brotli-LICENSE
thirdparty/cmark-gfm/COPYING cmark-gfm-COPYING
thirdparty/freetype/LICENSE.TXT freetype-LICENSE.TXT
thirdparty/freetype/docs/FTL.TXT freetype-FTL.TXT
thirdparty/gumbo-parser/COPYING gumbo-parser-COPYING
thirdparty/harfbuzz/COPYING harfbuzz-COPYING
thirdparty/jbig2dec/LICENSE jbig2dec-LICENSE
thirdparty/lcms2/LICENSE lcms2-LICENSE
thirdparty/libjpeg/README libjpeg-README
thirdparty/mujs/COPYING mujs-COPYING
thirdparty/openjpeg/LICENSE openjpeg-LICENSE
thirdparty/zlib/LICENSE zlib-LICENSE
resources/fonts/noto/COPYING fonts-noto-COPYING
resources/fonts/han/LICENSE.txt fonts-source-han-serif-LICENSE.txt
resources/fonts/droid/NOTICE fonts-droid-NOTICE
resources/fonts/sil/OFL.txt fonts-charis-sil-OFL.txt
resources/fonts/urw/OFL.txt fonts-urw-OFL.txt
resources/README cmaps-README
EOF
cp "$root/THIRD_PARTY.md" "$root/lib/$target/licenses/"
cd "$root/lib/$target"
if command -v sha256sum >/dev/null; then sha256sum ./*.a; else shasum -a 256 ./*.a; fi
