//go:build cgo && !nomupdf

// What PyMuPDF 1.28.0 does for page.get_text("blocks") and
// page.get_text("rawdict") (src/extra.i: _as_blocks, JM_make_textpage_dict,
// JM_make_spanlist, JM_make_image_block), over a text page read with the
// page derotated and PyMuPDF's default flags for each.

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include <mupdf/pdf.h>

#include "nx.h"

#define FLAGS_TEXT (FZ_STEXT_PRESERVE_LIGATURES | FZ_STEXT_PRESERVE_WHITESPACE | \
	FZ_STEXT_MEDIABOX_CLIP | FZ_STEXT_USE_CID_FOR_UNKNOWN_UNICODE)
#define FLAGS_DICT (FLAGS_TEXT | FZ_STEXT_PRESERVE_IMAGES)

// A text built rune by rune, as JM_append_rune and JM_EscapeStrFromBuffer
// give it.
typedef struct { char *s; int n, cap; } text;

static void put_block(fz_context *ctx, fz_buffer *b, int *first, fz_rect r, text *t, int no, int type) {
	nx_j_raw(ctx, b, *first ? "{\"bbox\":" : ",{\"bbox\":");
	*first = 0;
	nx_j_rect(ctx, b, r);
	nx_j_raw(ctx, b, ",\"text\":");
	nx_j_str(ctx, b, t->s ? t->s : "", t->n);
	nx_j_raw(ctx, b, ",\"no\":");
	nx_j_int(ctx, b, no);
	nx_j_raw(ctx, b, ",\"type\":");
	nx_j_int(ctx, b, type);
	nx_j_raw(ctx, b, "}");
}

static void put_ascii(text *t, const char *s) {
	for (; *s; s++) nx_put_rune(&t->s, &t->n, &t->cap, (unsigned char)*s);
}

// as_blocks is _as_blocks: one entry per text, image or vector block, in
// block order, a structure block's blocks in its place. A block's box is
// that of its lines, each the box of the characters that touch the page.
static int as_blocks(fz_context *ctx, fz_buffer *b, int *first, fz_rect tp, fz_stext_block *block, int n, text *t) {
	fz_rect box = fz_empty_rect;
	char s[512];
	for (; block; block = block->next) {
		int have = 0;
		t->n = 0;
		switch (block->type) {
		case FZ_STEXT_BLOCK_STRUCT:
			if (block->u.s.down) n = as_blocks(ctx, b, first, tp, block->u.s.down->first_block, n, t);
			break;
		case FZ_STEXT_BLOCK_TEXT: {
			box = fz_empty_rect;
			int last = 10;
			for (fz_stext_line *line = block->u.t.first_line; line; line = line->next) {
				fz_rect lr = fz_empty_rect;
				for (fz_stext_char *ch = line->first_char; ch; ch = ch->next) {
					fz_rect cb = nx_char_bbox(ctx, line, ch);
					if (!nx_overlap(tp, cb) && !fz_is_infinite_rect(tp)) continue;
					nx_put_rune(&t->s, &t->n, &t->cap, ch->c);
					last = ch->c;
					lr = fz_union_rect(lr, cb);
				}
				if (last != 10 && !fz_is_empty_rect(lr)) nx_put_rune(&t->s, &t->n, &t->cap, 10);
				box = fz_union_rect(box, lr);
			}
			have = 1;
			break;
		}
		case FZ_STEXT_BLOCK_IMAGE:
			if (fz_contains_rect(tp, block->bbox) || fz_is_infinite_rect(tp)) {
				fz_image *img = block->u.i.image;
				box = block->bbox;
				snprintf(s, sizeof s, "<image: %s, width: %d, height: %d, bpc: %d>\n",
					fz_colorspace_name(ctx, img->colorspace), img->w, img->h, img->bpc);
				put_ascii(t, s);
				have = 1;
			}
			break;
		case FZ_STEXT_BLOCK_VECTOR:
			if (nx_overlap(tp, block->bbox) || fz_is_infinite_rect(tp)) {
				uint32_t f = block->u.v.flags;
				box = block->bbox;
				snprintf(s, sizeof s, "\n<vector %s, color: #%06x, alpha: %i, is-rect: %s, continues: %s>\n",
					(f & FZ_STEXT_VECTOR_IS_STROKED) ? "stroked" : "filled",
					(unsigned)(block->u.v.argb & 0xffffff), (int)(block->u.v.argb >> 24),
					(f & FZ_STEXT_VECTOR_IS_RECTANGLE) ? "true" : "false",
					(f & FZ_STEXT_VECTOR_CONTINUES) ? "true" : "false");
				put_ascii(t, s);
				have = 1;
			}
			break;
		}
		if (have) put_block(ctx, b, first, box, t, ++n, block->type);
	}
	return n;
}

void nx_blocks(fz_context *ctx, fz_document *doc, int pno, nx_json *out) {
	fz_stext_page *tp = NULL;
	fz_buffer *b = NULL;
	text t = {0};
	fz_var(tp); fz_var(b);
	fz_try(ctx) {
		b = fz_new_buffer(ctx, 4096);
		tp = nx_textpage(ctx, doc, pno, FLAGS_TEXT);
		int first = 1;
		nx_j_raw(ctx, b, "[");
		as_blocks(ctx, b, &first, tp->mediabox, tp->first_block, -1, &t);
		nx_j_raw(ctx, b, "]");
		nx_json_done(ctx, b, out);
	}
	fz_always(ctx) {
		fz_drop_stext_page(ctx, tp);
		free(t.s);
	}
	fz_catch(ctx) { nx_json_fail(ctx, b, out); }
}

// font_name is JM_font_name read back by JM_EscapeStrFromStr: a subset
// prefix ("ABCDEF+") dropped, the bytes read as raw-unicode-escape (Latin-1,
// with \uXXXX and \UXXXXXXXX escapes).
static void font_name(fz_context *ctx, fz_font *font, text *t) {
	const char *name = fz_font_name(ctx, font), *plus = strchr(name, '+');
	if (plus && plus - name == 6) name = plus + 1;
	nx_put_raw_escape(&t->s, &t->n, &t->cap, name, strlen(name));
}

// char_flags is JM_char_font_flags: superscript 1, italic 2, serif 4,
// monospaced 8, bold 16.
static int char_flags(fz_context *ctx, fz_stext_line *line, fz_stext_char *ch) {
	int f = 0;
	if (line->wmode == 0 && line->dir.x == 1 && line->dir.y == 0)
		f = ch->origin.y < line->first_char->origin.y - ch->size * 0.1f;
	f += fz_font_is_italic(ctx, ch->font) * 2;
	f += fz_font_is_serif(ctx, ch->font) * 4;
	f += fz_font_is_monospaced(ctx, ch->font) * 8;
	f += fz_font_is_bold(ctx, ch->font) * 16;
	return f;
}

typedef struct {
	float size; int flags; unsigned cflags; text font; uint32_t argb;
} style;

static int same_style(style *a, style *b) {
	return a->size == b->size && a->flags == b->flags && a->cflags == b->cflags && a->argb == b->argb &&
		a->font.n == b->font.n && !memcmp(a->font.s, b->font.s, a->font.n);
}

static void put_char(fz_context *ctx, fz_buffer *b, int first, fz_stext_char *ch, fz_rect r, text *tmp) {
	nx_j_raw(ctx, b, first ? "{\"origin\":" : ",{\"origin\":");
	nx_j_point(ctx, b, ch->origin);
	nx_j_raw(ctx, b, ",\"bbox\":");
	nx_j_rect(ctx, b, r);
	tmp->n = 0;
	int c = ch->c;
	if (c < 0 || c > 0x10ffff || (c >= 0xd800 && c <= 0xdfff)) c = 0xfffd;
	nx_put_rune(&tmp->s, &tmp->n, &tmp->cap, c);
	nx_j_raw(ctx, b, ",\"c\":");
	nx_j_str(ctx, b, tmp->s, tmp->n);
	nx_j_raw(ctx, b, (ch->flags & FZ_STEXT_SYNTHETIC) ? ",\"synthetic\":true}" : ",\"synthetic\":false}");
}

// open_span writes a span's style; its chars follow, then close_span.
static void open_span(fz_context *ctx, fz_buffer *b, int first, style *st, fz_font *font) {
	float asc = fz_font_ascender(ctx, font), dsc = fz_font_descender(ctx, font);
	if (asc < 1e-3f) { asc = 0.9f; dsc = -0.1f; }
	nx_j_raw(ctx, b, first ? "{\"size\":" : ",{\"size\":");
	nx_j_num(ctx, b, st->size);
	nx_j_raw(ctx, b, ",\"flags\":"); nx_j_int(ctx, b, st->flags);
	nx_j_raw(ctx, b, ",\"bidi\":0,\"char_flags\":"); nx_j_int(ctx, b, st->cflags);
	nx_j_raw(ctx, b, ",\"font\":"); nx_j_str(ctx, b, st->font.s ? st->font.s : "", st->font.n);
	nx_j_raw(ctx, b, ",\"color\":"); nx_j_int(ctx, b, st->argb & 0xffffff);
	nx_j_raw(ctx, b, ",\"alpha\":"); nx_j_int(ctx, b, st->argb >> 24);
	nx_j_raw(ctx, b, ",\"ascender\":"); nx_j_num(ctx, b, asc);
	nx_j_raw(ctx, b, ",\"descender\":"); nx_j_num(ctx, b, dsc);
	nx_j_raw(ctx, b, ",\"chars\":[");
}

static void close_span(fz_context *ctx, fz_buffer *b, fz_point origin, fz_rect r) {
	nx_j_raw(ctx, b, "],\"origin\":");
	nx_j_point(ctx, b, origin);
	nx_j_raw(ctx, b, ",\"bbox\":");
	nx_j_rect(ctx, b, r);
	nx_j_raw(ctx, b, "}");
}

// line_spans is JM_make_spanlist with raw=1: a new span wherever size,
// flags, char flags, colour or font change. Every span is kept but the
// last, which is dropped when its box is empty; a dropped span's JSON is cut
// back off the buffer.
static fz_rect line_spans(fz_context *ctx, fz_buffer *b, fz_rect tp, fz_stext_line *line, style *old, style *cur, text *tmp) {
	fz_rect span = fz_empty_rect, lr = fz_empty_rect;
	fz_point origin = {0, 0};
	int open = 0, nspans = 0, nchars = 0;
	size_t start = 0;
	old->size = -1;
	for (fz_stext_char *ch = line->first_char; ch; ch = ch->next) {
		fz_rect r = nx_char_bbox(ctx, line, ch);
		if (!nx_overlap(tp, r) && !fz_is_infinite_rect(tp)) continue;
		cur->size = ch->size;
		cur->flags = char_flags(ctx, line, ch);
		cur->cflags = ch->flags & ~FZ_STEXT_SYNTHETIC;
		cur->argb = ch->argb;
		cur->font.n = 0;
		font_name(ctx, ch->font, &cur->font);
		if (!same_style(cur, old)) {
			if (old->size >= 0) {
				close_span(ctx, b, origin, span);
				lr = fz_union_rect(lr, span);
			}
			start = fz_buffer_storage(ctx, b, NULL);
			open_span(ctx, b, nspans++ == 0, cur, ch->font);
			open = 1;
			nchars = 0;
			text keep = old->font;
			*old = *cur;
			old->font = keep;
			old->font.n = 0;
			for (int i = 0; i < cur->font.n; i++) {
				if (old->font.n == old->font.cap) {
					old->font.cap = old->font.cap ? old->font.cap * 2 : 64;
					old->font.s = realloc(old->font.s, old->font.cap);
				}
				old->font.s[old->font.n++] = cur->font.s[i];
			}
			span = r;
			origin = ch->origin;
		}
		span = fz_union_rect(span, r);
		put_char(ctx, b, nchars++ == 0, ch, r, tmp);
	}
	if (open) {
		if (!fz_is_empty_rect(span)) {
			close_span(ctx, b, origin, span);
			lr = fz_union_rect(lr, span);
		} else {
			b->len = start; // with the comma before it, if a span came first
		}
	}
	return lr;
}

// image_block is JM_make_image_block: the image as its stored stream where
// PyMuPDF keeps that (JPEG, JPX, PNG, …), else as PNG; a CMYK JPEG as JPEG
// again; the mask as PNG.
static void image_block(fz_context *ctx, fz_buffer *b, fz_stext_block *block) {
	fz_image *image = block->u.i.image;
	fz_compressed_buffer *cbuf = fz_compressed_image_buffer(ctx, image);
	fz_buffer *buf = NULL, *freebuf = NULL, *maskbuf = NULL;
	int n = fz_colorspace_n(ctx, image->colorspace), type = FZ_IMAGE_UNKNOWN;
	const char *ext = "";
	static const char *exts[] = {"n/a", "raw", "flate", "lzw", "rld", "bmp", "gif", "jb2", "jpeg", "jpx", "jxr", "png", "pnm", "tiff"};
	if (cbuf) {
		type = cbuf->params.type;
		ext = "n/a";
		switch (type) {
		case FZ_IMAGE_RAW: ext = exts[1]; break;
		case FZ_IMAGE_FLATE: ext = exts[2]; break;
		case FZ_IMAGE_LZW: ext = exts[3]; break;
		case FZ_IMAGE_RLD: ext = exts[4]; break;
		case FZ_IMAGE_BMP: ext = exts[5]; break;
		case FZ_IMAGE_GIF: ext = exts[6]; break;
		case FZ_IMAGE_JBIG2: ext = exts[7]; break;
		case FZ_IMAGE_JPEG: ext = exts[8]; break;
		case FZ_IMAGE_JPX: ext = exts[9]; break;
		case FZ_IMAGE_JXR: ext = exts[10]; break;
		case FZ_IMAGE_PNG: ext = exts[11]; break;
		case FZ_IMAGE_PNM: ext = exts[12]; break;
		case FZ_IMAGE_TIFF: ext = exts[13]; break;
		}
	}
	if (type < FZ_IMAGE_BMP || type == FZ_IMAGE_JBIG2) type = FZ_IMAGE_UNKNOWN;
	fz_var(buf); fz_var(freebuf); fz_var(maskbuf); fz_var(ext);
	fz_try(ctx) {
		if (!cbuf || type == FZ_IMAGE_UNKNOWN) {
			buf = freebuf = fz_new_buffer_from_image_as_png(ctx, image, fz_default_color_params);
			ext = "png";
		} else if (n == 4 && !strcmp(ext, "jpeg")) {
			buf = freebuf = fz_new_buffer_from_image_as_jpeg(ctx, image, fz_default_color_params, 95, 1);
		} else {
			buf = cbuf->buffer;
		}
		if (image->mask) maskbuf = fz_new_buffer_from_image_as_png(ctx, image->mask, fz_default_color_params);
	}
	fz_catch(ctx) {
		// PyMuPDF ignores the error and writes what it has.
	}
	unsigned char *data = NULL, *mdata = NULL;
	size_t nd = buf ? fz_buffer_storage(ctx, buf, &data) : 0;
	size_t nm = maskbuf ? fz_buffer_storage(ctx, maskbuf, &mdata) : 0;
	fz_matrix m = block->u.i.transform;
	nx_j_raw(ctx, b, ",\"width\":"); nx_j_int(ctx, b, image->w);
	nx_j_raw(ctx, b, ",\"height\":"); nx_j_int(ctx, b, image->h);
	nx_j_raw(ctx, b, ",\"ext\":"); nx_j_str(ctx, b, ext, strlen(ext));
	nx_j_raw(ctx, b, ",\"colorspace\":"); nx_j_int(ctx, b, n);
	nx_j_raw(ctx, b, ",\"xres\":"); nx_j_int(ctx, b, image->xres);
	nx_j_raw(ctx, b, ",\"yres\":"); nx_j_int(ctx, b, image->xres); // sic: PyMuPDF writes xres twice
	nx_j_raw(ctx, b, ",\"bpc\":"); nx_j_int(ctx, b, image->bpc);
	nx_j_raw(ctx, b, ",\"transform\":[");
	nx_j_num(ctx, b, m.a); nx_j_raw(ctx, b, ","); nx_j_num(ctx, b, m.b); nx_j_raw(ctx, b, ",");
	nx_j_num(ctx, b, m.c); nx_j_raw(ctx, b, ","); nx_j_num(ctx, b, m.d); nx_j_raw(ctx, b, ",");
	nx_j_num(ctx, b, m.e); nx_j_raw(ctx, b, ","); nx_j_num(ctx, b, m.f);
	nx_j_raw(ctx, b, "],\"size\":"); nx_j_int(ctx, b, (long long)nd);
	nx_j_raw(ctx, b, ",\"image\":"); nx_j_bytes(ctx, b, data, nd);
	nx_j_raw(ctx, b, ",\"mask\":");
	if (maskbuf) nx_j_bytes(ctx, b, mdata, nm);
	else nx_j_raw(ctx, b, "null");
	fz_drop_buffer(ctx, maskbuf);
	fz_drop_buffer(ctx, freebuf);
}

// as_dict is _as_dict with raw=1 for the blocks TEXTFLAGS_RAWDICT makes:
// text blocks touching the page and image blocks inside it.
static int as_dict(fz_context *ctx, fz_buffer *b, int *first, fz_rect tp, fz_stext_block *block, int n, style *old, style *cur, text *tmp) {
	for (; block; block = block->next) {
		if (block->type == FZ_STEXT_BLOCK_TEXT && (nx_overlap(tp, block->bbox) || fz_is_infinite_rect(tp))) {
			nx_j_raw(ctx, b, *first ? "{\"type\":0,\"number\":" : ",{\"type\":0,\"number\":");
			*first = 0;
			nx_j_int(ctx, b, ++n);
			nx_j_raw(ctx, b, ",\"flags\":");
			nx_j_int(ctx, b, block->u.t.flags);
			nx_j_raw(ctx, b, ",\"lines\":[");
			fz_rect br = fz_empty_rect;
			int nl = 0;
			for (fz_stext_line *line = block->u.t.first_line; line; line = line->next) {
				if (fz_is_empty_rect(fz_intersect_rect(tp, line->bbox)) && !fz_is_infinite_rect(tp)) continue;
				nx_j_raw(ctx, b, nl++ ? ",{\"spans\":[" : "{\"spans\":[");
				fz_rect lr = line_spans(ctx, b, tp, line, old, cur, tmp);
				br = fz_union_rect(br, lr);
				nx_j_raw(ctx, b, "],\"wmode\":");
				nx_j_int(ctx, b, line->wmode);
				nx_j_raw(ctx, b, ",\"dir\":");
				nx_j_point(ctx, b, line->dir);
				nx_j_raw(ctx, b, ",\"bbox\":");
				nx_j_rect(ctx, b, lr);
				nx_j_raw(ctx, b, "}");
			}
			nx_j_raw(ctx, b, "],\"bbox\":");
			nx_j_rect(ctx, b, br);
			nx_j_raw(ctx, b, "}");
		} else if (block->type == FZ_STEXT_BLOCK_IMAGE && (fz_contains_rect(tp, block->bbox) || fz_is_infinite_rect(tp))) {
			nx_j_raw(ctx, b, *first ? "{\"type\":1,\"number\":" : ",{\"type\":1,\"number\":");
			*first = 0;
			nx_j_int(ctx, b, ++n);
			nx_j_raw(ctx, b, ",\"bbox\":");
			nx_j_rect(ctx, b, block->bbox);
			image_block(ctx, b, block);
			nx_j_raw(ctx, b, "}");
		}
		// Structure, vector and grid blocks come only with flags the default
		// leaves out (TEXT_COLLECT_STRUCTURE, _VECTORS, TEXT_TABLE_HUNT).
	}
	return n;
}

// cropbox_size is page.cropbox's width and height (JM_cropbox): Python
// subtracts the float32 corners in double, and a negative size is 0.
static void cropbox_size(fz_context *ctx, pdf_obj *page, double *w, double *h) {
	fz_rect m = pdf_to_rect(ctx, pdf_dict_get_inheritable(ctx, page, PDF_NAME(MediaBox)));
	if (fz_is_empty_rect(m) || fz_is_infinite_rect(m)) m = fz_make_rect(0, 0, 612, 792);
	fz_rect pm = {fz_min(m.x0, m.x1), fz_min(m.y0, m.y1), fz_max(m.x0, m.x1), fz_max(m.y0, m.y1)};
	if (pm.x1 - pm.x0 < 1 || pm.y1 - pm.y0 < 1) pm = fz_unit_rect;
	fz_rect c = pdf_to_rect(ctx, pdf_dict_get_inheritable(ctx, page, PDF_NAME(CropBox)));
	if (fz_is_infinite_rect(c) || fz_is_empty_rect(c)) c = pm;
	float y0 = pm.y1 - c.y1, y1 = pm.y1 - c.y0;
	double dw = (double)c.x1 - (double)c.x0, dh = (double)y1 - (double)y0;
	*w = dw > 0 ? dw : 0;
	*h = dh > 0 ? dh : 0;
}

void nx_rawdict(fz_context *ctx, fz_document *doc, int pno, nx_json *out) {
	fz_stext_page *tp = NULL;
	fz_buffer *b = NULL;
	style old = {0}, cur = {0};
	text tmp = {0};
	fz_var(tp); fz_var(b);
	fz_try(ctx) {
		b = fz_new_buffer(ctx, 1 << 16);
		tp = nx_textpage(ctx, doc, pno, FLAGS_DICT);
		int first = 1;
		double w = tp->mediabox.x1 - (double)tp->mediabox.x0, h = tp->mediabox.y1 - (double)tp->mediabox.y0;
		pdf_document *pdf = pdf_specifics(ctx, doc);
		if (pdf) cropbox_size(ctx, pdf_lookup_page_obj(ctx, pdf, pno), &w, &h);
		nx_j_raw(ctx, b, "{\"width\":");
		nx_j_double(ctx, b, w);
		nx_j_raw(ctx, b, ",\"height\":");
		nx_j_double(ctx, b, h);
		nx_j_raw(ctx, b, ",\"blocks\":[");
		as_dict(ctx, b, &first, tp->mediabox, tp->first_block, -1, &old, &cur, &tmp);
		nx_j_raw(ctx, b, "]}");
		nx_json_done(ctx, b, out);
	}
	fz_always(ctx) {
		fz_drop_stext_page(ctx, tp);
		free(old.font.s);
		free(cur.font.s);
		free(tmp.s);
	}
	fz_catch(ctx) { nx_json_fail(ctx, b, out); }
}
