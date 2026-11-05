//go:build cgo && !nomupdf

package pdftext

/*
#cgo CFLAGS: -I${SRCDIR}/include
#cgo LDFLAGS: -lmupdf -lmupdf-third -lm
#cgo linux,amd64 LDFLAGS: -L${SRCDIR}/lib/linux_amd64
#cgo linux,arm64 LDFLAGS: -L${SRCDIR}/lib/linux_arm64
#cgo darwin,amd64 LDFLAGS: -L${SRCDIR}/lib/darwin_amd64
#cgo darwin,arm64 LDFLAGS: -L${SRCDIR}/lib/darwin_arm64
#cgo windows,amd64 LDFLAGS: -L${SRCDIR}/lib/windows_amd64
#include <stdlib.h>
#include "nx.h"
*/
import "C"

import (
	"encoding/json"
	"errors"
	"fmt"
	"unsafe"
)

// Doc is one open PDF with its own MuPDF context. It is not safe for
// concurrent use: open one Doc per goroutine.
type Doc struct {
	ctx   *C.fz_context
	doc   *C.fz_document
	data  unsafe.Pointer
	pages int
	fonts map[int]*fontInfo // FontInfos: the fonts InsertText and InsertTextbox installed, by xref
}

// Open opens a PDF from memory. The bytes are copied, so data may be reused.
func Open(data []byte) (*Doc, error) {
	ctx := C.nx_new_context()
	if ctx == nil {
		return nil, errors.New("pdftext: no MuPDF context (the headers in include/ and the linked library differ)")
	}
	buf := C.CBytes(data)
	doc := C.nx_open(ctx, (*C.uchar)(buf), C.size_t(len(data)))
	if doc == nil {
		C.free(buf)
		C.fz_drop_context(ctx)
		return nil, errors.New("pdftext: not a readable PDF")
	}
	d := &Doc{ctx: ctx, doc: doc, data: buf}
	if d.pages = int(C.nx_count_pages(ctx, doc)); d.pages < 0 {
		d.Close()
		return nil, errors.New("pdftext: page count failed")
	}
	return d, nil
}

// Pages is the number of pages.
func (d *Doc) Pages() int { return d.pages }

// Close frees the document and its context.
func (d *Doc) Close() {
	C.fz_drop_document(d.ctx, d.doc)
	C.fz_drop_context(d.ctx)
	C.free(d.data)
}

// Page reads page n (0-based).
func (d *Doc) Page(n int) (Page, error) {
	var out C.nx_page
	C.nx_read_page(d.ctx, d.doc, C.int(n), &out)
	defer C.nx_free_page(&out)
	if out.failed != 0 {
		return Page{}, fmt.Errorf("pdftext: page %d could not be read", n+1)
	}
	text := C.GoBytes(unsafe.Pointer(out.text), out.ntext)
	words := make([]Word, 0, int(out.nwords))
	for _, w := range unsafe.Slice(out.words, int(out.nwords)) {
		words = append(words, Word{
			X0: float32(w.x0), Y0: float32(w.y0), X1: float32(w.x1), Y1: float32(w.y1),
			Text: string(text[w.off : w.off+w.len]),
		})
	}
	return Page{
		Words:    words,
		Text:     C.GoStringN(out.plain, out.nplain),
		HasImage: out.has_image != 0,
	}, nil
}

// Bounds is page n's page.rect: its box with the page's rotation applied.
func (d *Doc) Bounds(n int) (Rect, error) {
	var r C.nx_rect
	if C.nx_bound(d.ctx, d.doc, C.int(n), &r) != 0 {
		return Rect{}, fmt.Errorf("pdftext: page %d could not be read", n+1)
	}
	return Rect{float32(r.x0), float32(r.y0), float32(r.x1), float32(r.y1)}, nil
}

// Render draws page n as page.get_pixmap(dpi=dpi).tobytes("png") does: RGB,
// no alpha, on white, annotations included, the page's rotation applied,
// at dpi/72 pixels per point, with dpi as the PNG's resolution.
func (d *Doc) Render(n, dpi int) (Picture, error) {
	if dpi <= 0 {
		return Picture{}, fmt.Errorf("pdftext: dpi %d", dpi)
	}
	var out C.nx_picture
	C.nx_render(d.ctx, d.doc, C.int(n), C.int(dpi), &out)
	defer C.nx_free_picture(d.ctx, &out)
	if out.failed != 0 {
		return Picture{}, fmt.Errorf("pdftext: page %d could not be drawn", n+1)
	}
	return Picture{
		PNG:   C.GoBytes(unsafe.Pointer(out.png), C.int(out.npng)),
		Width: int(out.w), Height: int(out.h), White: out.white != 0,
	}, nil
}

// ImageBoxes is the bbox of each image page n draws, as page.get_image_info()
// lists them: in drawing order, read with the page derotated.
func (d *Doc) ImageBoxes(n int) ([]Rect, error) {
	var out C.nx_boxes
	C.nx_image_boxes(d.ctx, d.doc, C.int(n), &out)
	defer C.nx_free_boxes(&out)
	if out.failed != 0 {
		return nil, fmt.Errorf("pdftext: page %d could not be read", n+1)
	}
	boxes := make([]Rect, 0, int(out.n))
	for _, b := range unsafe.Slice(out.boxes, int(out.n)) {
		boxes = append(boxes, Rect{float32(b.x0), float32(b.y0), float32(b.x1), float32(b.y1)})
	}
	return boxes, nil
}

// read runs one of the JSON readers of nx_text.c, nx_vector.c, … and
// decodes what it wrote into v.
func (d *Doc) read(n int, what string, v any, run func(out *C.nx_json)) error {
	if n < 0 || n >= d.pages {
		return fmt.Errorf("pdftext: no page %d", n+1)
	}
	var out C.nx_json
	run(&out)
	return d.decode(&out, fmt.Sprintf("page %d: %s", n+1, what), v)
}

// Blocks is page n's get_text("blocks").
func (d *Doc) Blocks(n int) ([]Block, error) {
	var b []Block
	err := d.read(n, "blocks", &b, func(out *C.nx_json) { C.nx_blocks(d.ctx, d.doc, C.int(n), out) })
	return b, err
}

// RawDict is page n's get_text("rawdict").
func (d *Doc) RawDict(n int) (TextDict, error) {
	var t TextDict
	err := d.read(n, "rawdict", &t, func(out *C.nx_json) { C.nx_rawdict(d.ctx, d.doc, C.int(n), out) })
	return t, err
}

// Drawings is page n's get_drawings(extended=extended).
func (d *Doc) Drawings(n int, extended bool) ([]Path, error) {
	var p []Path
	ext := C.int(0)
	if extended {
		ext = 1
	}
	err := d.read(n, "drawings", &p, func(out *C.nx_json) { C.nx_drawings(d.ctx, d.doc, C.int(n), ext, out) })
	return p, err
}

// Images is page n's get_images(full=True).
func (d *Doc) Images(n int) ([]ImageRef, error) {
	var r []ImageRef
	err := d.read(n, "images", &r, func(out *C.nx_json) { C.nx_images(d.ctx, d.doc, C.int(n), out) })
	return r, err
}

// Image is Pixmap(doc, xref) with its tobytes("png"); with rgb, the
// pixmap is converted first as Pixmap(csRGB, pix) does (alpha kept).
func (d *Doc) Image(xref int, rgb bool) (Image, error) {
	var img Image
	conv := C.int(0)
	if rgb {
		conv = 1
	}
	var out C.nx_json
	C.nx_image(d.ctx, d.doc, C.int(xref), conv, &out)
	err := d.decode(&out, fmt.Sprintf("image %d", xref), &img)
	return img, err
}

// ImageRects is page n's get_image_rects(xref): where the page draws that
// image, matched by its pixels' digest.
func (d *Doc) ImageRects(n, xref int) ([]Rect, error) {
	var r []Rect
	err := d.read(n, "image rects", &r, func(out *C.nx_json) { C.nx_image_rects(d.ctx, d.doc, C.int(n), C.int(xref), out) })
	return r, err
}

// XrefLength is doc.xref_length(): the size of the xref table, so objects
// are 1 to XrefLength()-1.
func (d *Doc) XrefLength() int { return int(C.nx_xref_len(d.ctx, d.doc)) }

// XrefObject is doc.xref_object(xref): the object as PDF source; xref -1
// is the trailer.
func (d *Doc) XrefObject(xref int) (string, error) {
	var s string
	var out C.nx_json
	C.nx_xref_object(d.ctx, d.doc, C.int(xref), &out)
	err := d.decode(&out, fmt.Sprintf("xref %d", xref), &s)
	return s, err
}

// decode reads what a reader wrote into out, then frees it.
func (d *Doc) decode(out *C.nx_json, what string, v any) error {
	defer C.nx_free_json(d.ctx, out)
	if out.failed != 0 {
		return fmt.Errorf("pdftext: %s: %s", what, C.GoString(&out.err[0]))
	}
	if err := json.Unmarshal(C.GoBytes(unsafe.Pointer(out.data), C.int(out.n)), v); err != nil {
		return fmt.Errorf("pdftext: %s: %w", what, err)
	}
	return nil
}

// FindTables is page n's find_tables() with PyMuPDF's default settings (the
// "lines" strategy): the tables of its TableFinder, with their cells, rows
// and text. As in PyMuPDF, a rotated page is derotated to be read, which
// leaves it with one more (blank) content stream and without its CropBox
// and other boxes but the MediaBox. Table headers (table.header) are not
// computed.
func (d *Doc) FindTables(n int) ([]Table, error) {
	var in tableInput
	if err := d.read(n, "find_tables", &in, func(out *C.nx_json) { C.nx_table_page(d.ctx, d.doc, C.int(n), out) }); err != nil {
		return nil, err
	}
	return findTables(in, n), nil
}

// ResolveNames is doc.resolve_names(): the named destinations of the
// catalog's /Dests and its /Names/Dests tree, by name.
func (d *Doc) ResolveNames() (map[string]Dest, error) {
	var in destInput
	var out C.nx_json
	C.nx_dest_names(d.ctx, d.doc, &out)
	if err := d.decode(&out, "resolve_names", &in); err != nil {
		return nil, err
	}
	return resolveNames(in)
}
