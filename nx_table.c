//go:build cgo && !nomupdf

// What PyMuPDF 1.28.0's find_tables reads of a page (src/table.py), for
// tables.go: the page box and transformation matrix, the characters
// (make_chars: a rawdict with the table finder's flags and small glyph
// heights, each char also mapped by the matrix) and the drawings
// (make_edges: get_drawings()). A rotated page is first derotated as
// page_rotation_set0 does, and put back as page_rotation_reset does, which
// leaves the document changed as PyMuPDF leaves it: one more (blank)
// content stream, and no CropBox, ArtBox, BleedBox or TrimBox.

#include <stdio.h>
#include <string.h>

#include <mupdf/pdf.h>

#include "nx.h"

// The table finder's FLAGS: TEXTFLAGS_TEXT | TEXT_COLLECT_STYLES |
// TEXT_ACCURATE_BBOXES | TEXT_MEDIABOX_CLIP.
#define TABLE_FLAGS (FZ_STEXT_PRESERVE_LIGATURES | FZ_STEXT_PRESERVE_WHITESPACE | FZ_STEXT_MEDIABOX_CLIP | \
	FZ_STEXT_USE_CID_FOR_UNKNOWN_UNICODE | FZ_STEXT_COLLECT_STYLES | FZ_STEXT_ACCURATE_BBOXES)

// mediabox is JM_mediabox: the page's MediaBox, normalized, Letter when
// missing, the unit box when smaller than a point.
static fz_rect mediabox(fz_context *ctx, pdf_obj *page) {
	fz_rect m = pdf_to_rect(ctx, pdf_dict_get_inheritable(ctx, page, PDF_NAME(MediaBox)));
	if (fz_is_empty_rect(m) || fz_is_infinite_rect(m)) m = fz_make_rect(0, 0, 612, 792);
	fz_rect r = {fz_min(m.x0, m.x1), fz_min(m.y0, m.y1), fz_max(m.x0, m.x1), fz_max(m.y0, m.y1)};
	if (r.x1 - r.x0 < 1 || r.y1 - r.y0 < 1) r = fz_unit_rect;
	return r;
}

// derotation is Page.derotation_matrix: the inverse of JM_rotate_page_matrix,
// which turns by the rotation within the CropBox.
static fz_matrix derotation(fz_context *ctx, pdf_obj *page, int rot) {
	fz_rect m = mediabox(ctx, page);
	fz_rect c = pdf_to_rect(ctx, pdf_dict_get_inheritable(ctx, page, PDF_NAME(CropBox)));
	if (fz_is_infinite_rect(c) || fz_is_empty_rect(c)) c = m;
	float y0 = m.y1 - c.y1, y1 = m.y1 - c.y0;
	c.y0 = y0;
	c.y1 = y1;
	float w = c.x0 < c.x1 ? c.x1 - c.x0 : c.x0 - c.x1, h = c.y0 < c.y1 ? c.y1 - c.y0 : c.y0 - c.y1;
	fz_matrix r = rot == 90 ? fz_make_matrix(0, 1, -1, 0, h, 0) :
		rot == 180 ? fz_make_matrix(-1, 0, 0, -1, w, h) : fz_make_matrix(0, -1, 1, 0, 0, w);
	return fz_invert_matrix(r);
}

// set_mediabox is Page.set_mediabox: the other page boxes go.
static void set_mediabox(fz_context *ctx, pdf_obj *page, fz_rect r) {
	pdf_dict_put_rect(ctx, page, PDF_NAME(MediaBox), r);
	pdf_dict_del(ctx, page, PDF_NAME(CropBox));
	pdf_dict_del(ctx, page, PDF_NAME(ArtBox));
	pdf_dict_del(ctx, page, PDF_NAME(BleedBox));
	pdf_dict_del(ctx, page, PDF_NAME(TrimBox));
}

// prepend_contents is JM_insert_contents with overlay 0: the stream goes
// first among the page's /Contents. It returns its xref.
static int prepend_contents(fz_context *ctx, pdf_document *pdf, pdf_obj *page, const char *data) {
	fz_buffer *buf = fz_new_buffer_from_copied_data(ctx, (const unsigned char *)data, strlen(data));
	pdf_obj *ref = NULL, *arr = NULL;
	int xref = 0;
	fz_var(ref); fz_var(arr);
	fz_try(ctx) {
		pdf_obj *contents = pdf_dict_get(ctx, page, PDF_NAME(Contents));
		ref = pdf_add_stream(ctx, pdf, buf, NULL, 0);
		xref = pdf_to_num(ctx, ref);
		if (pdf_is_array(ctx, contents)) {
			pdf_array_insert(ctx, contents, ref, 0);
		} else {
			arr = pdf_new_array(ctx, pdf, 5);
			pdf_array_push(ctx, arr, ref);
			if (contents) pdf_array_push(ctx, arr, contents);
			pdf_dict_put(ctx, page, PDF_NAME(Contents), arr);
		}
	}
	fz_always(ctx) {
		pdf_drop_obj(ctx, arr);
		pdf_drop_obj(ctx, ref);
		fz_drop_buffer(ctx, buf);
	}
	fz_catch(ctx) { fz_rethrow(ctx); }
	return xref;
}

// nx_table_page writes {"rect", "ctm", "rawdict", "drawings"} for page pno,
// derotated first where it is rotated.
void nx_table_page(fz_context *ctx, fz_document *doc, int pno, nx_json *out) {
	fz_buffer *b = NULL, *blank = NULL;
	fz_page *page = NULL;
	pdf_obj *pobj = NULL;
	int rot = 0, xref = 0;
	fz_rect oldbox = fz_empty_rect;
	fz_var(b); fz_var(blank); fz_var(page); fz_var(pobj); fz_var(rot); fz_var(xref);
	pdf_document *pdf = pdf_specifics(ctx, doc);
	fz_try(ctx) {
		if (!pdf) fz_throw(ctx, FZ_ERROR_ARGUMENT, "not a PDF");
		b = fz_new_buffer(ctx, 1 << 16);
		pobj = pdf_lookup_page_obj(ctx, pdf, pno);
		rot = nx_norm_rotation(pdf_to_int(ctx, pdf_dict_get_inheritable(ctx, pobj, PDF_NAME(Rotate))));
		if (rot) {
			fz_rect mb = mediabox(ctx, pobj);
			oldbox = mb;
			double x0 = mb.x0, y0 = mb.y0, x1 = mb.x1, y1 = mb.y1;
			fz_matrix m0 = rot == 90 ? fz_make_matrix(1, 0, 0, 1, (float)(y1 - x1 - x0 - y0), 0) :
				rot == 270 ? fz_make_matrix(1, 0, 0, 1, 0, (float)(x1 - y1 - y0 - x0)) :
				fz_make_matrix(1, 0, 0, 1, (float)(-2 * x0), (float)(-2 * y0));
			fz_matrix m = fz_concat(m0, derotation(ctx, pobj, rot));
			char cmd[256];
			snprintf(cmd, sizeof cmd, "%g %g %g %g %g %g cm ", m.a, m.b, m.c, m.d, m.e, m.f);
			xref = prepend_contents(ctx, pdf, pobj, cmd);
			if (rot == 90 || rot == 270) set_mediabox(ctx, pobj, fz_make_rect(mb.y0, mb.x0, mb.y1, mb.x1));
			pdf_dict_put_int(ctx, pobj, PDF_NAME(Rotate), 0);
		}
		page = fz_load_page(ctx, doc, pno);
		fz_rect r = fz_bound_page(ctx, page);
		fz_rect unit = fz_unit_rect;
		fz_matrix ctm;
		pdf_page_transform(ctx, pdf_page_from_fz_page(ctx, page), &unit, &ctm);
		fz_drop_page(ctx, page);
		page = NULL;
		nx_j_raw(ctx, b, "{\"rect\":");
		nx_j_rect(ctx, b, r);
		nx_j_raw(ctx, b, ",\"ctm\":[");
		nx_j_num(ctx, b, ctm.a); nx_j_raw(ctx, b, ","); nx_j_num(ctx, b, ctm.b); nx_j_raw(ctx, b, ",");
		nx_j_num(ctx, b, ctm.c); nx_j_raw(ctx, b, ","); nx_j_num(ctx, b, ctm.d); nx_j_raw(ctx, b, ",");
		nx_j_num(ctx, b, ctm.e); nx_j_raw(ctx, b, ","); nx_j_num(ctx, b, ctm.f);
		nx_j_raw(ctx, b, "],\"rawdict\":");
		nx_write_rawdict(ctx, doc, pno, TABLE_FLAGS, 1, &ctm, b);
		nx_j_raw(ctx, b, ",\"drawings\":");
		nx_write_drawings(ctx, doc, pno, 0, b);
		nx_j_raw(ctx, b, "}");
		nx_json_done(ctx, b, out);
	}
	fz_always(ctx) {
		fz_drop_page(ctx, page);
		if (rot && xref) {
			fz_try(ctx) {
				blank = fz_new_buffer_from_copied_data(ctx, (const unsigned char *)" ", 1);
				pdf_obj *ref = pdf_new_indirect(ctx, pdf, xref, 0);
				pdf_update_stream(ctx, pdf, ref, blank, 0);
				pdf_drop_obj(ctx, ref);
				set_mediabox(ctx, pobj, oldbox);
				pdf_dict_put_int(ctx, pobj, PDF_NAME(Rotate), rot);
			}
			fz_catch(ctx) {}
		}
		fz_drop_buffer(ctx, blank);
	}
	fz_catch(ctx) {
		if (b == out->buf) b = NULL; // handed over before the failure: not ours to drop
		nx_json_fail(ctx, b, out);
	}
}
