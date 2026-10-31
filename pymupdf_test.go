//go:build cgo && !nomupdf

package pdftext

import (
	"bufio"
	"encoding/json"
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
		Text   string   `json:"text"`
		Sorted *string  `json:"sorted"` // nil: PyMuPDF raised ZeroDivisionError
		Image  bool     `json:"image"`
		Words  []string `json:"words"`
	} `json:"pages"`
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
		default:
			same++
		}
	}
	return same
}
