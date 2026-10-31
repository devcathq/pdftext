//go:build cgo && !nomupdf

package pdftext

import (
	"bytes"
	"fmt"
	"testing"
)

// testPDF builds a PDF whose pages each show one line of Helvetica text at
// (72, 720). A page's extra entries (e.g. "/Rotate 90") go into its dict;
// image puts a 1x1 image XObject in its resources.
func testPDF(pages []string, extra []string, image []bool) []byte {
	var b bytes.Buffer
	var offs []int
	obj := func(body string) {
		offs = append(offs, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", len(offs), body)
	}
	b.WriteString("%PDF-1.4\n")
	kids := ""
	for i := range pages {
		kids += fmt.Sprintf("%d 0 R ", 5+3*i)
	}
	obj("<< /Type /Catalog /Pages 2 0 R >>")
	obj(fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", kids, len(pages)))
	obj("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	obj("<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8 /Length 1 >>\nstream\n\xff\nendstream")
	for i, text := range pages {
		xobj := ""
		if image[i] {
			xobj = "/XObject << /Im1 4 0 R >>"
		}
		obj(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] %s /Resources << /Font << /F1 3 0 R >> %s >> /Contents %d 0 R >>", extra[i], xobj, 6+3*i))
		stream := fmt.Sprintf("BT /F1 12 Tf 72 720 Td (%s) Tj ET", text)
		obj(fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream))
		obj("<< >>") // keeps three objects per page
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(offs)+1)
	for _, o := range offs {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offs)+1, xref)
	return b.Bytes()
}

func TestPages(t *testing.T) {
	data := testPDF(
		[]string{"Hello World", "Hello World", "Picture"},
		[]string{"", "/Rotate 90", ""},
		[]bool{false, false, true},
	)
	doc, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	defer doc.Close()
	if doc.Pages() != 3 {
		t.Fatalf("pages: %d", doc.Pages())
	}
	first, err := doc.Page(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Words) != 2 || first.Words[0].Text != "Hello" || first.Words[1].Text != "World" {
		t.Fatalf("words: %+v", first.Words)
	}
	if first.Text != "Hello World\n" || first.HasImage {
		t.Fatalf("page 1: text %q, image %v", first.Text, first.HasImage)
	}
	// PyMuPDF reads a rotated page as if it were not rotated.
	rotated, err := doc.Page(1)
	if err != nil {
		t.Fatal(err)
	}
	for i, w := range rotated.Words {
		if w != first.Words[i] {
			t.Fatalf("rotated word %d: %+v, want %+v", i, w, first.Words[i])
		}
	}
	pic, err := doc.Page(2)
	if err != nil || !pic.HasImage {
		t.Fatalf("page 3: image %v, err %v", pic.HasImage, err)
	}
}

func TestNotAPDF(t *testing.T) {
	if _, err := Open([]byte("not a pdf")); err == nil {
		t.Fatal("want an error")
	}
}

func TestBadPage(t *testing.T) {
	doc, err := Open(testPDF([]string{"x"}, []string{""}, []bool{false}))
	if err != nil {
		t.Fatal(err)
	}
	defer doc.Close()
	if _, err := doc.Page(5); err == nil {
		t.Fatal("want an error for a page past the end")
	}
}
