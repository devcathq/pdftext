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

// A page's /Rotate as PyMuPDF normalizes it: 0, 90, 180 or 270, and 0 for
// anything that is not a multiple of 90.
static inline int nx_norm_rotation(int r) {
	while (r < 0) r += 360;
	while (r >= 360) r -= 360;
	return r % 90 ? 0 : r;
}

// A box in points, as MuPDF's fz_rect.
typedef struct { float x0, y0, x1, y1; } nx_rect;

// A page drawn as PNG: the bytes (in buf, a MuPDF buffer), its size in
// pixels, and whether every sample is white.
typedef struct {
	fz_buffer *buf; unsigned char *png; size_t npng;
	int w, h, white;
	int failed;
} nx_picture;

// The boxes of the images a page draws.
typedef struct { nx_rect *boxes; int n, cap; int failed; } nx_boxes;

fz_context *nx_new_context(void);
fz_document *nx_open(fz_context *ctx, unsigned char *data, size_t len);
int nx_count_pages(fz_context *ctx, fz_document *doc);
void nx_read_page(fz_context *ctx, fz_document *doc, int pno, nx_page *out);
void nx_free_page(nx_page *p);
int nx_bound(fz_context *ctx, fz_document *doc, int pno, nx_rect *out);
void nx_render(fz_context *ctx, fz_document *doc, int pno, int dpi, nx_picture *out);
void nx_free_picture(fz_context *ctx, nx_picture *p);
void nx_image_boxes(fz_context *ctx, fz_document *doc, int pno, nx_boxes *out);
void nx_free_boxes(nx_boxes *b);

// What the readers below give Go: JSON in a MuPDF buffer (data, n), or
// failed with the reason in err.
typedef struct {
	fz_buffer *buf; unsigned char *data; size_t n;
	int failed; char err[256];
} nx_json;

void nx_free_json(fz_context *ctx, nx_json *j);
void nx_blocks(fz_context *ctx, fz_document *doc, int pno, nx_json *out);
void nx_rawdict(fz_context *ctx, fz_document *doc, int pno, nx_json *out);
void nx_drawings(fz_context *ctx, fz_document *doc, int pno, int extended, nx_json *out);

// Shared by the readers (nx.c, nx_json.c).
fz_stext_page *nx_textpage(fz_context *ctx, fz_document *doc, int pno, int flags);
fz_rect nx_char_bbox(fz_context *ctx, fz_stext_line *line, fz_stext_char *ch);
int nx_overlap(fz_rect a, fz_rect b);
void nx_put_rune(char **buf, int *n, int *cap, int c);
void nx_j_raw(fz_context *ctx, fz_buffer *b, const char *s);
void nx_j_str(fz_context *ctx, fz_buffer *b, const char *s, size_t n);
void nx_j_num(fz_context *ctx, fz_buffer *b, float f);
void nx_j_double(fz_context *ctx, fz_buffer *b, double f);
void nx_j_int(fz_context *ctx, fz_buffer *b, long long v);
void nx_j_rect(fz_context *ctx, fz_buffer *b, fz_rect r);
void nx_j_point(fz_context *ctx, fz_buffer *b, fz_point p);
void nx_j_bytes(fz_context *ctx, fz_buffer *b, const unsigned char *data, size_t n);
void nx_json_done(fz_context *ctx, fz_buffer *b, nx_json *out);
void nx_json_fail(fz_context *ctx, fz_buffer *b, nx_json *out);

#endif
