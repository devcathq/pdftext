//go:build cgo && !nomupdf

// What PyMuPDF 1.28.0 does to read a page's text (src/extra.i), written over
// MuPDF's C API: page_get_textpage with the "text"/"words" flags and the page
// derotated, JM_char_quad / JM_char_bbox, the word split of extractWORDS, the
// plain text of extractText, and the image scan behind Page.get_images.
// PyMuPDF's own small corrections (asc/dsc of thin fonts, zero-width glyphs)
// are kept, because they change the boxes the sort runs on.

#include <float.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

#include <mupdf/pdf.h>

#include "nx.h"

#define TRACER_MAX 4096

static void grow(char **buf, int *n, int *cap, int add) {
	if (*n + add <= *cap) return;
	int c = *cap ? *cap * 2 : 4096;
	while (c < *n + add) c *= 2;
	*buf = realloc(*buf, c);
	*cap = c;
}

static void put_utf8(char **buf, int *n, int *cap, int c) {
	grow(buf, n, cap, 4);
	unsigned char *o = (unsigned char *)*buf + *n;
	if (c < 0x80) { o[0] = c; *n += 1; return; }
	if (c < 0x800) { o[0] = 0xc0 | (c >> 6); o[1] = 0x80 | (c & 0x3f); *n += 2; return; }
	if (c < 0x10000) {
		o[0] = 0xe0 | (c >> 12); o[1] = 0x80 | ((c >> 6) & 0x3f); o[2] = 0x80 | (c & 0x3f);
		*n += 3; return;
	}
	o[0] = 0xf0 | (c >> 18); o[1] = 0x80 | ((c >> 12) & 0x3f);
	o[2] = 0x80 | ((c >> 6) & 0x3f); o[3] = 0x80 | (c & 0x3f);
	*n += 4;
}

// put_rune is JM_append_rune read back by JM_EscapeStrFromBuffer
// (raw-unicode-escape, "replace"): a lone surrogate or a value past U+10FFFF
// becomes U+FFFD; a negative value prints as "￿ffff", which reads back
// as U+FFFF and the last four hex digits as text.
static void put_rune(char **buf, int *n, int *cap, int c) {
	if (c < 0) {
		static const char hex[] = "0123456789abcdef";
		unsigned int u = (unsigned int)c;
		put_utf8(buf, n, cap, (int)(u >> 16));
		for (int s = 12; s >= 0; s -= 4) put_utf8(buf, n, cap, hex[(u >> s) & 0xf]);
		return;
	}
	if ((c >= 0xd800 && c <= 0xdfff) || c > 0x10ffff) c = 0xfffd;
	put_utf8(buf, n, cap, c);
}

// char_quad is JM_char_quad; small is TOOLS.set_small_glyph_heights(True),
// which find_tables turns on: every glyph's box then comes from its font's
// ascender and descender.
static fz_quad char_quad(fz_context *ctx, fz_stext_line *line, fz_stext_char *ch, int small) {
	if (line->wmode) return ch->quad;
	fz_font *font = ch->font;
	float asc = fz_font_ascender(ctx, font), dsc = fz_font_descender(ctx, font);
	float fsize = ch->size, asc_dsc = asc - dsc + FLT_EPSILON;
	if (asc_dsc >= 1 && !small) return ch->quad;
	if (asc < 1e-3) { dsc = -0.1f; asc = 0.9f; asc_dsc = 1.0f; }
	if (small || asc_dsc < 1) { dsc = dsc / asc_dsc; asc = asc / asc_dsc; }
	asc_dsc = asc - dsc;
	asc = asc * fsize / asc_dsc;
	dsc = dsc * fsize / asc_dsc;
	float c = line->dir.x, s = line->dir.y;
	fz_matrix trm1 = fz_make_matrix(c, -s, s, c, 0, 0), trm2 = fz_make_matrix(c, s, -s, c, 0, 0);
	if (c == -1) { trm1.d = 1; trm2.d = 1; }
	fz_matrix xlate1 = fz_make_matrix(1, 0, 0, 1, -ch->origin.x, -ch->origin.y);
	fz_matrix xlate2 = fz_make_matrix(1, 0, 0, 1, ch->origin.x, ch->origin.y);
	fz_quad q = fz_transform_quad(fz_transform_quad(ch->quad, xlate1), trm1);
	if (c == 1 && q.ul.y > 0) {
		q.ul.y = asc; q.ur.y = asc; q.ll.y = dsc; q.lr.y = dsc;
	} else {
		q.ul.y = -asc; q.ur.y = -asc; q.ll.y = -dsc; q.lr.y = -dsc;
	}
	if (q.ll.x < 0) { q.ll.x = 0; q.ul.x = 0; }
	if (q.lr.x - q.ll.x < FLT_EPSILON) {
		int glyph = fz_encode_character(ctx, font, ch->c);
		if (glyph) {
			q.lr.x = q.ll.x + fz_advance_glyph(ctx, font, glyph, line->wmode) * fsize;
			q.ur.x = q.lr.x;
		}
	}
	return fz_transform_quad(fz_transform_quad(q, trm2), xlate2);
}

static fz_rect char_bbox_of(fz_context *ctx, fz_stext_line *line, fz_stext_char *ch, int small) {
	fz_rect r = fz_rect_from_quad(char_quad(ctx, line, ch, small));
	if (line->wmode && r.y1 < r.y0 + ch->size) r.y0 = r.y1 - ch->size;
	return r;
}

static fz_rect char_bbox(fz_context *ctx, fz_stext_line *line, fz_stext_char *ch) {
	return char_bbox_of(ctx, line, ch, 0);
}

static int overlap(fz_rect a, fz_rect b) {
	return !(a.x0 >= b.x1 || a.y0 >= b.y1 || a.x1 <= b.x0 || a.y1 <= b.y0);
}

static int in_page(fz_context *ctx, fz_rect page, fz_stext_line *line, fz_stext_char *ch) {
	return fz_is_infinite_rect(page) || overlap(page, char_bbox(ctx, line, ch));
}

fz_rect nx_char_bbox(fz_context *ctx, fz_stext_line *line, fz_stext_char *ch) {
	return char_bbox_of(ctx, line, ch, 0);
}

fz_rect nx_char_bbox_small(fz_context *ctx, fz_stext_line *line, fz_stext_char *ch, int small) {
	return char_bbox_of(ctx, line, ch, small);
}

int nx_overlap(fz_rect a, fz_rect b) { return overlap(a, b); }

void nx_put_rune(char **buf, int *n, int *cap, int c) { put_rune(buf, n, cap, c); }

static int delimiter(int c) { return c <= 32 || c == 160 || (c >= 0x202a && c <= 0x202e); }
static int rtl(int c) { return c >= 0x590 && c <= 0x900; }

static void add_word(nx_page *o, fz_rect *box, int start) {
	if (o->nwords == o->capw) {
		o->capw = o->capw ? o->capw * 2 : 256;
		o->words = realloc(o->words, o->capw * sizeof(nx_word));
	}
	o->words[o->nwords++] = (nx_word){box->x0, box->y0, box->x1, box->y1, start, o->ntext - start};
	*box = fz_empty_rect;
}

// line_words is one line of _as_words. A word closes at a delimiter or where
// the writing direction changes; a word whose box is empty is dropped.
static void line_words(fz_context *ctx, fz_rect page, fz_stext_line *line, fz_rect *box, nx_page *o) {
	int start = o->ntext, last_rtl = 0;
	for (fz_stext_char *ch = line->first_char; ch; ch = ch->next) {
		if (!in_page(ctx, page, line, ch)) continue;
		if (o->ntext == start && ch->c == 0x200d) continue;
		int d = delimiter(ch->c), r = rtl(ch->c);
		if (d || r != last_rtl) {
			if (o->ntext == start && d) continue;
			if (!fz_is_empty_rect(*box)) add_word(o, box, start);
			else o->ntext = start;
			start = o->ntext;
			if (d) continue;
		}
		put_rune(&o->text, &o->ntext, &o->capt, ch->c);
		last_rtl = r;
		*box = fz_union_rect(*box, char_bbox(ctx, line, ch));
	}
	if (o->ntext > start && !fz_is_empty_rect(*box)) add_word(o, box, start);
	else o->ntext = start;
}

static void page_words(fz_context *ctx, fz_rect page, fz_stext_block *b, nx_page *o) {
	for (; b; b = b->next) {
		if (b->type == FZ_STEXT_BLOCK_STRUCT && b->u.s.down)
			page_words(ctx, page, b->u.s.down->first_block, o);
		if (b->type != FZ_STEXT_BLOCK_TEXT) continue;
		fz_rect box = fz_empty_rect;
		for (fz_stext_line *line = b->u.t.first_line; line; line = line->next)
			line_words(ctx, page, line, &box, o);
	}
}

// page_plain is _as_text: each line ends with one newline, unless it already
// does or wrote nothing.
static void page_plain(fz_context *ctx, fz_rect page, fz_stext_block *b, nx_page *o) {
	for (; b; b = b->next) {
		if (b->type == FZ_STEXT_BLOCK_STRUCT && b->u.s.down)
			page_plain(ctx, page, b->u.s.down->first_block, o);
		if (b->type != FZ_STEXT_BLOCK_TEXT) continue;
		int last = 0;
		for (fz_stext_line *line = b->u.t.first_line; line; line = line->next) {
			for (fz_stext_char *ch = line->first_char; ch; ch = ch->next) {
				if (!in_page(ctx, page, line, ch)) continue;
				last = ch->c;
				put_rune(&o->plain, &o->nplain, &o->capp, last);
			}
			if (last != 10 && last > 0) { put_rune(&o->plain, &o->nplain, &o->capp, 10); last = 10; }
		}
		if (last != 10 && last > 0) put_rune(&o->plain, &o->nplain, &o->capp, 10);
	}
}

// scan_images is JM_scan_resources for images: the XObjects of these
// resources, then those of each Form XObject's resources. Like Python, a
// Form XObject already visited ends the scan of the resources that list it
// (only that level).
static void scan_images(fz_context *ctx, pdf_obj *rsrc, int *found, int *tracer, int *ntr) {
	if (pdf_mark_obj(ctx, rsrc)) return;
	fz_try(ctx) {
		pdf_obj *xobj = pdf_dict_get(ctx, rsrc, PDF_NAME(XObject));
		int n = pdf_dict_len(ctx, xobj);
		for (int i = 0; i < n; i++) {
			pdf_obj *d = pdf_dict_get_val(ctx, xobj, i);
			if (pdf_is_dict(ctx, d) && pdf_name_eq(ctx, pdf_dict_get(ctx, d, PDF_NAME(Subtype)), PDF_NAME(Image)))
				(*found)++;
		}
		for (int i = 0; i < n; i++) {
			pdf_obj *obj = pdf_dict_get_val(ctx, xobj, i);
			pdf_obj *sub = pdf_dict_get(ctx, obj, PDF_NAME(Resources));
			if (!sub) continue;
			int sxref = pdf_is_stream(ctx, obj) ? pdf_to_num(ctx, obj) : 0, seen = 0;
			for (int k = 0; k < *ntr; k++) seen |= tracer[k] == sxref;
			if (seen || *ntr == TRACER_MAX) break;
			tracer[(*ntr)++] = sxref;
			scan_images(ctx, sub, found, tracer, ntr);
		}
	}
	fz_always(ctx) { pdf_unmark_obj(ctx, rsrc); }
	fz_catch(ctx) {}
}

static int has_image(fz_context *ctx, pdf_document *pdf, int pno) {
	int found = 0, ntr = 0;
	int *tracer = malloc(TRACER_MAX * sizeof(int));
	fz_try(ctx) {
		pdf_obj *rsrc = pdf_dict_get_inheritable(ctx, pdf_lookup_page_obj(ctx, pdf, pno), PDF_NAME(Resources));
		if (rsrc) scan_images(ctx, rsrc, &found, tracer, &ntr);
	}
	fz_catch(ctx) { found = 0; }
	free(tracer);
	return found > 0;
}

static void silent(void *user, const char *message) {}

fz_context *nx_new_context(void) {
	fz_context *ctx = fz_new_context(NULL, NULL, FZ_STORE_DEFAULT);
	if (!ctx) return NULL;
	fz_set_warning_callback(ctx, silent, NULL);
	fz_set_error_callback(ctx, silent, NULL);
	fz_register_document_handlers(ctx);
	return ctx;
}

fz_document *nx_open(fz_context *ctx, unsigned char *data, size_t len) {
	fz_document *doc = NULL;
	fz_stream *stm = NULL;
	fz_var(doc);
	fz_var(stm);
	fz_try(ctx) {
		stm = fz_open_memory(ctx, data, len);
		doc = fz_open_document_with_stream(ctx, "pdf", stm);
	}
	fz_always(ctx) { fz_drop_stream(ctx, stm); }
	fz_catch(ctx) { doc = NULL; }
	return doc;
}

int nx_count_pages(fz_context *ctx, fz_document *doc) {
	int n = -1;
	fz_try(ctx) { n = fz_count_pages(ctx, doc); }
	fz_catch(ctx) { n = -1; }
	return n;
}

// nx_textpage is Page.get_textpage(flags=flags): the page's structured
// text, read with its /Rotate set to 0 and put back after, as PyMuPDF does.
// It throws on failure.
fz_stext_page *nx_textpage(fz_context *ctx, fz_document *doc, int pno, int flags) {
	fz_page *page = NULL;
	fz_stext_page *tp = NULL;
	fz_device *dev = NULL;
	pdf_obj *pobj = NULL;
	int old = 0;
	fz_var(page); fz_var(tp); fz_var(dev); fz_var(pobj); fz_var(old);
	pdf_document *pdf = pdf_specifics(ctx, doc);
	fz_try(ctx) {
		page = fz_load_page(ctx, doc, pno);
		if (pdf) {
			pobj = pdf_lookup_page_obj(ctx, pdf, pno);
			old = nx_norm_rotation(pdf_to_int(ctx, pdf_dict_get_inheritable(ctx, pobj, PDF_NAME(Rotate))));
			if (old) pdf_dict_put_int(ctx, pobj, PDF_NAME(Rotate), 0);
		}
		fz_stext_options opts;
		memset(&opts, 0, sizeof opts);
		opts.flags = flags;
		tp = fz_new_stext_page(ctx, fz_bound_page(ctx, page));
		dev = fz_new_stext_device(ctx, tp, &opts);
		fz_run_page(ctx, page, dev, fz_identity, NULL);
		fz_close_device(ctx, dev);
	}
	fz_always(ctx) {
		if (pobj && old) pdf_dict_put_int(ctx, pobj, PDF_NAME(Rotate), old);
		fz_drop_device(ctx, dev);
		fz_drop_page(ctx, page);
	}
	fz_catch(ctx) {
		fz_drop_stext_page(ctx, tp);
		fz_rethrow(ctx);
	}
	return tp;
}

// nx_read_page is Page.get_textpage(flags=TEXTFLAGS_TEXT) read as words and
// as plain text, and the page's image scan.
void nx_read_page(fz_context *ctx, fz_document *doc, int pno, nx_page *out) {
	fz_stext_page *tp = NULL;
	fz_var(tp);
	pdf_document *pdf = pdf_specifics(ctx, doc);
	fz_try(ctx) {
		tp = nx_textpage(ctx, doc, pno, FZ_STEXT_PRESERVE_LIGATURES | FZ_STEXT_PRESERVE_WHITESPACE |
			FZ_STEXT_MEDIABOX_CLIP | FZ_STEXT_USE_CID_FOR_UNKNOWN_UNICODE);
		page_words(ctx, tp->mediabox, tp->first_block, out);
		page_plain(ctx, tp->mediabox, tp->first_block, out);
		if (pdf) out->has_image = has_image(ctx, pdf, pno);
	}
	fz_always(ctx) { fz_drop_stext_page(ctx, tp); }
	fz_catch(ctx) { out->failed = 1; }
}

void nx_free_page(nx_page *p) {
	free(p->words);
	free(p->text);
	free(p->plain);
}
