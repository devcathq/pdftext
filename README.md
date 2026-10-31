# pdftext

PDF text for Go, exactly as Python's PyMuPDF gives it.

When a Python service that reads PDFs with PyMuPDF moves to Go, the text must not change: classifiers, prompts, field extraction and stored rows all depend on it, byte for byte. `pdftext` reads PDFs with the same C library, MuPDF, and repeats what PyMuPDF does on top of it. A page gives the same plain text, the same text in reading order (`get_text("text", sort=True)`), the same words and the same image flag.

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
}
```

A `Doc` is not safe for concurrent use: open one per goroutine. `SortedText(words)` is the sort on its own, for words you already have. Where PyMuPDF raises `ZeroDivisionError` (a word or line with no width or height), `Sorted` returns `ErrNoSize`.

## Build

The package needs cgo and MuPDF's static libraries for your platform, about 69 MB each, so they are not in the repository:

- **Download** them from a release: `bash scripts/fetch-libs.sh <goos_goarch> [dir]` checks them against the release's `SHA256SUMS`.
- **Or build** them from MuPDF's source: `bash scripts/build-mupdf.sh <goos_goarch>` (Docker for Linux and Windows, a Mac for macOS).

Inside this repository they go to `lib/<goos>_<goarch>/` and are found from there. In your own module, point cgo at them: `CGO_ENABLED=1 CGO_LDFLAGS=-L/path/to/libs go build`. On Windows cgo needs MinGW-w64's `gcc`.

| Platform | Libraries |
|---|---|
| `linux_amd64`, `linux_arm64` | built in Docker (`golang:1.27`, gcc) |
| `windows_amd64` | cross-built in Docker with MinGW-w64 |
| `darwin_arm64`, `darwin_amd64` | built on a Mac of that architecture |

Without cgo, or with `-tags nomupdf`, the package builds without MuPDF and `Open` returns `ErrUnavailable`, so a program that only sometimes reads PDFs still builds anywhere.

### Building MuPDF

MuPDF 1.28.6 from its released source (`mupdf-downloads`, sha256 checked), `make build=release HAVE_X11=no HAVE_GLUT=no HAVE_CURL=no libs`:

- no viewers and no network code;
- the default fonts and the CJK tables stay in;
- Linux adds `-fPIC`;
- Windows sets `ARCH_HAS_SSE=0`. MinGW's headers define `_M_AMD64`, which would turn on MuPDF's SSE4.1 code without its compiler flags; this keeps Linux's code paths (only image deskewing uses them).

`include/` holds the headers of that release; `Open` refuses to run if the headers and the linked library disagree.

## Checked against PyMuPDF

`TestAgainstPyMuPDF` reads every PDF of a folder and requires, page by page, PyMuPDF's plain text, sorted text, words and image flag (`pymupdf.jsonl`, written by `scripts/pymupdf_golden.py` with PyMuPDF 1.28.0).

- **`testdata/`:** the PDFs `scripts/make_testdata.py` makes: two-column and scattered layouts, a rotated page, Japanese, Korean and Chinese pages in the standard CJK fonts, and a seven-page statement. CI runs it on every platform.
- **Your own PDFs:** `python scripts/pymupdf_golden.py <folder>`, then `PDFTEXT_CORPUS=<folder> go test -run TestAgainstPyMuPDF`.
- **Measured 31 Oct 2026** on 300 real business PDFs (Japanese, Korean and English invoices, contracts, statements; not published):
  - `linux_amd64`: 300 of 300 files, 3,659 of 3,659 pages the same as PyMuPDF.
  - `windows_amd64` (cross-built, run natively on Windows 11): 300 of 300 files, 3,659 of 3,659 pages.

PyMuPDF 1.28.0's wheel bundles a MuPDF development snapshot ("1.29.0"); this release build gives the same text on all of those files. go-fitz's prebuilt MuPDF, which leaves the CJK tables out, gave 298 of 300 (30 Oct 2026).

## Status

| Platform | Built | Tests | Against PyMuPDF (300 files) |
|---|---|---|---|
| `linux_amd64` | yes | pass | 300 of 300 |
| `windows_amd64` | yes | pass (native, Windows 11) | 300 of 300 |
| `linux_arm64`, `darwin_arm64`, `darwin_amd64` | by CI | by CI | not yet |

## Where it comes from

The code was first written for a Go service that replaces a Python document pipeline, and moved here to be shared. `nx.c` repeats in C what PyMuPDF 1.28.0 does to read a page (its `extra.i`): the text page flags, the character boxes, the word split and its small corrections for thin fonts and zero-width glyphs. `sort.go` is PyMuPDF's `get_sorted_text`, with Python's arithmetic (float64 over MuPDF's float32, round half to even).

## Licence

AGPL-3.0, as MuPDF and PyMuPDF are (`LICENSE`). MuPDF is © Artifex Software, available under the AGPL or a commercial licence from Artifex; a program that links this package links MuPDF and takes on the same terms.
