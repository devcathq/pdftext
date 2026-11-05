//go:build cgo && !nomupdf

// What PyMuPDF 1.28.0 does to write PDFs (src/__init__.py, src/extra.i),
// over MuPDF's C API, for shape.go and write.go: fitz.open() and
// Document.new_page (_newPage), the page facts Shape works from, the fonts
// Page.insert_font installs (JM_insert_font, Document._get_char_widths),
// Page._set_opacity, Shape.commit (wrap_contents, _insert_contents,
// update_stream), Document.tobytes, Document.metadata and set_metadata,
// a page subset as a new PDF, Font.text_length, and Story with
// DocumentWriter.

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include <mupdf/pdf.h>

#include "nx.h"

fz_document *nx_new_pdf(fz_context *ctx) {
	pdf_document *pdf = NULL;
	fz_try(ctx) { pdf = pdf_create_document(ctx); }
	fz_catch(ctx) { pdf = NULL; }
	return (fz_document *)pdf;
}

static void need_pdf(fz_context *ctx, pdf_document *pdf) {
	if (!pdf) fz_throw(ctx, FZ_ERROR_ARGUMENT, "is no PDF");
}

static void fail(fz_context *ctx, char *err, int n) { snprintf(err, n, "%s", fz_caught_message(ctx)); }

// nx_new_page is _newPage: a page of width x height at the end, with an
// empty /Resources dict and no contents. It returns non-zero on failure.
int nx_new_page(fz_context *ctx, fz_document *doc, float w, float h, char *err, int nerr) {
	pdf_document *pdf = pdf_specifics(ctx, doc);
	pdf_obj *res = NULL, *page = NULL;
	int failed = 0;
	fz_var(res); fz_var(page);
	fz_try(ctx) {
		need_pdf(ctx, pdf);
		res = pdf_add_new_dict(ctx, pdf, 1);
		page = pdf_add_page(ctx, pdf, fz_make_rect(0, 0, w, h), 0, res, NULL);
		pdf_insert_page(ctx, pdf, -1, page);
	}
	fz_always(ctx) {
		pdf_drop_obj(ctx, page);
		pdf_drop_obj(ctx, res);
	}
	fz_catch(ctx) { failed = 1; fail(ctx, err, nerr); }
	return failed;
}

// nx_page_geom gives what Shape takes from a page: its rotation, the
// MediaBox's bottom right (mediabox_size), the CropBox's top left
// (cropbox_position) and page.transformation_matrix, all in float32.
int nx_page_geom(fz_context *ctx, fz_document *doc, int pno, float *out, char *err, int nerr) {
	pdf_document *pdf = pdf_specifics(ctx, doc);
	fz_page *page = NULL;
	int failed = 0;
	fz_var(page);
	fz_try(ctx) {
		need_pdf(ctx, pdf);
		pdf_obj *obj = pdf_lookup_page_obj(ctx, pdf, pno);
		out[0] = nx_norm_rotation(pdf_to_int(ctx, pdf_dict_get_inheritable(ctx, obj, PDF_NAME(Rotate))));
		fz_rect m = pdf_to_rect(ctx, pdf_dict_get_inheritable(ctx, obj, PDF_NAME(MediaBox)));
		if (fz_is_empty_rect(m) || fz_is_infinite_rect(m)) m = fz_make_rect(0, 0, 612, 792);
		fz_rect mb = {fz_min(m.x0, m.x1), fz_min(m.y0, m.y1), fz_max(m.x0, m.x1), fz_max(m.y0, m.y1)};
		if (mb.x1 - mb.x0 < 1 || mb.y1 - mb.y0 < 1) mb = fz_unit_rect;
		fz_rect c = pdf_to_rect(ctx, pdf_dict_get_inheritable(ctx, obj, PDF_NAME(CropBox)));
		if (fz_is_infinite_rect(c) || fz_is_empty_rect(c)) c = mb;
		out[1] = mb.x1;
		out[2] = mb.y1;
		out[3] = c.x0;
		out[4] = mb.y1 - c.y1;
		page = fz_load_page(ctx, doc, pno);
		fz_rect unit = fz_unit_rect;
		fz_matrix ctm;
		pdf_page_transform(ctx, pdf_page_from_fz_page(ctx, page), &unit, &ctm);
		out[5] = ctm.a; out[6] = ctm.b; out[7] = ctm.c; out[8] = ctm.d; out[9] = ctm.e; out[10] = ctm.f;
	}
	fz_always(ctx) { fz_drop_page(ctx, page); }
	fz_catch(ctx) { failed = 1; fail(ctx, err, nerr); }
	return failed;
}

// nx_page_font finds the font the page's resources name ref (CheckFont):
// its xref, or 0.
int nx_page_font(fz_context *ctx, fz_document *doc, int pno, const char *ref) {
	pdf_document *pdf = pdf_specifics(ctx, doc);
	int xref = 0;
	fz_try(ctx) {
		need_pdf(ctx, pdf);
		pdf_obj *page = pdf_lookup_page_obj(ctx, pdf, pno);
		pdf_obj *fonts = pdf_dict_get(ctx, pdf_dict_get_inheritable(ctx, page, PDF_NAME(Resources)), PDF_NAME(Font));
		pdf_obj *f = pdf_dict_gets(ctx, fonts, ref);
		if (f) xref = pdf_to_num(ctx, f);
	}
	fz_catch(ctx) { xref = 0; }
	return xref;
}

// nx_insert_font is Page.insert_font for a Base-14 font (bfname) or a CJK
// one (ordering >= 0, serif for the -s names): the font added to the PDF
// (JM_insert_font) and named ref in the page's /Resources/Font
// (_insertFont). It writes the font's xref, its BaseFont name, and its
// ascender and descender.
int nx_insert_font(fz_context *ctx, fz_document *doc, int pno, const char *ref, const char *bfname, int ordering,
	int serif, int *xref, char *name, int nname, float *asc, float *dsc, char *err, int nerr) {
	pdf_document *pdf = pdf_specifics(ctx, doc);
	fz_font *font = NULL;
	pdf_obj *fobj = NULL, *fonts = NULL;
	int failed = 0;
	fz_var(font); fz_var(fobj); fz_var(fonts);
	fz_try(ctx) {
		need_pdf(ctx, pdf);
		const unsigned char *data = NULL;
		int size = 0, index = 0;
		if (ordering > -1) {
			data = fz_lookup_cjk_font(ctx, ordering, &size, &index);
			if (!data) fz_throw(ctx, FZ_ERROR_ARGUMENT, "no CJK font");
			font = fz_new_font_from_memory(ctx, NULL, data, size, index, 0);
			fobj = pdf_add_cjk_font(ctx, pdf, font, ordering, 0, serif);
		} else {
			data = fz_lookup_base14_font(ctx, bfname, &size);
			if (!data) fz_throw(ctx, FZ_ERROR_ARGUMENT, "not a Base-14 font: %s", bfname);
			font = fz_new_font_from_memory(ctx, bfname, data, size, 0, 0);
			fobj = pdf_add_simple_font(ctx, pdf, font, 0);
		}
		*xref = pdf_to_num(ctx, fobj);
		snprintf(name, nname, "%s", pdf_to_name(ctx, pdf_dict_get(ctx, fobj, PDF_NAME(BaseFont))));
		*asc = fz_font_ascender(ctx, font);
		*dsc = fz_font_descender(ctx, font);
		pdf_obj *page = pdf_lookup_page_obj(ctx, pdf, pno);
		pdf_obj *res = pdf_dict_get_inheritable(ctx, page, PDF_NAME(Resources));
		if (!pdf_is_dict(ctx, res)) res = pdf_dict_put_dict(ctx, page, PDF_NAME(Resources), 5);
		pdf_obj *f = pdf_dict_get(ctx, res, PDF_NAME(Font));
		if (!f) {
			fonts = pdf_new_dict(ctx, pdf, 5);
			pdf_dict_putl(ctx, page, fonts, PDF_NAME(Resources), PDF_NAME(Font), NULL);
			f = fonts;
		}
		pdf_dict_puts_drop(ctx, f, ref, pdf_new_indirect(ctx, pdf, *xref, 0));
	}
	fz_always(ctx) {
		pdf_drop_obj(ctx, fonts);
		pdf_drop_obj(ctx, fobj);
		fz_drop_font(ctx, font);
	}
	fz_catch(ctx) { failed = 1; fail(ctx, err, nerr); }
	return failed;
}

// nx_glyph_widths is Document._get_char_widths for a Base-14 font: for
// each code below limit, its glyph and advance (0 where it has no glyph).
int nx_glyph_widths(fz_context *ctx, const char *bfname, int limit, int *glyphs, float *adv) {
	fz_font *font = NULL;
	int failed = 0;
	fz_var(font);
	fz_try(ctx) {
		int size = 0;
		const unsigned char *data = fz_lookup_base14_font(ctx, bfname, &size);
		if (!data) fz_throw(ctx, FZ_ERROR_ARGUMENT, "not a Base-14 font");
		font = fz_new_font_from_memory(ctx, bfname, data, size, 0, 0);
		for (int i = 0; i < limit; i++) {
			int g = fz_encode_character(ctx, font, i);
			float a = fz_advance_glyph(ctx, font, g, 0);
			glyphs[i] = g;
			adv[i] = g > 0 ? a : 0;
		}
	}
	fz_always(ctx) { fz_drop_font(ctx, font); }
	fz_catch(ctx) { failed = 1; }
	return failed;
}

// nx_set_opacity is Page._set_opacity's ExtGState: named gs (fitzcaNNMM)
// with /CA and /ca in the page's /Resources/ExtGState, unless there.
int nx_set_opacity(fz_context *ctx, fz_document *doc, int pno, const char *gs, float CA, float ca, char *err, int nerr) {
	pdf_document *pdf = pdf_specifics(ctx, doc);
	pdf_obj *opa = NULL;
	int failed = 0;
	fz_var(opa);
	fz_try(ctx) {
		need_pdf(ctx, pdf);
		pdf_obj *page = pdf_lookup_page_obj(ctx, pdf, pno);
		pdf_obj *res = pdf_dict_get(ctx, page, PDF_NAME(Resources));
		if (!res) res = pdf_dict_put_dict(ctx, page, PDF_NAME(Resources), 2);
		pdf_obj *extg = pdf_dict_get(ctx, res, PDF_NAME(ExtGState));
		if (!extg) extg = pdf_dict_put_dict(ctx, res, PDF_NAME(ExtGState), 2);
		int found = 0, n = pdf_dict_len(ctx, extg);
		for (int i = 0; i < n && !found; i++)
			found = !strcmp(pdf_to_name(ctx, pdf_dict_get_key(ctx, extg, i)), gs);
		if (!found) {
			opa = pdf_new_dict(ctx, pdf, 3);
			pdf_dict_put_real(ctx, opa, PDF_NAME(CA), CA);
			pdf_dict_put_real(ctx, opa, PDF_NAME(ca), ca);
			pdf_dict_puts(ctx, extg, gs, opa);
		}
	}
	fz_always(ctx) { pdf_drop_obj(ctx, opa); }
	fz_catch(ctx) { failed = 1; fail(ctx, err, nerr); }
	return failed;
}

// add_contents is JM_insert_contents: data as a new stream, appended to the
// page's /Contents (overlay) or put first. It returns the stream's xref.
static int add_contents(fz_context *ctx, pdf_document *pdf, pdf_obj *page, fz_buffer *buf, int overlay) {
	pdf_obj *ref = NULL, *arr = NULL;
	int xref = 0;
	fz_var(ref); fz_var(arr);
	fz_try(ctx) {
		pdf_obj *contents = pdf_dict_get(ctx, page, PDF_NAME(Contents));
		ref = pdf_add_stream(ctx, pdf, buf, NULL, 0);
		xref = pdf_to_num(ctx, ref);
		if (pdf_is_array(ctx, contents)) {
			if (overlay) pdf_array_push(ctx, contents, ref);
			else pdf_array_insert(ctx, contents, ref, 0);
		} else {
			arr = pdf_new_array(ctx, pdf, 5);
			if (overlay) {
				if (contents) pdf_array_push(ctx, arr, contents);
				pdf_array_push(ctx, arr, ref);
			} else {
				pdf_array_push(ctx, arr, ref);
				if (contents) pdf_array_push(ctx, arr, contents);
			}
			pdf_dict_put(ctx, page, PDF_NAME(Contents), arr);
		}
	}
	fz_always(ctx) {
		pdf_drop_obj(ctx, arr);
		pdf_drop_obj(ctx, ref);
	}
	fz_catch(ctx) { fz_rethrow(ctx); }
	return xref;
}

static void add_text_contents(fz_context *ctx, pdf_document *pdf, pdf_obj *page, const char *s, int overlay) {
	fz_buffer *b = fz_new_buffer_from_copied_data(ctx, (const unsigned char *)s, strlen(s));
	fz_try(ctx) { add_contents(ctx, pdf, page, b, overlay); }
	fz_always(ctx) { fz_drop_buffer(ctx, b); }
	fz_catch(ctx) { fz_rethrow(ctx); }
}

// nx_commit is Shape.commit: with overlay, the page's graphics state
// balanced first (wrap_contents); then the content as a new stream, flate
// compressed when that makes it smaller (update_stream).
int nx_commit(fz_context *ctx, fz_document *doc, int pno, const char *data, size_t n, int overlay, char *err, int nerr) {
	pdf_document *pdf = pdf_specifics(ctx, doc);
	fz_buffer *buf = NULL, *z = NULL, *blank = NULL;
	pdf_obj *ref = NULL;
	int failed = 0;
	fz_var(buf); fz_var(z); fz_var(blank); fz_var(ref);
	fz_try(ctx) {
		need_pdf(ctx, pdf);
		pdf_obj *page = pdf_lookup_page_obj(ctx, pdf, pno);
		if (overlay) {
			int push = 0, pop = 0;
			pdf_count_q_balance(ctx, pdf, pdf_dict_get(ctx, page, PDF_NAME(Resources)),
				pdf_dict_get(ctx, page, PDF_NAME(Contents)), &push, &pop);
			if (push > 0) {
				char *s = malloc(2 * push + 1);
				for (int i = 0; i < push; i++) memcpy(s + 2 * i, "q\n", 2);
				s[2 * push] = 0;
				fz_try(ctx) { add_text_contents(ctx, pdf, page, s, 0); }
				fz_always(ctx) { free(s); }
				fz_catch(ctx) { fz_rethrow(ctx); }
			}
			if (pop > 0) {
				char *s = malloc(2 * pop + 2);
				for (int i = 0; i < pop; i++) memcpy(s + 2 * i, "\nQ", 2);
				s[2 * pop] = '\n';
				s[2 * pop + 1] = 0;
				fz_try(ctx) { add_text_contents(ctx, pdf, page, s, 1); }
				fz_always(ctx) { free(s); }
				fz_catch(ctx) { fz_rethrow(ctx); }
			}
		}
		blank = fz_new_buffer_from_copied_data(ctx, (const unsigned char *)" ", 1);
		int xref = add_contents(ctx, pdf, page, blank, overlay);
		ref = pdf_new_indirect(ctx, pdf, xref, 0);
		buf = fz_new_buffer_from_copied_data(ctx, (const unsigned char *)data, n);
		if (n > 30) {
			size_t zn = 0;
			unsigned char *zd = fz_new_deflated_data_from_buffer(ctx, &zn, buf, FZ_DEFLATE_BEST);
			if (zd && zn) z = fz_new_buffer_from_data(ctx, zd, zn);
			else fz_free(ctx, zd);
			if (z && zn < n) {
				pdf_dict_put(ctx, ref, PDF_NAME(Filter), PDF_NAME(FlateDecode));
				pdf_update_stream(ctx, pdf, ref, z, 1);
				break;
			}
		}
		pdf_update_stream(ctx, pdf, ref, buf, 0);
	}
	fz_always(ctx) {
		pdf_drop_obj(ctx, ref);
		fz_drop_buffer(ctx, z);
		fz_drop_buffer(ctx, buf);
		fz_drop_buffer(ctx, blank);
	}
	fz_catch(ctx) { failed = 1; fail(ctx, err, nerr); }
	return failed;
}

// write_pdf is Document.tobytes() with its defaults: no garbage collection,
// cleaning or compression, encryption kept, metadata kept, a document /ID
// added when there is none (JM_ensure_identity).
static void write_pdf(fz_context *ctx, pdf_document *pdf, fz_buffer *b) {
	fz_output *o = NULL;
	fz_var(o);
	fz_try(ctx) {
		pdf_obj *id = pdf_dict_get(ctx, pdf_trailer(ctx, pdf), PDF_NAME(ID));
		if (!id) {
			unsigned char rnd[16];
			fz_memrnd(ctx, rnd, 16);
			id = pdf_dict_put_array(ctx, pdf_trailer(ctx, pdf), PDF_NAME(ID), 2);
			pdf_array_push_string(ctx, id, (char *)rnd, 16);
			pdf_array_push_string(ctx, id, (char *)rnd, 16);
		}
		pdf_write_options opts = pdf_default_write_options;
		opts.do_encrypt = PDF_ENCRYPT_NONE; // PyMuPDF's encryption=1
		opts.permissions = 4095;
		opts.do_preserve_metadata = 1;
		o = fz_new_output_with_buffer(ctx, b);
		pdf_write_document(ctx, pdf, o, &opts);
		fz_close_output(ctx, o);
	}
	fz_always(ctx) { fz_drop_output(ctx, o); }
	fz_catch(ctx) { fz_rethrow(ctx); }
}

void nx_tobytes(fz_context *ctx, fz_document *doc, nx_json *out) {
	fz_buffer *b = NULL;
	fz_var(b);
	fz_try(ctx) {
		pdf_document *pdf = pdf_specifics(ctx, doc);
		need_pdf(ctx, pdf);
		b = fz_new_buffer(ctx, 1 << 16);
		write_pdf(ctx, pdf, b);
		nx_json_done(ctx, b, out);
	}
	fz_catch(ctx) { nx_json_fail(ctx, b, out); }
}

// nx_extract_pages writes a new PDF of the given pages, in that order, each
// grafted with what it uses, and the document's /Info.
void nx_extract_pages(fz_context *ctx, fz_document *doc, const int *pages, int n, nx_json *out) {
	fz_buffer *b = NULL;
	pdf_document *dst = NULL;
	pdf_graft_map *map = NULL;
	pdf_obj *info = NULL;
	fz_var(b); fz_var(dst); fz_var(map); fz_var(info);
	fz_try(ctx) {
		pdf_document *src = pdf_specifics(ctx, doc);
		need_pdf(ctx, src);
		dst = pdf_create_document(ctx);
		map = pdf_new_graft_map(ctx, dst);
		for (int i = 0; i < n; i++) pdf_graft_mapped_page(ctx, map, -1, src, pages[i]);
		pdf_obj *si = pdf_dict_get(ctx, pdf_trailer(ctx, src), PDF_NAME(Info));
		if (si) {
			info = pdf_graft_mapped_object(ctx, map, si);
			pdf_dict_put(ctx, pdf_trailer(ctx, dst), PDF_NAME(Info), info);
		}
		b = fz_new_buffer(ctx, 1 << 16);
		write_pdf(ctx, dst, b);
		nx_json_done(ctx, b, out);
	}
	fz_always(ctx) {
		pdf_drop_obj(ctx, info);
		pdf_drop_graft_map(ctx, map);
		pdf_drop_document(ctx, dst);
	}
	fz_catch(ctx) {
		if (b == out->buf) b = NULL;
		nx_json_fail(ctx, b, out);
	}
}

// nx_metadata is Document.metadata: fz_lookup_metadata for each key, ""
// where there is none; encryption null where it is "None".
void nx_metadata(fz_context *ctx, fz_document *doc, nx_json *out) {
	static const char *keys[][2] = {
		{"format", "format"}, {"title", "info:Title"}, {"author", "info:Author"}, {"subject", "info:Subject"},
		{"keywords", "info:Keywords"}, {"creator", "info:Creator"}, {"producer", "info:Producer"},
		{"creationDate", "info:CreationDate"}, {"modDate", "info:ModDate"}, {"trapped", "info:Trapped"},
		{"encryption", "encryption"},
	};
	fz_buffer *b = NULL;
	char *val = NULL;
	fz_var(b); fz_var(val);
	fz_try(ctx) {
		b = fz_new_buffer(ctx, 512);
		nx_j_raw(ctx, b, "{");
		for (size_t i = 0; i < sizeof keys / sizeof keys[0]; i++) {
			int n = fz_lookup_metadata(ctx, doc, keys[i][1], NULL, 0);
			free(val);
			val = malloc(n > 0 ? n + 1 : 1);
			val[0] = 0;
			if (n > 0) fz_lookup_metadata(ctx, doc, keys[i][1], val, n + 1);
			if (i) nx_j_raw(ctx, b, ",");
			nx_j_str(ctx, b, keys[i][0], strlen(keys[i][0]));
			nx_j_raw(ctx, b, ":");
			if (!strcmp(keys[i][0], "encryption") && (n < 0 || !strcmp(val, "None"))) nx_j_raw(ctx, b, "null");
			else nx_j_str(ctx, b, val, strlen(val));
		}
		nx_j_raw(ctx, b, "}");
		nx_json_done(ctx, b, out);
	}
	fz_always(ctx) { free(val); }
	fz_catch(ctx) { nx_json_fail(ctx, b, out); }
}

// nx_set_info is one key of set_metadata: the /Info entry key (Title,
// Author, …) set to the text value, or removed for NULL. A document
// without /Info gets one.
int nx_set_info(fz_context *ctx, fz_document *doc, const char *key, const char *value, char *err, int nerr) {
	pdf_document *pdf = pdf_specifics(ctx, doc);
	pdf_obj *info = NULL;
	int failed = 0;
	fz_var(info);
	fz_try(ctx) {
		need_pdf(ctx, pdf);
		pdf_obj *t = pdf_trailer(ctx, pdf), *i = pdf_dict_get(ctx, t, PDF_NAME(Info));
		if (!pdf_is_indirect(ctx, i)) {
			info = pdf_add_new_dict(ctx, pdf, 8);
			pdf_dict_put(ctx, t, PDF_NAME(Info), info);
			i = info;
		}
		if (value) pdf_dict_puts_drop(ctx, i, key, pdf_new_text_string(ctx, value));
		else pdf_dict_dels(ctx, i, key);
	}
	fz_always(ctx) { pdf_drop_obj(ctx, info); }
	fz_catch(ctx) { failed = 1; fail(ctx, err, nerr); }
	return failed;
}

// nx_text_length is Font(name).text_length(text, fontsize): the sum of
// each character's advance in the font or the fallback MuPDF picks for it.
// name is a Base-14 name, or ordering >= 0 a CJK font. -1 on failure.
double nx_text_length(fz_context *ctx, const char *name, int ordering, const int *codes, int n, double size) {
	fz_font *font = NULL;
	double len = -1;
	fz_var(font);
	fz_try(ctx) {
		font = ordering > -1 ? fz_new_cjk_font(ctx, ordering) : fz_new_base14_font(ctx, name);
		double rc = 0;
		for (int i = 0; i < n; i++) {
			fz_font *f = NULL;
			int gid = fz_encode_character_with_fallback(ctx, font, codes[i], 0, FZ_LANG_UNSET, &f);
			rc += fz_advance_glyph(ctx, f, gid, 0);
		}
		len = rc * size;
	}
	fz_always(ctx) { fz_drop_font(ctx, font); }
	fz_catch(ctx) { len = -1; }
	return len;
}

// A DocumentWriter writing PDF into a buffer, and the device of the page it
// is on.
// The writer owns its output (writer.h), so only the buffer is ours.
struct nx_writer {
	fz_buffer *buf;
	fz_document_writer *w;
	fz_device *dev;
};

nx_writer *nx_new_writer(fz_context *ctx, char *err, int nerr) {
	nx_writer *w = calloc(1, sizeof *w);
	fz_try(ctx) {
		w->buf = fz_new_buffer(ctx, 1 << 16);
		w->w = fz_new_pdf_writer_with_output(ctx, fz_new_output_with_buffer(ctx, w->buf), "");
	}
	fz_catch(ctx) {
		fail(ctx, err, nerr);
		fz_drop_buffer(ctx, w->buf);
		free(w);
		w = NULL;
	}
	return w;
}

int nx_writer_begin(fz_context *ctx, nx_writer *w, float x0, float y0, float x1, float y1, char *err, int nerr) {
	int failed = 0;
	fz_try(ctx) { w->dev = fz_begin_page(ctx, w->w, fz_make_rect(x0, y0, x1, y1)); }
	fz_catch(ctx) { failed = 1; fail(ctx, err, nerr); }
	return failed;
}

int nx_writer_end(fz_context *ctx, nx_writer *w, char *err, int nerr) {
	int failed = 0;
	fz_try(ctx) {
		fz_end_page(ctx, w->w);
		w->dev = NULL;
	}
	fz_catch(ctx) { failed = 1; fail(ctx, err, nerr); }
	return failed;
}

// nx_writer_close finishes the PDF; its bytes are then w->buf's.
int nx_writer_close(fz_context *ctx, nx_writer *w, unsigned char **data, size_t *n, char *err, int nerr) {
	int failed = 0;
	fz_try(ctx) {
		fz_close_document_writer(ctx, w->w);
		*n = fz_buffer_storage(ctx, w->buf, data);
	}
	fz_catch(ctx) { failed = 1; fail(ctx, err, nerr); }
	return failed;
}

void nx_drop_writer(fz_context *ctx, nx_writer *w) {
	if (!w) return;
	fz_drop_document_writer(ctx, w->w);
	fz_drop_buffer(ctx, w->buf);
	free(w);
}

// A Story: HTML and CSS laid out by MuPDF, placed rectangle by rectangle.
fz_story *nx_new_story(fz_context *ctx, const char *html, const char *css, char *err, int nerr) {
	fz_story *s = NULL;
	fz_buffer *b = NULL;
	fz_var(b);
	fz_try(ctx) {
		b = fz_new_buffer_from_copied_data(ctx, (const unsigned char *)html, strlen(html));
		s = fz_new_story(ctx, b, css, 12, NULL);
	}
	fz_always(ctx) { fz_drop_buffer(ctx, b); }
	fz_catch(ctx) { s = NULL; fail(ctx, err, nerr); }
	return s;
}

// nx_story_place is Story.place(where): -1 on failure, else whether more
// is left to place; filled is the part used.
int nx_story_place(fz_context *ctx, fz_story *s, float *where, float *filled, char *err, int nerr) {
	int more = -1;
	fz_try(ctx) {
		fz_rect f = fz_empty_rect;
		more = fz_place_story_flags(ctx, s, fz_make_rect(where[0], where[1], where[2], where[3]), &f, 0);
		filled[0] = f.x0; filled[1] = f.y0; filled[2] = f.x1; filled[3] = f.y1;
	}
	fz_catch(ctx) { more = -1; fail(ctx, err, nerr); }
	return more;
}

int nx_story_draw(fz_context *ctx, fz_story *s, nx_writer *w, char *err, int nerr) {
	int failed = 0;
	fz_try(ctx) { fz_draw_story(ctx, s, w ? w->dev : NULL, fz_identity); }
	fz_catch(ctx) { failed = 1; fail(ctx, err, nerr); }
	return failed;
}

void nx_drop_story(fz_context *ctx, fz_story *s) { fz_drop_story(ctx, s); }
