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

// FindTables returns ErrUnavailable in this build.
func (*Doc) FindTables(int) ([]Table, error) { return nil, ErrUnavailable }

// New returns ErrUnavailable in this build.
func New() (*Doc, error) { return nil, ErrUnavailable }

// NewPage returns ErrUnavailable in this build.
func (*Doc) NewPage(float64, float64) error { return ErrUnavailable }

// DrawLine returns ErrUnavailable in this build.
func (*Doc) DrawLine(int, float64, float64, float64, float64, DrawOptions) error {
	return ErrUnavailable
}

// DrawRect returns ErrUnavailable in this build.
func (*Doc) DrawRect(int, Box, DrawOptions) error { return ErrUnavailable }

// InsertText returns ErrUnavailable in this build.
func (*Doc) InsertText(int, float64, float64, string, TextOptions) (int, error) {
	return 0, ErrUnavailable
}

// InsertTextbox returns ErrUnavailable in this build.
func (*Doc) InsertTextbox(int, Box, string, TextOptions) (float64, error) { return 0, ErrUnavailable }

// Bytes returns ErrUnavailable in this build.
func (*Doc) Bytes() ([]byte, error) { return nil, ErrUnavailable }

// ExtractPages returns ErrUnavailable in this build.
func (*Doc) ExtractPages([]int) ([]byte, error) { return nil, ErrUnavailable }

// Metadata returns ErrUnavailable in this build.
func (*Doc) Metadata() (map[string]*string, error) { return nil, ErrUnavailable }

// SetMetadata returns ErrUnavailable in this build.
func (*Doc) SetMetadata(map[string]string) error { return ErrUnavailable }

// TextLength returns ErrUnavailable in this build.
func TextLength(string, string, float64) (float64, error) { return 0, ErrUnavailable }

// PDFWriter is a stand-in: there is no MuPDF to write with.
type PDFWriter struct{}

// NewPDFWriter returns ErrUnavailable in this build.
func NewPDFWriter() (*PDFWriter, error) { return nil, ErrUnavailable }

// BeginPage returns ErrUnavailable in this build.
func (*PDFWriter) BeginPage(Box) error { return ErrUnavailable }

// EndPage returns ErrUnavailable in this build.
func (*PDFWriter) EndPage() error { return ErrUnavailable }

// Close returns ErrUnavailable in this build.
func (*PDFWriter) Close() ([]byte, error) { return nil, ErrUnavailable }

// Story is a stand-in: there is no MuPDF to lay out with.
type Story struct{}

// NewStory returns ErrUnavailable in this build.
func (*PDFWriter) NewStory(string, string) (*Story, error) { return nil, ErrUnavailable }

// Place returns ErrUnavailable in this build.
func (*Story) Place(Box) (bool, Box, error) { return false, Box{}, ErrUnavailable }

// Draw returns ErrUnavailable in this build.
func (*Story) Draw() error { return ErrUnavailable }

// Close does nothing.
func (*Story) Close() {}

// ResolveNames returns ErrUnavailable in this build.
func (*Doc) ResolveNames() (map[string]Dest, error) { return nil, ErrUnavailable }
