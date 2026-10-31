//go:build !cgo || nomupdf

package pdftext

// Doc is a stand-in: without cgo, or with the nomupdf tag, there is no MuPDF
// to read with.
type Doc struct{}

// Open returns ErrUnavailable in this build.
func Open([]byte) (*Doc, error) { return nil, ErrUnavailable }

// Pages is never reached: Open always fails here.
func (*Doc) Pages() int { return 0 }

// Close does nothing.
func (*Doc) Close() {}

// Page returns ErrUnavailable in this build.
func (*Doc) Page(int) (Page, error) { return Page{}, ErrUnavailable }
