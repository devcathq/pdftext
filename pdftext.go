// Package pdftext reads the text of PDF pages through MuPDF exactly as
// PyMuPDF 1.28 gives it: a page's words with their boxes, its plain text
// (page.get_text()), its text in reading order (page.get_text("text",
// sort=True)), and whether it shows an image (page.get_images()). A Go
// service that replaces a Python one gets the same text, byte for byte.
//
// MuPDF is a C library, linked through cgo. Its static build for each
// platform, with the CJK tables Japanese, Chinese and Korean PDFs need, comes
// from this repo's releases or from scripts/build-mupdf.sh (README.md). A
// build without cgo, or with the nomupdf tag, has no MuPDF: Open returns
// ErrUnavailable and the rest of a program builds as before.
//
// MuPDF and PyMuPDF are AGPL-3.0, and so is this package.
package pdftext

import "errors"

// ErrUnavailable means this build has no MuPDF (no cgo, or the nomupdf tag).
var ErrUnavailable = errors.New("pdftext: MuPDF is not in this build (needs cgo)")

// Word is one word of a page, as PyMuPDF's TextPage.extractWORDS gives it:
// its box in MuPDF's float32, and its text.
type Word struct {
	X0, Y0, X1, Y1 float32
	Text           string
}

// Page is what one page of a PDF gives, read with the page derotated as
// PyMuPDF's get_text does.
type Page struct {
	Words    []Word // in MuPDF's order, before any sort
	Text     string // TextPage.extractText(): the plain text, unsorted
	HasImage bool   // Page.get_images(full=False) is not empty
}

// Sorted is the page's get_text("text", sort=True) (SortedText).
func (p Page) Sorted() (string, error) { return SortedText(p.Words) }
