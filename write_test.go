//go:build cgo && !nomupdf

package pdftext

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeOp is one operation of testdata/write_cases.json.
type writeOp struct {
	Op       string            `json:"op"`
	File     string            `json:"file"`
	W        float64           `json:"w"`
	H        float64           `json:"-"`
	Page     int               `json:"page"`
	Rect     []float64         `json:"rect"`
	P1       []float64         `json:"p1"`
	P2       []float64         `json:"-"`
	Point    []float64         `json:"point"`
	Text     string            `json:"text"`
	FontName *string           `json:"fontname"`
	FontSize *float64          `json:"fontsize"`
	Width    *float64          `json:"width"`
	Align    int               `json:"align"`
	Render   int               `json:"render_mode"`
	FillOp   *float64          `json:"fill_opacity"`
	Mediabox []float64         `json:"mediabox"`
	CSS      string            `json:"css"`
	Groups   []string          `json:"groups"`
	Meta     map[string]string `json:"meta"`
	Pages    []int             `json:"pages"`
	Font     string            `json:"font"`
	Size     float64           `json:"size"`

	raw map[string]json.RawMessage
}

func (o *writeOp) UnmarshalJSON(b []byte) error {
	type plain writeOp
	if err := json.Unmarshal(b, (*plain)(o)); err != nil {
		return err
	}
	var p2 struct {
		P2 []float64 `json:"p2"`
		H  float64   `json:"h"`
	}
	if err := json.Unmarshal(b, &p2); err != nil {
		return err
	}
	o.P2, o.H = p2.P2, p2.H
	return json.Unmarshal(b, &o.raw)
}

// color reads key as PyMuPDF does: absent is the default, null is None.
func (o *writeOp) color(key string, def Color) Color {
	raw, ok := o.raw[key]
	if !ok {
		return def
	}
	var c Color
	if string(raw) == "null" {
		return nil
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		panic(err)
	}
	return c
}

var longText = func() string {
	var b strings.Builder
	for i := 1; i <= 60; i++ {
		fmt.Fprintf(&b, "<p>Paragraph %d: the second group runs over several pages so the story places it more than once.</p>", i)
	}
	return b.String()
}()

func box(v []float64) Box { return Box{v[0], v[1], v[2], v[3]} }

// runCase runs one case as pymupdf_write_golden.py does; it returns each
// operation's result and the final PDF.
func runCase(t *testing.T, ops []writeOp) ([]any, []byte) {
	var doc *Doc
	var rets []any
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	reopen := func(data []byte) {
		if doc != nil {
			doc.Close()
		}
		var err error
		doc, err = Open(data)
		must(err)
	}
	defer func() {
		if doc != nil {
			doc.Close()
		}
	}()
	for _, op := range ops {
		var ret any
		switch op.Op {
		case "new":
			var err error
			doc, err = New()
			must(err)
		case "open":
			data, err := os.ReadFile(filepath.Join("testdata", op.File))
			must(err)
			reopen(data)
		case "reopen":
			data, err := doc.Bytes()
			must(err)
			reopen(data)
		case "new_page":
			must(doc.NewPage(op.W, op.H))
		case "draw_rect", "draw_line":
			o := NewDrawOptions()
			o.Color = op.color("color", o.Color)
			o.Fill = op.color("fill", nil)
			if op.Width != nil {
				o.Width = *op.Width
			}
			if op.Op == "draw_rect" {
				must(doc.DrawRect(op.Page, box(op.Rect), o))
			} else {
				must(doc.DrawLine(op.Page, op.P1[0], op.P1[1], op.P2[0], op.P2[1], o))
			}
		case "insert_text", "insert_textbox":
			o := NewTextOptions()
			if op.FontName != nil {
				o.FontName = *op.FontName
			}
			if op.FontSize != nil {
				o.FontSize = *op.FontSize
			}
			o.Color = op.color("color", nil)
			o.RenderMode, o.Align = op.Render, op.Align
			if op.FillOp != nil {
				o.FillOpacity = *op.FillOp
			}
			if op.Op == "insert_text" {
				n, err := doc.InsertText(op.Page, op.Point[0], op.Point[1], op.Text, o)
				must(err)
				ret = n
			} else {
				v, err := doc.InsertTextbox(op.Page, box(op.Rect), op.Text, o)
				must(err)
				ret = v
			}
		case "story":
			w, err := NewPDFWriter()
			must(err)
			for _, html := range op.Groups {
				s, err := w.NewStory(strings.ReplaceAll(html, "LONGTEXT", longText), op.CSS)
				must(err)
				for more := true; more; {
					must(w.BeginPage(box(op.Mediabox)))
					more, _, err = s.Place(box(op.Rect))
					must(err)
					must(s.Draw())
					must(w.EndPage())
				}
				s.Close()
			}
			data, err := w.Close()
			must(err)
			reopen(data)
		case "metadata":
			m, err := doc.Metadata()
			must(err)
			ret = m
		case "set_metadata":
			must(doc.SetMetadata(op.Meta))
		case "extract_pages":
			data, err := doc.ExtractPages(op.Pages)
			must(err)
			reopen(data)
		case "text_length":
			v, err := TextLength(op.Font, op.Text, op.Size)
			must(err)
			ret = v
		default:
			t.Fatalf("unknown op %q", op.Op)
		}
		rets = append(rets, ret)
	}
	if doc == nil {
		return rets, nil
	}
	data, err := doc.Bytes()
	must(err)
	return rets, data
}

// TestWritingAgainstPyMuPDF runs testdata/write_cases.json and requires
// PyMuPDF's return values and, for the PDF made, its pages' text and pixels
// (pymupdf_write.jsonl, by scripts/pymupdf_write_golden.py).
func TestWritingAgainstPyMuPDF(t *testing.T) {
	data, err := os.ReadFile("testdata/write_cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name string    `json:"name"`
		Ops  []writeOp `json:"ops"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open("testdata/pymupdf_write.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 16<<20), 16<<20)
	i := 0
	for sc.Scan() {
		var want struct {
			Name    string `json:"name"`
			Returns []any  `json:"returns"`
			Pages   []struct {
				Text    string `json:"text"`
				Samples string `json:"samples"`
			} `json:"pages"`
		}
		if err := json.Unmarshal(sc.Bytes(), &want); err != nil {
			t.Fatal(err)
		}
		c := cases[i]
		i++
		t.Run(c.Name, func(t *testing.T) {
			rets, pdf := runCase(t, c.Ops)
			if ok, diff := sameShape(rets, want.Returns); !ok {
				t.Errorf("returns differ at %s", diff)
			}
			if pdf == nil {
				return
			}
			doc, err := Open(pdf)
			if err != nil {
				t.Fatal(err)
			}
			defer doc.Close()
			if doc.Pages() != len(want.Pages) {
				t.Fatalf("%d pages, PyMuPDF %d", doc.Pages(), len(want.Pages))
			}
			for n, wp := range want.Pages {
				p, err := doc.Page(n)
				if err != nil {
					t.Fatal(err)
				}
				if p.Text != wp.Text {
					t.Errorf("page %d: text %q, PyMuPDF %q", n+1, short(p.Text), short(wp.Text))
				}
				pic, err := doc.Render(n, 72)
				if err != nil {
					t.Fatal(err)
				}
				samples, err := rgbSamples(pic.PNG)
				if err != nil {
					t.Fatal(err)
				}
				if digest(samples) != wp.Samples {
					t.Errorf("page %d: drawn differently from PyMuPDF's", n+1)
				}
			}
		})
	}
	if i != len(cases) {
		t.Errorf("%d cases, %d in the golden", len(cases), i)
	}
}
