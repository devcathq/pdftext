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
