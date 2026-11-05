//go:build cgo && !nomupdf

package pdftext

/*
#include <stdlib.h>
#include "nx.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"strings"
	"unsafe"
)

// New is fitz.open(): an empty PDF to write into.
func New() (*Doc, error) {
	ctx := C.nx_new_context()
	if ctx == nil {
		return nil, errors.New("pdftext: no MuPDF context (the headers in include/ and the linked library differ)")
	}
	doc := C.nx_new_pdf(ctx)
	if doc == nil {
		C.fz_drop_context(ctx)
		return nil, errors.New("pdftext: could not create a PDF")
	}
	return &Doc{ctx: ctx, doc: doc}, nil
}

func (d *Doc) cerr(buf []C.char) error { return errors.New("pdftext: " + C.GoString(&buf[0])) }

func (d *Doc) recount() { d.pages = int(C.nx_count_pages(d.ctx, d.doc)) }

// NewPage is doc.new_page(width=…, height=…): a page at the end.
func (d *Doc) NewPage(width, height float64) error {
	var e [256]C.char
	if C.nx_new_page(d.ctx, d.doc, C.float(width), C.float(height), &e[0], 256) != 0 {
		return d.cerr(e[:])
	}
	d.recount()
	return nil
}

func (d *Doc) geom(n int) (pageGeom, error) {
	if n < 0 || n >= d.pages {
		return pageGeom{}, fmt.Errorf("pdftext: no page %d", n+1)
	}
	var v [11]C.float
	var e [256]C.char
	if C.nx_page_geom(d.ctx, d.doc, C.int(n), &v[0], &e[0], 256) != 0 {
		return pageGeom{}, d.cerr(e[:])
	}
	if v[0] != 0 {
		return pageGeom{}, fmt.Errorf("pdftext: page %d is rotated: drawing on it is not supported", n+1)
	}
	ctm := [6]float32{float32(v[5]), float32(v[6]), float32(v[7]), float32(v[8]), float32(v[9]), float32(v[10])}
	inv, ok := invert(ctm)
	if !ok {
		return pageGeom{}, fmt.Errorf("pdftext: page %d: matrix not invertible", n+1)
	}
	return pageGeom{width: float64(v[1]), height: float64(v[2]), x: float64(v[3]), y: float64(v[4]), ipctm: inv}, nil
}

// font is Page.insert_font(fontname): the font the page names so, else a
// new one installed and named so on the page.
func (d *Doc) font(n int, name string) (*fontInfo, error) {
	name = strings.TrimPrefix(name, "/")
	cref := C.CString(name)
	defer C.free(unsafe.Pointer(cref))
	if x := int(C.nx_page_font(d.ctx, d.doc, C.int(n), cref)); x != 0 {
		if f := d.fonts[x]; f != nil {
			return f, nil
		}
		return nil, fmt.Errorf("pdftext: page %d already has a font %q that pdftext did not insert", n+1, name)
	}
	ordering, serif, cjk := cjkFont(name)
	bf, ok := base14[strings.ToLower(name)]
	if !cjk && !ok {
		return nil, fmt.Errorf("pdftext: font %q: only Base-14 and CJK font names are supported", name)
	}
	if bf == "Symbol" || bf == "ZapfDingbats" {
		return nil, fmt.Errorf("pdftext: font %q is not supported", name)
	}
	cbf := C.CString(bf)
	defer C.free(unsafe.Pointer(cbf))
	var xref C.int
	var bname [128]C.char
	var asc, dsc C.float
	var e [256]C.char
	s := C.int(0)
	if serif {
		s = 1
	}
	if C.nx_insert_font(d.ctx, d.doc, C.int(n), cref, cbf, C.int(ordering), s, &xref, &bname[0], 128, &asc, &dsc, &e[0], 256) != 0 {
		return nil, d.cerr(e[:])
	}
	f := &fontInfo{name: C.GoString(&bname[0]), simple: !cjk, ascender: float64(asc), descender: float64(dsc), ordering: -1}
	switch f.name { // get_char_widths: the ordering from the font's name
	case "Fangti", "Ming":
		f.ordering = 0
	case "Heiti", "Song":
		f.ordering = 1
	case "Gothic", "Mincho":
		f.ordering = 2
	case "Dotum", "Batang":
		f.ordering = 3
	}
	if !cjk {
		var g [256]C.int
		var a [256]C.float
		if C.nx_glyph_widths(d.ctx, cbf, 256, &g[0], &a[0]) != 0 {
			return nil, fmt.Errorf("pdftext: font %q: no widths", name)
		}
		f.adv = make([]float64, 256)
		for i := range f.adv {
			f.adv[i] = float64(a[i])
		}
	}
	if d.fonts == nil {
		d.fonts = map[int]*fontInfo{}
	}
	d.fonts[int(xref)] = f
	return f, nil
}

func (d *Doc) opacity(n int, CA, ca float64) (string, error) {
	gs := opacityName(CA, ca)
	if gs == "" {
		return "", nil
	}
	c := C.CString(gs)
	defer C.free(unsafe.Pointer(c))
	var e [256]C.char
	if C.nx_set_opacity(d.ctx, d.doc, C.int(n), c, C.float(CA), C.float(ca), &e[0], 256) != 0 {
		return "", d.cerr(e[:])
	}
	return gs, nil
}

func (d *Doc) commit(n int, cont string, background bool) error {
	if cont == "" {
		return nil
	}
	c := C.CString(cont)
	defer C.free(unsafe.Pointer(c))
	ov := C.int(1)
	if background {
		ov = 0
	}
	var e [256]C.char
	if C.nx_commit(d.ctx, d.doc, C.int(n), c, C.size_t(len(cont)), ov, &e[0], 256) != 0 {
		return d.cerr(e[:])
	}
	return nil
}

// DrawLine is page.draw_line((x1, y1), (x2, y2), …) on page n.
func (d *Doc) DrawLine(n int, x1, y1, x2, y2 float64, o DrawOptions) error {
	g, err := d.geom(n)
	if err != nil {
		return err
	}
	gs, err := d.opacity(n, o.StrokeOpacity, o.FillOpacity)
	if err != nil {
		return err
	}
	cont, err := lineContent(g, x1, y1, x2, y2, o, gs)
	if err != nil {
		return err
	}
	return d.commit(n, cont, o.Background)
}

// DrawRect is page.draw_rect(rect, …) on page n.
func (d *Doc) DrawRect(n int, r Box, o DrawOptions) error {
	g, err := d.geom(n)
	if err != nil {
		return err
	}
	gs, err := d.opacity(n, o.StrokeOpacity, o.FillOpacity)
	if err != nil {
		return err
	}
	cont, err := rectContent(g, r, o, gs)
	if err != nil {
		return err
	}
	return d.commit(n, cont, o.Background)
}

// InsertText is page.insert_text((x, y), text, …) on page n: the number of
// lines written.
func (d *Doc) InsertText(n int, x, y float64, text string, o TextOptions) (int, error) {
	lines := pySplitLines(text)
	if len(lines) == 0 {
		return 0, nil
	}
	g, err := d.geom(n)
	if err != nil {
		return 0, err
	}
	f, err := d.font(n, o.FontName)
	if err != nil {
		return 0, err
	}
	gs, err := d.opacity(n, o.StrokeOpacity, o.FillOpacity)
	if err != nil {
		return 0, err
	}
	cont, lines2, err := textContent(g, x, y, lines, f, o, gs)
	if err != nil {
		return 0, err
	}
	return lines2, d.commit(n, cont, o.Background)
}

// InsertTextbox is page.insert_textbox(rect, text, …) on page n: the
// height left unused, or, negative, how much more the text needs (then
// nothing is written, as in PyMuPDF).
func (d *Doc) InsertTextbox(n int, r Box, text string, o TextOptions) (float64, error) {
	g, err := d.geom(n)
	if err != nil {
		return 0, err
	}
	gs, err := d.opacity(n, o.StrokeOpacity, o.FillOpacity)
	if err != nil {
		return 0, err
	}
	var f *fontInfo
	if text != "" {
		if f, err = d.font(n, o.FontName); err != nil {
			return 0, err
		}
	}
	cont, more, err := textboxContent(g, r, text, f, o, gs)
	if err != nil || more < 0 {
		return more, err
	}
	return more, d.commit(n, cont, o.Background)
}

func (d *Doc) bytes(run func(out *C.nx_json)) ([]byte, error) {
	var out C.nx_json
	run(&out)
	defer C.nx_free_json(d.ctx, &out)
	if out.failed != 0 {
		return nil, errors.New("pdftext: " + C.GoString(&out.err[0]))
	}
	return C.GoBytes(unsafe.Pointer(out.data), C.int(out.n)), nil
}

// Bytes is doc.tobytes(): the PDF as it now is.
func (d *Doc) Bytes() ([]byte, error) {
	return d.bytes(func(out *C.nx_json) { C.nx_tobytes(d.ctx, d.doc, out) })
}

// ExtractPages is a new PDF of the given pages (0-based, in that order),
// each with what it uses, and this document's /Info: a split as PdfWriter
// with add_page and add_metadata makes it.
func (d *Doc) ExtractPages(pages []int) ([]byte, error) {
	if len(pages) == 0 {
		return nil, errors.New("pdftext: no pages")
	}
	cp := make([]C.int, len(pages))
	for i, p := range pages {
		if p < 0 || p >= d.pages {
			return nil, fmt.Errorf("pdftext: no page %d", p+1)
		}
		cp[i] = C.int(p)
	}
	return d.bytes(func(out *C.nx_json) { C.nx_extract_pages(d.ctx, d.doc, &cp[0], C.int(len(cp)), out) })
}

// Metadata is doc.metadata: format, title, author, subject, keywords,
// creator, producer, creationDate, modDate, trapped ("" where unset) and
// encryption (nil for none).
func (d *Doc) Metadata() (map[string]*string, error) {
	var m map[string]*string
	var out C.nx_json
	C.nx_metadata(d.ctx, d.doc, &out)
	return m, d.decode(&out, "metadata", &m)
}

// infoKeys is set_metadata's keymap: the metadata keys /Info holds.
var infoKeys = map[string]string{
	"author": "Author", "producer": "Producer", "creator": "Creator", "title": "Title",
	"creationDate": "CreationDate", "modDate": "ModDate", "subject": "Subject",
	"keywords": "Keywords", "trapped": "Trapped", "format": "", "encryption": "",
}

// SetMetadata is doc.set_metadata(m): each key of Metadata's set in /Info,
// or removed where its value is "", "none" or "null". format and
// encryption are ignored, as in PyMuPDF.
func (d *Doc) SetMetadata(m map[string]string) error {
	for k := range m {
		if _, ok := infoKeys[k]; !ok {
			return fmt.Errorf("pdftext: bad metadata key %q", k)
		}
	}
	for k, v := range m {
		key := infoKeys[k]
		if key == "" {
			continue
		}
		ck := C.CString(key)
		var cv *C.char
		if v != "" && v != "none" && v != "null" {
			cv = C.CString(v)
		}
		var e [256]C.char
		failed := C.nx_set_info(d.ctx, d.doc, ck, cv, &e[0], 256)
		C.free(unsafe.Pointer(ck))
		C.free(unsafe.Pointer(cv))
		if failed != 0 {
			return d.cerr(e[:])
		}
	}
	return nil
}

// TextLength is Font(fontname).text_length(text, fontsize) for a Base-14
// or CJK font name, characters it lacks measured in MuPDF's fallback.
func TextLength(fontname, text string, fontsize float64) (float64, error) {
	ordering, _, cjk := cjkFont(strings.ToLower(fontname))
	if !cjk {
		ordering = -1
	}
	name, ok := base14[strings.ToLower(fontname)]
	if !ok {
		name = fontname
	}
	ctx := C.nx_new_context()
	if ctx == nil {
		return 0, errors.New("pdftext: no MuPDF context")
	}
	defer C.fz_drop_context(ctx)
	codes := make([]C.int, 1, len(text)+1) // codes[0] stays valid for ""
	codes = codes[:0]
	for _, r := range text {
		codes = append(codes, C.int(r))
	}
	cn := C.CString(name)
	defer C.free(unsafe.Pointer(cn))
	l := float64(C.nx_text_length(ctx, cn, C.int(ordering), &codes[:1][0], C.int(len(codes)), C.double(fontsize)))
	if l < 0 {
		return 0, fmt.Errorf("pdftext: font %q", fontname)
	}
	return l, nil
}

// PDFWriter is fitz.DocumentWriter writing a PDF into memory, with its own
// MuPDF context. It is not safe for concurrent use.
type PDFWriter struct {
	ctx *C.fz_context
	w   *C.nx_writer
	out []byte
}

// NewPDFWriter is fitz.DocumentWriter(buffer).
func NewPDFWriter() (*PDFWriter, error) {
	ctx := C.nx_new_context()
	if ctx == nil {
		return nil, errors.New("pdftext: no MuPDF context")
	}
	var e [256]C.char
	w := C.nx_new_writer(ctx, &e[0], 256)
	if w == nil {
		C.fz_drop_context(ctx)
		return nil, errors.New("pdftext: " + C.GoString(&e[0]))
	}
	return &PDFWriter{ctx: ctx, w: w}, nil
}

// BeginPage is writer.begin_page(mediabox).
func (w *PDFWriter) BeginPage(mediabox Box) error {
	var e [256]C.char
	if C.nx_writer_begin(w.ctx, w.w, C.float(mediabox.X0), C.float(mediabox.Y0), C.float(mediabox.X1), C.float(mediabox.Y1), &e[0], 256) != 0 {
		return errors.New("pdftext: " + C.GoString(&e[0]))
	}
	return nil
}

// EndPage is writer.end_page().
func (w *PDFWriter) EndPage() error {
	var e [256]C.char
	if C.nx_writer_end(w.ctx, w.w, &e[0], 256) != 0 {
		return errors.New("pdftext: " + C.GoString(&e[0]))
	}
	return nil
}

// Close is writer.close(): the PDF's bytes. The writer is freed.
func (w *PDFWriter) Close() ([]byte, error) {
	if w.w == nil {
		return w.out, nil
	}
	var data *C.uchar
	var n C.size_t
	var e [256]C.char
	failed := C.nx_writer_close(w.ctx, w.w, &data, &n, &e[0], 256)
	if failed == 0 {
		w.out = C.GoBytes(unsafe.Pointer(data), C.int(n))
	}
	C.nx_drop_writer(w.ctx, w.w)
	C.fz_drop_context(w.ctx)
	w.w = nil
	if failed != 0 {
		return nil, errors.New("pdftext: " + C.GoString(&e[0]))
	}
	return w.out, nil
}

// Story is fitz.Story(html=…, user_css=…): HTML and CSS laid out by MuPDF,
// with its fonts and their fallbacks. A Story belongs to the PDFWriter it
// is made for (it shares its MuPDF context); Close it before the writer.
type Story struct {
	w *PDFWriter
	s *C.fz_story
}

// NewStory makes a story for writer w.
func (w *PDFWriter) NewStory(html, css string) (*Story, error) {
	ch, cc := C.CString(html), C.CString(css)
	defer C.free(unsafe.Pointer(ch))
	defer C.free(unsafe.Pointer(cc))
	var e [256]C.char
	s := C.nx_new_story(w.ctx, ch, cc, &e[0], 256)
	if s == nil {
		return nil, errors.New("pdftext: " + C.GoString(&e[0]))
	}
	return &Story{w: w, s: s}, nil
}

// Place is story.place(where): whether more is left to place, and the part
// of where it filled.
func (s *Story) Place(where Box) (bool, Box, error) {
	in := [4]C.float{C.float(where.X0), C.float(where.Y0), C.float(where.X1), C.float(where.Y1)}
	var f [4]C.float
	var e [256]C.char
	more := C.nx_story_place(s.w.ctx, s.s, &in[0], &f[0], &e[0], 256)
	if more < 0 {
		return false, Box{}, errors.New("pdftext: " + C.GoString(&e[0]))
	}
	return more != 0, Box{float64(f[0]), float64(f[1]), float64(f[2]), float64(f[3])}, nil
}

// Draw is story.draw(device) on the writer's current page.
func (s *Story) Draw() error {
	var e [256]C.char
	if C.nx_story_draw(s.w.ctx, s.s, s.w.w, &e[0], 256) != 0 {
		return errors.New("pdftext: " + C.GoString(&e[0]))
	}
	return nil
}

// Close frees the story.
func (s *Story) Close() {
	if s.s != nil {
		C.nx_drop_story(s.w.ctx, s.s)
		s.s = nil
	}
}
