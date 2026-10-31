//go:build cgo && !nomupdf

// What PyMuPDF 1.28.0 does to draw a page and to list the images it draws,
// written over MuPDF's C API: Page.rect (fz_bound_page), Page.get_pixmap(
// dpi=…) with Pixmap.tobytes("png") (utils.get_pixmap, then
// JM_pixmap_from_display_list without a clip, then fz_write_pixmap_as_png),
// and the bboxes of Page.get_image_info() (a text page with only
// TEXT_PRESERVE_IMAGES, read with the page derotated as get_textpage does).

#include <stdlib.h>
#include <string.h>

#include <mupdf/pdf.h>

#include "nx.h"

int nx_bound(fz_context *ctx, fz_document *doc, int pno, nx_rect *out) {
	fz_page *page = NULL;
	int failed = 0;
	fz_var(page);
	fz_try(ctx) {
		page = fz_load_page(ctx, doc, pno);
		fz_rect r = fz_bound_page(ctx, page);
		*out = (nx_rect){r.x0, r.y0, r.x1, r.y1};
	}
	fz_always(ctx) { fz_drop_page(ctx, page); }
	fz_catch(ctx) { failed = 1; }
	return failed;
}

// all_white: every sample of an opaque pixmap is 0xFF.
static int all_white(fz_context *ctx, fz_pixmap *pix) {
	int h = fz_pixmap_height(ctx, pix), stride = fz_pixmap_stride(ctx, pix);
	int row = fz_pixmap_width(ctx, pix) * fz_pixmap_components(ctx, pix);
	unsigned char *s = fz_pixmap_samples(ctx, pix);
	for (int y = 0; y < h; y++, s += stride)
		for (int x = 0; x < row; x++)
			if (s[x] != 0xFF) return 0;
	return 1;
}

// nx_render: the page's display list, annotations included, drawn in RGB
// with no alpha on white at dpi/72, its resolution set to dpi, as PNG.
void nx_render(fz_context *ctx, fz_document *doc, int pno, int dpi, nx_picture *out) {
	fz_page *page = NULL;
	fz_display_list *dl = NULL;
	fz_pixmap *pix = NULL;
	fz_device *dev = NULL;
	fz_output *o = NULL;
	fz_var(page); fz_var(dl); fz_var(pix); fz_var(dev); fz_var(o);
	fz_try(ctx) {
		page = fz_load_page(ctx, doc, pno);
		dl = fz_new_display_list_from_page(ctx, page);
		float zoom = (float)((double)dpi / 72.0);
		fz_matrix ctm = fz_scale(zoom, zoom);
		fz_irect bbox = fz_round_rect(fz_transform_rect(fz_bound_display_list(ctx, dl), ctm));
		pix = fz_new_pixmap_with_bbox(ctx, fz_device_rgb(ctx), bbox, NULL, 0);
		fz_clear_pixmap_with_value(ctx, pix, 0xFF);
		dev = fz_new_draw_device(ctx, ctm, pix);
		fz_run_display_list(ctx, dl, dev, fz_identity, fz_infinite_rect, NULL);
		fz_close_device(ctx, dev);
		fz_set_pixmap_resolution(ctx, pix, dpi, dpi);
		out->w = fz_pixmap_width(ctx, pix);
		out->h = fz_pixmap_height(ctx, pix);
		out->white = all_white(ctx, pix);
		out->buf = fz_new_buffer(ctx, (size_t)fz_pixmap_stride(ctx, pix) * out->h);
		o = fz_new_output_with_buffer(ctx, out->buf);
		fz_write_pixmap_as_png(ctx, o, pix);
		fz_close_output(ctx, o);
		out->npng = fz_buffer_storage(ctx, out->buf, &out->png);
	}
	fz_always(ctx) {
		fz_drop_output(ctx, o);
		fz_drop_device(ctx, dev);
		fz_drop_pixmap(ctx, pix);
		fz_drop_display_list(ctx, dl);
		fz_drop_page(ctx, page);
	}
	fz_catch(ctx) { out->failed = 1; }
}

void nx_free_picture(fz_context *ctx, nx_picture *p) { fz_drop_buffer(ctx, p->buf); }

static void add_box(nx_boxes *b, fz_rect r) {
	if (b->n == b->cap) {
		b->cap = b->cap ? b->cap * 2 : 16;
		b->boxes = realloc(b->boxes, b->cap * sizeof(nx_rect));
	}
	b->boxes[b->n++] = (nx_rect){r.x0, r.y0, r.x1, r.y1};
}

// nx_image_boxes is extractIMGINFO's bbox of each image block of the page's
// text page, in block order.
void nx_image_boxes(fz_context *ctx, fz_document *doc, int pno, nx_boxes *out) {
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
		opts.flags = FZ_STEXT_PRESERVE_IMAGES;
		tp = fz_new_stext_page(ctx, fz_bound_page(ctx, page));
		dev = fz_new_stext_device(ctx, tp, &opts);
		fz_run_page(ctx, page, dev, fz_identity, NULL);
		fz_close_device(ctx, dev);
		for (fz_stext_block *b = tp->first_block; b; b = b->next)
			if (b->type == FZ_STEXT_BLOCK_IMAGE) add_box(out, b->bbox);
	}
	fz_always(ctx) {
		if (pobj && old) pdf_dict_put_int(ctx, pobj, PDF_NAME(Rotate), old);
		fz_drop_device(ctx, dev);
		fz_drop_stext_page(ctx, tp);
		fz_drop_page(ctx, page);
	}
	fz_catch(ctx) { out->failed = 1; }
}

void nx_free_boxes(nx_boxes *b) { free(b->boxes); }
