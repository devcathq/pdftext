//go:build cgo && !nomupdf

// What PyMuPDF 1.28.0 does to list and pull out a page's images
// (src/__init__.py): page.get_images(full=True) (JM_scan_resources,
// JM_gather_images), Pixmap(doc, xref) with Pixmap(csRGB, pix) and
// tobytes("png"), page.get_image_rects(xref) (image digests matched against
// get_image_info(hashes=True)), and the xref table (doc.xref_length(),
// doc.xref_object(xref)).

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include <mupdf/pdf.h>

#include "nx.h"

#define TRACER_MAX 4096

typedef struct { char *s; int n, cap; } text;

static void put_name(fz_context *ctx, fz_buffer *b, pdf_obj *o, text *t) {
	const char *name = pdf_to_name(ctx, o);
	t->n = 0;
	nx_put_raw_escape(&t->s, &t->n, &t->cap, name, strlen(name));
	nx_j_str(ctx, b, t->s ? t->s : "", t->n);
}

// gather is JM_gather_images: one entry per image XObject of these
// resources: (xref, smask, width, height, bpc, colorspace, alt. colorspace,
// name, filter, referencer).
static void gather(fz_context *ctx, fz_buffer *b, int *first, pdf_obj *xobj, int sxref, text *t) {
	int n = pdf_dict_len(ctx, xobj);
	for (int i = 0; i < n; i++) {
		pdf_obj *name = pdf_dict_get_key(ctx, xobj, i), *d = pdf_dict_get_val(ctx, xobj, i);
		if (!pdf_is_dict(ctx, d)) continue;
		if (!pdf_name_eq(ctx, pdf_dict_get(ctx, d, PDF_NAME(Subtype)), PDF_NAME(Image))) continue;
		pdf_obj *smask = pdf_dict_geta(ctx, d, PDF_NAME(SMask), PDF_NAME(Mask));
		pdf_obj *filter = pdf_dict_geta(ctx, d, PDF_NAME(Filter), PDF_NAME(F));
		if (pdf_is_array(ctx, filter)) filter = pdf_array_get(ctx, filter, 0);
		pdf_obj *alt = NULL, *cs = pdf_dict_geta(ctx, d, PDF_NAME(ColorSpace), PDF_NAME(CS));
		if (pdf_is_array(ctx, cs)) {
			pdf_obj *cses = cs;
			cs = pdf_array_get(ctx, cses, 0);
			if (pdf_name_eq(ctx, cs, PDF_NAME(DeviceN)) || pdf_name_eq(ctx, cs, PDF_NAME(Separation))) {
				alt = pdf_array_get(ctx, cses, 2);
				if (pdf_is_array(ctx, alt)) alt = pdf_array_get(ctx, alt, 0);
			}
		}
		nx_j_raw(ctx, b, *first ? "[" : ",[");
		*first = 0;
		nx_j_int(ctx, b, pdf_to_num(ctx, d)); nx_j_raw(ctx, b, ",");
		nx_j_int(ctx, b, smask ? pdf_to_num(ctx, smask) : 0); nx_j_raw(ctx, b, ",");
		nx_j_int(ctx, b, pdf_to_int(ctx, pdf_dict_geta(ctx, d, PDF_NAME(Width), PDF_NAME(W)))); nx_j_raw(ctx, b, ",");
		nx_j_int(ctx, b, pdf_to_int(ctx, pdf_dict_geta(ctx, d, PDF_NAME(Height), PDF_NAME(H)))); nx_j_raw(ctx, b, ",");
		nx_j_int(ctx, b, pdf_to_int(ctx, pdf_dict_geta(ctx, d, PDF_NAME(BitsPerComponent), PDF_NAME(BPC)))); nx_j_raw(ctx, b, ",");
		put_name(ctx, b, cs, t); nx_j_raw(ctx, b, ",");
		put_name(ctx, b, alt, t); nx_j_raw(ctx, b, ",");
		put_name(ctx, b, name, t); nx_j_raw(ctx, b, ",");
		put_name(ctx, b, filter, t); nx_j_raw(ctx, b, ",");
		nx_j_int(ctx, b, sxref);
		nx_j_raw(ctx, b, "]");
	}
}

// scan is JM_scan_resources for images: these resources' images, then
// those of each Form XObject's resources. A Form XObject already visited
// ends the scan of the resources that list it.
static void scan(fz_context *ctx, fz_buffer *b, int *first, pdf_obj *rsrc, int sxref, int *tracer, int *ntr, text *t) {
	if (pdf_mark_obj(ctx, rsrc)) return;
	fz_try(ctx) {
		pdf_obj *xobj = pdf_dict_get(ctx, rsrc, PDF_NAME(XObject));
		gather(ctx, b, first, xobj, sxref, t);
		int n = pdf_dict_len(ctx, xobj);
		for (int i = 0; i < n; i++) {
			pdf_obj *obj = pdf_dict_get_val(ctx, xobj, i);
			int x = pdf_is_stream(ctx, obj) ? pdf_to_num(ctx, obj) : 0, seen = 0;
			pdf_obj *sub = pdf_dict_get(ctx, obj, PDF_NAME(Resources));
			if (!sub) continue;
			for (int k = 0; k < *ntr; k++) seen |= tracer[k] == x;
			if (seen || *ntr == TRACER_MAX) break;
			tracer[(*ntr)++] = x;
			scan(ctx, b, first, sub, x, tracer, ntr, t);
		}
	}
	fz_always(ctx) { pdf_unmark_obj(ctx, rsrc); }
	fz_catch(ctx) { fz_rethrow(ctx); }
}

void nx_images(fz_context *ctx, fz_document *doc, int pno, nx_json *out) {
	fz_buffer *b = NULL;
	int *tracer = malloc(TRACER_MAX * sizeof(int)), ntr = 0, first = 1;
	text t = {0};
	fz_var(b);
	fz_try(ctx) {
		pdf_document *pdf = pdf_specifics(ctx, doc);
		b = fz_new_buffer(ctx, 1024);
		nx_j_raw(ctx, b, "[");
		if (pdf) {
			pdf_obj *rsrc = pdf_dict_get_inheritable(ctx, pdf_lookup_page_obj(ctx, pdf, pno), PDF_NAME(Resources));
			if (rsrc) scan(ctx, b, &first, rsrc, 0, tracer, &ntr, &t);
		}
		nx_j_raw(ctx, b, "]");
		nx_json_done(ctx, b, out);
	}
	fz_always(ctx) {
		free(tracer);
		free(t.s);
	}
	fz_catch(ctx) { nx_json_fail(ctx, b, out); }
}

// image_pixmap is Pixmap(doc, xref): the image at xref decoded at its own
// size. It throws where PyMuPDF raises.
static fz_pixmap *image_pixmap(fz_context *ctx, pdf_document *pdf, int xref) {
	if (!pdf) fz_throw(ctx, FZ_ERROR_ARGUMENT, "not a PDF");
	if (xref < 1 || xref >= pdf_xref_len(ctx, pdf)) fz_throw(ctx, FZ_ERROR_ARGUMENT, "bad xref");
	pdf_obj *ref = pdf_new_indirect(ctx, pdf, xref, 0);
	fz_image *img = NULL;
	fz_pixmap *pix = NULL;
	fz_var(img);
	fz_try(ctx) {
		pdf_obj *type = pdf_dict_get(ctx, ref, PDF_NAME(Subtype));
		if (!pdf_name_eq(ctx, type, PDF_NAME(Image)) && !pdf_name_eq(ctx, type, PDF_NAME(Alpha)) &&
			!pdf_name_eq(ctx, type, PDF_NAME(Luminosity)))
			fz_throw(ctx, FZ_ERROR_ARGUMENT, "is no image");
		img = pdf_load_image(ctx, pdf, ref);
		fz_irect all = {FZ_MIN_INF_RECT, FZ_MIN_INF_RECT, FZ_MAX_INF_RECT, FZ_MAX_INF_RECT};
		fz_matrix m = fz_make_matrix(img->w, 0, 0, img->h, 0, 0);
		int w, h;
		pix = fz_get_pixmap_from_image(ctx, img, &all, &m, &w, &h);
	}
	fz_always(ctx) {
		fz_drop_image(ctx, img);
		pdf_drop_obj(ctx, ref);
	}
	fz_catch(ctx) { fz_rethrow(ctx); }
	return pix;
}

static void put_hex(fz_context *ctx, fz_buffer *b, const unsigned char *d, int n) {
	static const char hex[] = "0123456789abcdef";
	fz_append_byte(ctx, b, '"');
	for (int i = 0; i < n; i++) {
		fz_append_byte(ctx, b, hex[d[i] >> 4]);
		fz_append_byte(ctx, b, hex[d[i] & 15]);
	}
	fz_append_byte(ctx, b, '"');
}

// nx_image is Pixmap(doc, xref), converted as Pixmap(csRGB, pix) when rgb
// is set, and tobytes("png") of it: its size, colour components, alpha,
// colourspace name, MD5 digest and PNG bytes. png_error is what PyMuPDF
// raises from tobytes("png") instead (a colourspace of more than three
// components, as CMYK).
void nx_image(fz_context *ctx, fz_document *doc, int xref, int rgb, nx_json *out) {
	fz_buffer *b = NULL, *png = NULL;
	fz_pixmap *pix = NULL, *conv = NULL;
	fz_output *o = NULL;
	fz_var(b); fz_var(png); fz_var(pix); fz_var(conv); fz_var(o);
	fz_try(ctx) {
		b = fz_new_buffer(ctx, 1 << 16);
		pix = image_pixmap(ctx, pdf_specifics(ctx, doc), xref);
		if (rgb) {
			if (!fz_pixmap_colorspace(ctx, pix)) fz_throw(ctx, FZ_ERROR_ARGUMENT, "source colorspace must not be None");
			conv = fz_convert_pixmap(ctx, pix, fz_device_rgb(ctx), NULL, NULL, fz_default_color_params, 1);
			fz_drop_pixmap(ctx, pix);
			pix = conv;
			conv = NULL;
		}
		fz_colorspace *cs = fz_pixmap_colorspace(ctx, pix);
		unsigned char digest[16];
		fz_md5_pixmap(ctx, pix, digest);
		nx_j_raw(ctx, b, "{\"width\":"); nx_j_int(ctx, b, fz_pixmap_width(ctx, pix));
		nx_j_raw(ctx, b, ",\"height\":"); nx_j_int(ctx, b, fz_pixmap_height(ctx, pix));
		nx_j_raw(ctx, b, ",\"n\":"); nx_j_int(ctx, b, fz_pixmap_components(ctx, pix));
		nx_j_raw(ctx, b, fz_pixmap_alpha(ctx, pix) ? ",\"alpha\":true" : ",\"alpha\":false");
		nx_j_raw(ctx, b, ",\"colorspace\":");
		const char *name = cs ? fz_colorspace_name(ctx, cs) : "";
		nx_j_str(ctx, b, name, strlen(name));
		nx_j_raw(ctx, b, ",\"digest\":");
		put_hex(ctx, b, digest, 16);
		if (cs && fz_colorspace_n(ctx, cs) > 3) {
			nx_j_raw(ctx, b, ",\"png_error\":\"unsupported colorspace for 'png'\"}");
		} else {
			png = fz_new_buffer(ctx, (size_t)fz_pixmap_stride(ctx, pix) * fz_pixmap_height(ctx, pix));
			o = fz_new_output_with_buffer(ctx, png);
			fz_write_pixmap_as_png(ctx, o, pix);
			fz_close_output(ctx, o);
			unsigned char *data;
			size_t n = fz_buffer_storage(ctx, png, &data);
			nx_j_raw(ctx, b, ",\"png\":");
			nx_j_bytes(ctx, b, data, n);
			nx_j_raw(ctx, b, "}");
		}
		nx_json_done(ctx, b, out);
	}
	fz_always(ctx) {
		fz_drop_output(ctx, o);
		fz_drop_buffer(ctx, png);
		fz_drop_pixmap(ctx, conv);
		fz_drop_pixmap(ctx, pix);
	}
	fz_catch(ctx) { nx_json_fail(ctx, b, out); }
}

// nx_image_rects is page.get_image_rects(xref): the box of every image the
// page draws whose pixels have the same MD5 digest as the image at xref, in
// drawing order, read with the page derotated.
void nx_image_rects(fz_context *ctx, fz_document *doc, int pno, int xref, nx_json *out) {
	fz_buffer *b = NULL;
	fz_pixmap *pix = NULL, *ip = NULL;
	fz_stext_page *tp = NULL;
	fz_var(b); fz_var(pix); fz_var(ip); fz_var(tp);
	fz_try(ctx) {
		b = fz_new_buffer(ctx, 256);
		unsigned char want[16], got[16];
		pix = image_pixmap(ctx, pdf_specifics(ctx, doc), xref);
		fz_md5_pixmap(ctx, pix, want);
		tp = nx_textpage(ctx, doc, pno, FZ_STEXT_PRESERVE_IMAGES);
		int first = 1;
		nx_j_raw(ctx, b, "[");
		for (fz_stext_block *blk = tp->first_block; blk; blk = blk->next) {
			if (blk->type == FZ_STEXT_BLOCK_TEXT) continue;
			fz_image *img = blk->u.i.image;
			fz_irect all = {FZ_MIN_INF_RECT, FZ_MIN_INF_RECT, FZ_MAX_INF_RECT, FZ_MAX_INF_RECT};
			fz_matrix m = fz_make_matrix(img->w, 0, 0, img->h, 0, 0);
			int w, h;
			ip = fz_get_pixmap_from_image(ctx, img, &all, &m, &w, &h);
			fz_md5_pixmap(ctx, ip, got);
			fz_drop_pixmap(ctx, ip);
			ip = NULL;
			if (memcmp(want, got, 16)) continue;
			if (!first) nx_j_raw(ctx, b, ",");
			first = 0;
			nx_j_rect(ctx, b, blk->bbox);
		}
		nx_j_raw(ctx, b, "]");
		nx_json_done(ctx, b, out);
	}
	fz_always(ctx) {
		fz_drop_stext_page(ctx, tp);
		fz_drop_pixmap(ctx, ip);
		fz_drop_pixmap(ctx, pix);
	}
	fz_catch(ctx) { nx_json_fail(ctx, b, out); }
}

int nx_xref_len(fz_context *ctx, fz_document *doc) {
	pdf_document *pdf = pdf_specifics(ctx, doc);
	int n = 0;
	fz_try(ctx) { n = pdf ? pdf_xref_len(ctx, pdf) : 0; }
	fz_catch(ctx) { n = 0; }
	return n;
}

// nx_xref_object is doc.xref_object(xref): the object printed as PDF source
// (pdf_print_obj, not compressed, not ASCII-only), read back as
// raw-unicode-escape, as a JSON string. xref -1 is the trailer.
void nx_xref_object(fz_context *ctx, fz_document *doc, int xref, nx_json *out) {
	fz_buffer *b = NULL, *src = NULL;
	fz_output *o = NULL;
	pdf_obj *obj = NULL;
	text t = {0};
	fz_var(b); fz_var(src); fz_var(o); fz_var(obj);
	fz_try(ctx) {
		pdf_document *pdf = pdf_specifics(ctx, doc);
		if (!pdf) fz_throw(ctx, FZ_ERROR_ARGUMENT, "not a PDF");
		if ((xref < 1 || xref >= pdf_xref_len(ctx, pdf)) && xref != -1) fz_throw(ctx, FZ_ERROR_ARGUMENT, "bad xref");
		b = fz_new_buffer(ctx, 512);
		obj = xref > 0 ? pdf_load_object(ctx, pdf, xref) : pdf_keep_obj(ctx, pdf_trailer(ctx, pdf));
		src = fz_new_buffer(ctx, 512);
		o = fz_new_output_with_buffer(ctx, src);
		pdf_print_obj(ctx, o, pdf_resolve_indirect(ctx, obj), 0, 0);
		fz_close_output(ctx, o);
		unsigned char *data;
		size_t n = fz_buffer_storage(ctx, src, &data);
		nx_put_raw_escape(&t.s, &t.n, &t.cap, (const char *)data, n);
		nx_j_str(ctx, b, t.s ? t.s : "", t.n);
		nx_json_done(ctx, b, out);
	}
	fz_always(ctx) {
		fz_drop_output(ctx, o);
		fz_drop_buffer(ctx, src);
		pdf_drop_obj(ctx, obj);
		free(t.s);
	}
	fz_catch(ctx) { nx_json_fail(ctx, b, out); }
}

// put_dests writes each name of a /Dests dict as [name, printed], printed
// being obj_string of resolve_names: the destination array, or a dict's
// /D, printed tight; null where it is neither.
static void put_dests(fz_context *ctx, fz_buffer *b, int *first, pdf_obj *dict, text *t) {
	int n = pdf_dict_len(ctx, dict);
	for (int i = 0; i < n; i++) {
		pdf_obj *key = pdf_dict_get_key(ctx, dict, i), *val = pdf_dict_get_val(ctx, dict, i);
		if (!pdf_is_name(ctx, key)) continue;
		val = pdf_resolve_indirect(ctx, val);
		pdf_obj *what = pdf_is_array(ctx, val) ? val : pdf_is_dict(ctx, val) ? pdf_dict_gets(ctx, val, "D") : NULL;
		int some = pdf_is_array(ctx, val) || pdf_is_dict(ctx, val);
		const char *name = pdf_to_name(ctx, key);
		nx_j_raw(ctx, b, *first ? "[" : ",[");
		*first = 0;
		t->n = 0;
		nx_put_raw_escape(&t->s, &t->n, &t->cap, name, strlen(name));
		nx_j_str(ctx, b, t->s ? t->s : "", t->n);
		nx_j_raw(ctx, b, ",");
		if (!some) {
			nx_j_raw(ctx, b, "null]");
			continue;
		}
		fz_buffer *pb = fz_new_buffer(ctx, 256);
		fz_output *o = NULL;
		fz_var(o);
		fz_try(ctx) {
			o = fz_new_output_with_buffer(ctx, pb);
			pdf_print_obj(ctx, o, what, 1, 0);
			fz_close_output(ctx, o);
			unsigned char *data;
			size_t len = fz_buffer_storage(ctx, pb, &data);
			nx_j_bytes(ctx, b, data, len);
		}
		fz_always(ctx) {
			fz_drop_output(ctx, o);
			fz_drop_buffer(ctx, pb);
		}
		fz_catch(ctx) { fz_rethrow(ctx); }
		nx_j_raw(ctx, b, "]");
	}
}

// nx_dest_names writes what resolve_names reads: each page's xref, then
// the names of the catalog's /Dests and of the /Names/Dests tree, in that
// order (a later one replaces an earlier one of the same name).
void nx_dest_names(fz_context *ctx, fz_document *doc, nx_json *out) {
	fz_buffer *b = NULL;
	pdf_obj *tree = NULL;
	text t = {0};
	fz_var(b); fz_var(tree);
	fz_try(ctx) {
		pdf_document *pdf = pdf_specifics(ctx, doc);
		if (!pdf) fz_throw(ctx, FZ_ERROR_ARGUMENT, "not a PDF");
		b = fz_new_buffer(ctx, 1024);
		nx_j_raw(ctx, b, "{\"pages\":[");
		int n = pdf_count_pages(ctx, pdf);
		for (int i = 0; i < n; i++) {
			if (i) nx_j_raw(ctx, b, ",");
			nx_j_int(ctx, b, pdf_to_num(ctx, pdf_lookup_page_obj(ctx, pdf, i)));
		}
		nx_j_raw(ctx, b, "],\"names\":[");
		int first = 1;
		pdf_obj *catalog = pdf_dict_gets(ctx, pdf_trailer(ctx, pdf), "Root");
		pdf_obj *old = pdf_dict_get(ctx, catalog, PDF_NAME(Dests));
		if (pdf_is_dict(ctx, old)) put_dests(ctx, b, &first, old, &t);
		tree = pdf_load_name_tree(ctx, pdf, PDF_NAME(Dests));
		if (pdf_is_dict(ctx, tree)) put_dests(ctx, b, &first, tree, &t);
		nx_j_raw(ctx, b, "]}");
		nx_json_done(ctx, b, out);
	}
	fz_always(ctx) {
		pdf_drop_obj(ctx, tree);
		free(t.s);
	}
	fz_catch(ctx) { nx_json_fail(ctx, b, out); }
}
