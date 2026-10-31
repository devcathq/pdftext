# pdftext

PDF text and page images for Go, exactly as Python's PyMuPDF gives them.

When a Python service that reads PDFs with PyMuPDF moves to Go, the text must not change: classifiers, prompts, field extraction and stored rows all depend on it, byte for byte. Nor may the page images it sends to an OCR model. `pdftext` reads PDFs with the same C library, MuPDF, and repeats what PyMuPDF does on top of it. A page gives the same plain text, the same text in reading order (`get_text("text", sort=True)`), the same words, the same image flag, the same size and image boxes, and the same picture (`get_pixmap(dpi=…)`), pixel for pixel.

It ships MuPDF **with its CJK tables**, which Japanese, Chinese and Korean PDFs need when their fonts carry no Unicode map. Other Go bindings leave them out to save space, and then those PDFs read differently or not at all.

## Use

```go
import "github.com/devcathq/pdftext"

doc, err := pdftext.Open(data) // the PDF's bytes
if err != nil {
	return err
}
defer doc.Close()
for i := 0; i < doc.Pages(); i++ {
	page, err := doc.Page(i)
	if err != nil {
		return err
	}
	sorted, err := page.Sorted() // page.get_text("text", sort=True)
	_ = page.Text                // page.get_text()
	_ = page.Words               // page.get_text("words"): text and box
	_ = page.HasImage            // len(page.get_images()) > 0
	_, _ = sorted, err

	box, _ := doc.Bounds(i)        // page.rect
	images, _ := doc.ImageBoxes(i) // [info["bbox"] for info in page.get_image_info()]
	pic, _ := doc.Render(i, 400)   // page.get_pixmap(dpi=400).tobytes("png")
	_ = pic.White                  // every pixel is white: the page draws nothing
	_, _ = box, images
}
```

A `Doc` is not safe for concurrent use: open one per goroutine. `SortedText(words)` is the sort on its own, for words you already have. Where PyMuPDF raises `ZeroDivisionError` (a word or line with no width or height), `Sorted` returns `ErrNoSize`.

## Build

The package needs cgo and MuPDF's static libraries for your platform, about 69 MB each, so they are not in the repository:

- **Download** them from a release: `bash scripts/fetch-libs.sh <goos_goarch> [dir]` checks them against the release's `SHA256SUMS`.
- **Or build** them from MuPDF's source: `bash scripts/build-mupdf.sh <goos_goarch>` (Docker for Linux and Windows, a Mac for macOS).

Inside this repository they go to `lib/<goos>_<goarch>/` and are found from there. In your own module, point cgo at them: `CGO_ENABLED=1 CGO_LDFLAGS=-L/path/to/libs go build`. On Windows cgo needs a MinGW-w64 `gcc` for the UCRT runtime, the default of MSYS2's UCRT64 and of MinGW-Builds; a toolchain for the older msvcrt runtime (TDM-GCC, old MinGW) cannot link these libraries, because `setjmp` is a different symbol there.

| Platform | Libraries |
|---|---|
| `linux_amd64`, `linux_arm64` | built in Docker on Debian 12 (`golang:1.27-bookworm`, gcc 12), so they link on glibc 2.36 and newer |
| `windows_amd64` | cross-built in Docker with MinGW-w64 for UCRT |
| `darwin_arm64`, `darwin_amd64` | built on a Mac of that architecture |

Without cgo, or with `-tags nomupdf`, the package builds without MuPDF and `Open` returns `ErrUnavailable`, so a program that only sometimes reads PDFs still builds anywhere.

### Building MuPDF

MuPDF 1.28.6 from its released source (`mupdf-downloads`, sha256 checked), `make build=release HAVE_X11=no HAVE_GLUT=no HAVE_CURL=no libs`:

- no viewers and no network code;
- the default fonts and the CJK tables stay in;
- Linux adds `-fPIC` and builds on Debian 12: a newer glibc's headers turn `strtol` into its C23 variant (`__isoc23_strtol`), which systems older than glibc 2.38 cannot link;
- Windows is built for the UCRT runtime and sets `ARCH_HAS_SSE=0`. MinGW's headers define `_M_AMD64`, which would turn on MuPDF's SSE4.1 code without its compiler flags; this keeps Linux's code paths (only image deskewing uses them).

`include/` holds the headers of that release; `Open` refuses to run if the headers and the linked library disagree.

## Checked against PyMuPDF

`TestAgainstPyMuPDF` reads every PDF of a folder and requires, page by page, PyMuPDF's plain text, sorted text, words, image flag, page box and image boxes, and, where they were recorded, the page drawn at each DPI: its size, its pixels and its PNG bytes (`pymupdf.jsonl`, written by `scripts/pymupdf_golden.py` with PyMuPDF 1.28.0).

- **`testdata/`:** the PDFs `scripts/make_testdata.py` makes: two-column and scattered layouts, a rotated page, Japanese, Korean and Chinese pages in the standard CJK fonts, a seven-page statement, and pages with images (a full-page scan, a logo, an image half off the page, a note annotation, a rotated page, a blank page), drawn at 150 and 400 DPI. CI runs it on every platform.
- **Your own PDFs:** `python scripts/pymupdf_golden.py <folder> [dpi,…]`, then `PDFTEXT_CORPUS=<folder> go test -run TestAgainstPyMuPDF`.
- **Measured 31 Oct 2026** on 300 real business PDFs (Japanese, Korean and English invoices, contracts, statements; not published):
  - `linux_amd64`: 300 of 300 files, 3,659 of 3,659 pages the same as PyMuPDF.
  - `windows_amd64` (cross-built, run natively on Windows 11): 300 of 300 files, 3,659 of 3,659 pages.
  - Drawn at 100 DPI (v0.2.0, `linux_amd64`): 3,659 of 3,659 pages the same pixels and the same PNG bytes, and the same page and image boxes.

PyMuPDF 1.28.0's wheel bundles a MuPDF development snapshot ("1.29.0"); this release build gives the same text and pictures on all of those files. One difference is known: where a PDF's CJK font is not embedded, MuPDF draws the text with a font of its own, and the snapshot picks a sans-serif one where 1.28.6 picks Source Han Serif (the test's `cjk.pdf`; none of the 300 files). go-fitz's prebuilt MuPDF, which leaves the CJK tables out, gave 298 of 300 (30 Oct 2026).

## Status

| Platform | Built by CI | Tests (native, CI) | `testdata/` against PyMuPDF | 300 real PDFs against PyMuPDF |
|---|---|---|---|---|
| `linux_amd64` | yes | pass | 13 of 13 pages | 300 of 300 files (local) |
| `windows_amd64` | yes (UCRT) | pass | 13 of 13 pages | 300 of 300 files (Windows 11) |
| `linux_arm64` | yes | pass | 13 of 13 pages | not run |
| `darwin_arm64` | yes | pass | 13 of 13 pages | not run |
| `darwin_amd64` | yes | pass | 13 of 13 pages | not run |

## Where it comes from

The code was first written for a Go service that replaces a Python document pipeline, and moved here to be shared. `nx.c` repeats in C what PyMuPDF 1.28.0 does to read a page (its `extra.i`): the text page flags, the character boxes, the word split and its small corrections for thin fonts and zero-width glyphs. `sort.go` is PyMuPDF's `get_sorted_text`, with Python's arithmetic (float64 over MuPDF's float32, round half to even). `nx_draw.c` is what PyMuPDF does to draw a page (`get_pixmap`, its display list and PNG writer) and to list the images a page draws (`get_image_info`).

## Thanks

This package is a thin layer over other people's work. Without them it would not exist, and we would not know how a PDF should read.

- **MuPDF**, by Artifex Software and the many developers who sent it fixes and features over the years. MuPDF's own contributors file thanks in particular Simon Bünzli (zeniko) and Krzysztof Kowalczyk of the SumatraPDF project, and Sebastian Rasmussen. Every page here is read by MuPDF.
- **PyMuPDF**, by Artifex and [its contributors](https://github.com/pymupdf/PyMuPDF/graphs/contributors). `nx.c` and `sort.go` follow its code, and its output is the answer every test checks against.
- **The libraries inside MuPDF**: brotli, cmark-gfm, FreeType, gumbo-parser, HarfBuzz, jbig2dec, Little CMS, libjpeg (the Independent JPEG Group), MuJS, OpenJPEG and zlib, and their authors.
- **The fonts and character maps inside MuPDF**: Noto (Google), Source Han Serif (Adobe), Droid Sans Fallback (the Android Open Source Project), Charis SIL (SIL International), the URW base 35 fonts ((URW)++), and Adobe's CMap resources, which are what read Japanese, Chinese and Korean text.
- **[go-fitz](https://github.com/gen2brain/go-fitz)** by Milan Nikolic, the Go binding to MuPDF we started from. It showed what a Go binding to MuPDF takes, and where the CJK tables matter.
- **ReportLab**, which makes the test PDFs.

[THIRD_PARTY.md](THIRD_PARTY.md) lists each of them with its licence and copyright; their licence files ship in every release archive (`licenses/`). Portions of this software are copyright © 2026 The FreeType Project (https://freetype.org). All rights reserved. This software is based in part on the work of the Independent JPEG Group.

## Licence

AGPL-3.0, as MuPDF and PyMuPDF are (`LICENSE`). MuPDF is © Artifex Software, available under the AGPL or a commercial licence from Artifex; a program that links this package links MuPDF and takes on the same terms. The components MuPDF bundles keep their own licences ([THIRD_PARTY.md](THIRD_PARTY.md)).
