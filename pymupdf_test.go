//go:build cgo && !nomupdf

package pdftext

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// golden is one PDF as PyMuPDF read it (scripts/pymupdf_golden.py).
type golden struct {
	File  string `json:"file"`
	Error string `json:"error"`
	Pages []struct {
		Text   string      `json:"text"`
		Sorted *string     `json:"sorted"` // nil: PyMuPDF raised ZeroDivisionError
		Image  bool        `json:"image"`
		Words  []string    `json:"words"`
		Rect   []float64   `json:"rect"`
		Boxes  [][]float64 `json:"image_boxes"`
		Draws  []drawn     `json:"renders"`
	} `json:"pages"`
}

// drawn is one page as PyMuPDF drew it: page.get_pixmap(dpi=DPI).
type drawn struct {
	DPI     int    `json:"dpi"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	PNG     string `json:"png"`     // SHA-256 of tobytes("png")
	Samples string `json:"samples"` // SHA-256 of the RGB samples
}

// TestAgainstPyMuPDF reads every PDF of PDFTEXT_CORPUS and requires, page by
// page, the plain text, the sorted text, the image flag and the words that
// PyMuPDF gave for it (pymupdf.jsonl in the same folder). Without the
// variable it reads testdata/.
func TestAgainstPyMuPDF(t *testing.T) {
	dir := os.Getenv("PDFTEXT_CORPUS")
	if dir == "" {
		dir = "testdata"
	}
	f, err := os.Open(filepath.Join(dir, "pymupdf.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<20), 64<<20)
	files, pages := 0, 0
	for sc.Scan() {
		var g golden
		if err := json.Unmarshal(sc.Bytes(), &g); err != nil {
			t.Fatal(err)
		}
		files++
		pages += comparePDF(t, filepath.Join(dir, g.File), g)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	t.Logf("%d files, %d pages the same as PyMuPDF", files, pages)
}

// comparePDF checks one file; it returns the pages that matched.
func comparePDF(t *testing.T, path string, g golden) int {
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := Open(data)
	if g.Error != "" || err != nil {
		if (g.Error == "") != (err == nil) {
			t.Errorf("%s: PyMuPDF %q, Go %v", g.File, g.Error, err)
		}
		return 0
	}
	defer doc.Close()
	if doc.Pages() != len(g.Pages) {
		t.Errorf("%s: %d pages, PyMuPDF %d", g.File, doc.Pages(), len(g.Pages))
		return 0
	}
	same := 0
	for i, want := range g.Pages {
		p, err := doc.Page(i)
		if err != nil {
			t.Errorf("%s page %d: %v", g.File, i+1, err)
			continue
		}
		sorted, serr := p.Sorted()
		words := make([]string, len(p.Words))
		for j, w := range p.Words {
			words[j] = w.Text
		}
		switch {
		case p.Text != want.Text:
			t.Errorf("%s page %d: plain text differs", g.File, i+1)
		case (want.Sorted == nil) != (serr != nil) || (want.Sorted != nil && sorted != *want.Sorted):
			t.Errorf("%s page %d: sorted text differs (Go error %v)", g.File, i+1, serr)
		case p.HasImage != want.Image:
			t.Errorf("%s page %d: image %v, PyMuPDF %v", g.File, i+1, p.HasImage, want.Image)
		case !slices.Equal(words, want.Words):
			t.Errorf("%s page %d: words differ", g.File, i+1)
		case !sameBoxes(t, doc, i, want.Rect, want.Boxes):
			t.Errorf("%s page %d: page box or image boxes differ", g.File, i+1)
		case !sameDrawings(t, doc, g.File, i, want.Draws):
			t.Errorf("%s page %d: drawn differently", g.File, i+1)
		default:
			same++
		}
	}
	return same
}

// sameBoxes compares page.rect and the image boxes; a golden without them
// (written before they were recorded) passes.
func sameBoxes(t *testing.T, doc *Doc, n int, rect []float64, boxes [][]float64) bool {
	if rect == nil {
		return true
	}
	r, err := doc.Bounds(n)
	if err != nil || !sameRect(r, rect) {
		t.Logf("page %d: bounds %+v (%v), PyMuPDF %v", n+1, r, err, rect)
		return false
	}
	got, err := doc.ImageBoxes(n)
	if err != nil || len(got) != len(boxes) {
		t.Logf("page %d: image boxes %+v (%v), PyMuPDF %v", n+1, got, err, boxes)
		return false
	}
	for i, b := range boxes {
		if !sameRect(got[i], b) {
			t.Logf("page %d: image box %d %+v, PyMuPDF %v", n+1, i, got[i], b)
			return false
		}
	}
	return true
}

func sameRect(r Rect, want []float64) bool {
	return len(want) == 4 && r == Rect{float32(want[0]), float32(want[1]), float32(want[2]), float32(want[3])}
}

// substitutedCJK are test files whose CJK fonts are not embedded. MuPDF
// draws them with a font of its own, and the one PyMuPDF 1.28.0's wheels
// pick (they bundle a MuPDF development snapshot, 1.29.0) is not the one
// released MuPDF 1.28.6 picks: the same text, in another typeface. Their
// pages are compared by size only. None of 300 real PDFs checked (31 Oct)
// has such a font.
var substitutedCJK = map[string]bool{"cjk.pdf": true}

// sameDrawings draws the page at each recorded DPI and requires PyMuPDF's
// size and pixels, and its PNG bytes.
func sameDrawings(t *testing.T, doc *Doc, file string, n int, draws []drawn) bool {
	for _, want := range draws {
		pic, err := doc.Render(n, want.DPI)
		if err != nil {
			t.Logf("page %d at %d dpi: %v", n+1, want.DPI, err)
			return false
		}
		samples, err := rgbSamples(pic.PNG)
		switch {
		case err != nil:
			t.Logf("page %d at %d dpi: %v", n+1, want.DPI, err)
			return false
		case pic.Width != want.Width || pic.Height != want.Height:
			t.Logf("page %d at %d dpi: %dx%d, PyMuPDF %dx%d", n+1, want.DPI, pic.Width, pic.Height, want.Width, want.Height)
			return false
		case substitutedCJK[file]:
		case digest(samples) != want.Samples:
			t.Logf("page %d at %d dpi: pixels differ", n+1, want.DPI)
			return false
		case digest(pic.PNG) != want.PNG:
			t.Logf("page %d at %d dpi: same pixels, PNG bytes differ", n+1, want.DPI)
			return false
		}
	}
	return true
}

// rgbSamples decodes a PNG to its RGB samples, as Pixmap.samples gives them.
func rgbSamples(data []byte) ([]byte, error) {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	out := make([]byte, 0, b.Dx()*b.Dy()*3)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			out = append(out, c.R, c.G, c.B)
		}
	}
	return out, nil
}

func digest(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
