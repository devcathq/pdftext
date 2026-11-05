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

		// Written since blocks, rawdict and drawings were added; nil in
		// an older golden.
		Blocks      any `json:"blocks"`
		RawDict     any `json:"rawdict"`
		Drawings    any `json:"drawings"`
		DrawingsExt any `json:"drawings_extended"`

		ImagesFull any         `json:"images_full"`
		Pixmaps    []pixmapRec `json:"pixmaps"`
		ImageRects [][]any     `json:"image_rects"`
	} `json:"pages"`
	XrefLength  int       `json:"xref_length"`
	XrefObjects []*string `json:"xref_objects"`
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
	sameXrefs(t, doc, g)
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
		case !sameStructure(t, doc, g.File, i, want.Blocks, want.RawDict, want.Drawings, want.DrawingsExt):
			t.Errorf("%s page %d: blocks, rawdict or drawings differ", g.File, i+1)
		case !sameImages(t, doc, g.File, i, want.ImagesFull, want.Pixmaps, want.ImageRects):
			t.Errorf("%s page %d: images differ", g.File, i+1)
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

// sameStructure compares the page's blocks, rawdict and drawings (plain and
// extended) with PyMuPDF's; those a golden did not record pass.
func sameStructure(t *testing.T, doc *Doc, file string, n int, blocks, raw, drawings, ext any) bool {
	check := func(what string, want any, read func() (any, error)) bool {
		if want == nil {
			return true
		}
		got, err := read()
		if err != nil {
			t.Logf("%s page %d: %s: %v", file, n+1, what, err)
			return false
		}
		if ok, diff := sameShape(got, want); !ok {
			t.Logf("%s page %d: %s differ at %s", file, n+1, what, diff)
			return false
		}
		return true
	}
	ok := check("blocks", blocks, func() (any, error) {
		b, err := doc.Blocks(n)
		return blocksShape(b), err
	})
	if substitutedCJK[file] && raw != nil {
		raw = withoutFontStyle(raw)
	}
	ok = check("rawdict", raw, func() (any, error) {
		r, err := doc.RawDict(n)
		if substitutedCJK[file] {
			return withoutFontStyle(rawdictShape(r)), err
		}
		return rawdictShape(r), err
	}) && ok
	ok = check("drawings", drawings, func() (any, error) {
		p, err := doc.Drawings(n, false)
		return drawingsShape(p, false), err
	}) && ok
	return check("drawings extended", ext, func() (any, error) {
		p, err := doc.Drawings(n, true)
		return drawingsShape(p, true), err
	}) && ok
}

// withoutFontStyle drops what a span takes from its font (name, flags,
// ascender, descender), for the pages of substitutedCJK, whose font MuPDF
// substitutes differently from PyMuPDF's wheels.
func withoutFontStyle(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, e := range x {
			if _, span := x["chars"]; span && (k == "font" || k == "flags" || k == "ascender" || k == "descender") {
				continue
			}
			out[k] = withoutFontStyle(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = withoutFontStyle(e)
		}
		return out
	}
	return v
}

// pixmapRec is Pixmap(doc, xref) as PyMuPDF made it (pymupdf_golden.py).
type pixmapRec struct {
	Xref       int    `json:"xref"`
	RGB        bool   `json:"rgb"`
	Error      string `json:"error"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	N          int    `json:"n"`
	Alpha      bool   `json:"alpha"`
	ColorSpace string `json:"colorspace"`
	Digest     string `json:"digest"`
	PNG        string `json:"png"`
	PNGError   string `json:"png_error"`
}

// sameImages compares get_images(full=True), each image's pixmap and PNG,
// and get_image_rects; those a golden did not record pass.
func sameImages(t *testing.T, doc *Doc, file string, n int, full any, pixmaps []pixmapRec, rects [][]any) bool {
	if full == nil {
		return true
	}
	refs, err := doc.Images(n)
	if err != nil {
		t.Logf("%s page %d: images: %v", file, n+1, err)
		return false
	}
	shape := []any{}
	for _, r := range refs {
		shape = append(shape, []any{r.Xref, r.SMask, r.Width, r.Height, r.BPC, r.ColorSpace, r.AltColorSpace, r.Name, r.Filter, r.Referencer})
	}
	if ok, diff := sameShape(shape, full); !ok {
		t.Logf("%s page %d: images differ at %s", file, n+1, diff)
		return false
	}
	for _, want := range pixmaps {
		img, err := doc.Image(want.Xref, want.RGB)
		if (err != nil) != (want.Error != "") {
			t.Logf("%s page %d: image %d (rgb %v): Go error %v, PyMuPDF %q", file, n+1, want.Xref, want.RGB, err, want.Error)
			return false
		}
		if err != nil {
			continue
		}
		png := ""
		if img.PNG != nil {
			png = digest(img.PNG)
		}
		got := pixmapRec{Xref: want.Xref, RGB: want.RGB, Width: img.Width, Height: img.Height, N: img.N, Alpha: img.Alpha,
			ColorSpace: img.ColorSpace, Digest: img.Digest, PNG: png, PNGError: img.PNGError}
		if got != want {
			t.Logf("%s page %d: image %d (rgb %v): Go %+v, PyMuPDF %+v", file, n+1, want.Xref, want.RGB, got, want)
			return false
		}
	}
	for _, xr := range rects {
		xref := int(xr[0].(float64))
		got, err := doc.ImageRects(n, xref)
		if err != nil {
			t.Logf("%s page %d: image rects %d: %v", file, n+1, xref, err)
			return false
		}
		shape := []any{}
		for _, r := range got {
			shape = append(shape, rectShape(r))
		}
		if ok, diff := sameShape(shape, xr[1]); !ok {
			t.Logf("%s page %d: image rects %d differ at %s", file, n+1, xref, diff)
			return false
		}
	}
	return true
}

// sameXrefs compares xref_length() and every xref_object() of the file.
func sameXrefs(t *testing.T, doc *Doc, g golden) {
	if g.XrefObjects == nil {
		return
	}
	if doc.XrefLength() != g.XrefLength {
		t.Errorf("%s: xref length %d, PyMuPDF %d", g.File, doc.XrefLength(), g.XrefLength)
		return
	}
	for i, want := range g.XrefObjects {
		got, err := doc.XrefObject(i + 1)
		switch {
		case want == nil && err == nil:
			t.Errorf("%s: xref %d: Go %q, PyMuPDF raised", g.File, i+1, got)
		case want != nil && (err != nil || got != *want):
			t.Errorf("%s: xref %d: Go %q (%v), PyMuPDF %q", g.File, i+1, short(got), err, short(*want))
		}
	}
}
