//go:build cgo && !nomupdf

#ifndef NX_MUPDF_H
#define NX_MUPDF_H

#include <mupdf/fitz.h>

// One word of PyMuPDF's extractWORDS: its box and its UTF-8 text at
// text[off:off+len] of the page's buffer.
typedef struct { float x0, y0, x1, y1; int off, len; } nx_word;

// What one page gives: the words, the plain text (TextPage.extractText) and
// whether the page lists an image (Page.get_images).
typedef struct {
	nx_word *words; int nwords, capw;
	char *text; int ntext, capt;
	char *plain; int nplain, capp;
	int has_image;
	int failed;
} nx_page;

fz_context *nx_new_context(void);
fz_document *nx_open(fz_context *ctx, unsigned char *data, size_t len);
int nx_count_pages(fz_context *ctx, fz_document *doc);
void nx_read_page(fz_context *ctx, fz_document *doc, int pno, nx_page *out);
void nx_free_page(nx_page *p);

#endif
