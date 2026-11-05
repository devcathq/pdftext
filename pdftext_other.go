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

// Bounds returns ErrUnavailable in this build.
func (*Doc) Bounds(int) (Rect, error) { return Rect{}, ErrUnavailable }

// Render returns ErrUnavailable in this build.
func (*Doc) Render(int, int) (Picture, error) { return Picture{}, ErrUnavailable }

// ImageBoxes returns ErrUnavailable in this build.
func (*Doc) ImageBoxes(int) ([]Rect, error) { return nil, ErrUnavailable }

// Blocks returns ErrUnavailable in this build.
func (*Doc) Blocks(int) ([]Block, error) { return nil, ErrUnavailable }

// RawDict returns ErrUnavailable in this build.
func (*Doc) RawDict(int) (TextDict, error) { return TextDict{}, ErrUnavailable }

// Drawings returns ErrUnavailable in this build.
func (*Doc) Drawings(int, bool) ([]Path, error) { return nil, ErrUnavailable }

// Images returns ErrUnavailable in this build.
func (*Doc) Images(int) ([]ImageRef, error) { return nil, ErrUnavailable }

// Image returns ErrUnavailable in this build.
func (*Doc) Image(int, bool) (Image, error) { return Image{}, ErrUnavailable }

// ImageRects returns ErrUnavailable in this build.
func (*Doc) ImageRects(int, int) ([]Rect, error) { return nil, ErrUnavailable }

// XrefLength is 0 in this build.
func (*Doc) XrefLength() int { return 0 }

// XrefObject returns ErrUnavailable in this build.
func (*Doc) XrefObject(int) (string, error) { return "", ErrUnavailable }
