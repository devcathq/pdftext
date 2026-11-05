//go:build cgo && !nomupdf

// What PyMuPDF 1.28.0 does for page.get_drawings(extended=…) (src/extra.i:
// the jm_lineart device, then get_drawings' own tidying in __init__.py), over
// the page run with its /Rotate set to 0. A fill followed by a stroke of the
// same items becomes one "fs" path; three lines and a close that make an
// axis-aligned box become "re"; four lines that close on themselves, outside
// a fill, become "qu". With extended, clips and transparency groups are
// listed too, each with its nesting level.

#include <math.h>
#include <stdlib.h>
#include <string.h>

#include <mupdf/pdf.h>

#include "nx.h"

enum { FILL_PATH = 1, STROKE_PATH, CLIP_PATH, CLIP_STROKE_PATH };

// An item of a path: a line l (p[0], p[1]), a curve c (p[0..3]), a box re
// (r, orient) or a quad qu (p[0..3] = ul, ur, ll, lr).
typedef struct { char op[3]; fz_point p[4]; fz_rect r; int orient; } item;

// A path as PyMuPDF's dict holds it. has_* is whether the key is there.
typedef struct {
	char type[6];
	item *items; int nitems, capi;
	fz_rect rect; // rect, or scissor for a clip
	int has_close, close;
	int even_odd; // -1: None (a stroke clip)
	float fill_opacity, stroke_opacity, width, linejoin;
	int fill_n, color_n; // -1: () (no colourspace)
	float fill[3], color[3];
	int cap[3];
	char *dashes;
	size_t seqno;
	char *layer;
	long level;
	int isolated, knockout; const char *blendmode; float opacity; // group
} path;

typedef struct {
	fz_device super;
	fz_context *ctx;
	path *out; int nout, capo;
	path cur; int have_cur;
	fz_rect *scissors; int nsc, capsc;
	float pathfactor;
	fz_matrix ctm;
	fz_point lastpoint, firstpoint;
	int havemove;
	fz_rect pathrect;
	int clips, linecount, path_type;
	long depth;
	size_t seqno;
	char *layer;
} lineart;

static char *dupstr(const char *s) {
	if (!s) return NULL;
	size_t n = strlen(s) + 1;
	char *d = malloc(n);
	memcpy(d, s, n);
	return d;
}

static void free_path(path *p) {
	free(p->items);
	free(p->dashes);
	free(p->layer);
	memset(p, 0, sizeof *p);
}

static void add_item(path *p, item it) {
	if (p->nitems == p->capi) {
		p->capi = p->capi ? p->capi * 2 : 16;
		p->items = realloc(p->items, p->capi * sizeof(item));
	}
	p->items[p->nitems++] = it;
}

static fz_rect compute_scissor(lineart *d) {
	fz_rect s = d->nsc ? fz_intersect_rect(d->scissors[d->nsc - 1], d->pathrect) : d->pathrect;
	if (d->nsc == d->capsc) {
		d->capsc = d->capsc ? d->capsc * 2 : 16;
		d->scissors = realloc(d->scissors, d->capsc * sizeof(fz_rect));
	}
	d->scissors[d->nsc++] = s;
	return s;
}

// checkquad: the last four lines close on their own start: one "qu".
static void checkquad(lineart *d) {
	path *p = &d->cur;
	item *l = p->items + p->nitems - 4;
	float f[8];
	fz_point lp = {0, 0};
	for (int i = 0; i < 4; i++) {
		f[i * 2] = l[i].p[0].x;
		f[i * 2 + 1] = l[i].p[0].y;
		lp = l[i].p[1];
	}
	if (lp.x != f[0] || lp.y != f[1]) return;
	d->linecount = 0;
	item q = {"qu"};
	q.p[0] = fz_make_point(f[0], f[1]); // ul
	q.p[1] = fz_make_point(f[6], f[7]); // ur
	q.p[2] = fz_make_point(f[2], f[3]); // ll
	q.p[3] = fz_make_point(f[4], f[5]); // lr
	p->items[p->nitems - 4] = q;
	p->nitems -= 3;
}

// checkrect: the last three lines are horizontal, vertical, horizontal and
// meet: one "re", oriented 1 when the first line is the lower one.
static int checkrect(lineart *d) {
	path *p = &d->cur;
	d->linecount = 0;
	fz_point ll = p->items[p->nitems - 3].p[0], lr = p->items[p->nitems - 3].p[1];
	fz_point ur = p->items[p->nitems - 1].p[0], ul = p->items[p->nitems - 1].p[1];
	if (ll.y != lr.y || ll.x != ul.x || ur.y != ul.y || ur.x != lr.x) return 0;
	item r = {"re"};
	if (ul.y < lr.y) {
		r.r = fz_make_rect(ul.x, ul.y, lr.x, lr.y);
		r.orient = 1;
	} else {
		r.r = fz_make_rect(ll.x, ll.y, ur.x, ur.y);
		r.orient = -1;
	}
	p->items[p->nitems - 3] = r;
	p->nitems -= 2;
	return 1;
}

static void trace_moveto(fz_context *ctx, void *arg, float x, float y) {
	lineart *d = arg;
	d->lastpoint = fz_transform_point(fz_make_point(x, y), d->ctm);
	if (fz_is_infinite_rect(d->pathrect))
		d->pathrect = fz_make_rect(d->lastpoint.x, d->lastpoint.y, d->lastpoint.x, d->lastpoint.y);
	d->firstpoint = d->lastpoint;
	d->havemove = 1;
	d->linecount = 0;
}

static void trace_lineto(fz_context *ctx, void *arg, float x, float y) {
	lineart *d = arg;
	fz_point p1 = fz_transform_point(fz_make_point(x, y), d->ctm);
	d->pathrect = fz_include_point_in_rect(d->pathrect, p1);
	item l = {"l"};
	l.p[0] = d->lastpoint;
	l.p[1] = p1;
	d->lastpoint = p1;
	add_item(&d->cur, l);
	d->linecount += 1;
	if (d->linecount == 4 && d->path_type != FILL_PATH) checkquad(d);
}

static void trace_curveto(fz_context *ctx, void *arg, float x1, float y1, float x2, float y2, float x3, float y3) {
	lineart *d = arg;
	d->linecount = 0;
	fz_point p1 = fz_transform_point(fz_make_point(x1, y1), d->ctm);
	fz_point p2 = fz_transform_point(fz_make_point(x2, y2), d->ctm);
	fz_point p3 = fz_transform_point(fz_make_point(x3, y3), d->ctm);
	d->pathrect = fz_include_point_in_rect(d->pathrect, p1);
	d->pathrect = fz_include_point_in_rect(d->pathrect, p2);
	d->pathrect = fz_include_point_in_rect(d->pathrect, p3);
	item c = {"c"};
	c.p[0] = d->lastpoint;
	c.p[1] = p1;
	c.p[2] = p2;
	c.p[3] = p3;
	d->lastpoint = p3;
	add_item(&d->cur, c);
}

// trace_close: closePath is False after a subpath that began with a move
// (PyMuPDF closes it with a line back to its start) and True otherwise.
static void trace_close(fz_context *ctx, void *arg) {
	lineart *d = arg;
	if (d->linecount == 3 && checkrect(d)) return;
	d->linecount = 0;
	if (d->havemove) {
		if (d->firstpoint.x != d->lastpoint.x || d->firstpoint.y != d->lastpoint.y) {
			item l = {"l"};
			l.p[0] = d->lastpoint;
			l.p[1] = d->firstpoint;
			d->lastpoint = d->firstpoint;
			add_item(&d->cur, l);
		}
		d->havemove = 0;
		d->cur.has_close = 1;
		d->cur.close = 0;
	} else {
		d->cur.has_close = 1;
		d->cur.close = 1;
	}
}

static const fz_path_walker walker = {trace_moveto, trace_lineto, trace_curveto, trace_close};

// walk is jm_lineart_path: a fresh path of the path's items; have_cur is 0
// when it has none.
static void walk(fz_context *ctx, lineart *d, const fz_path *fp) {
	d->pathrect = fz_infinite_rect;
	d->linecount = 0;
	d->lastpoint = fz_make_point(0, 0);
	d->firstpoint = fz_make_point(0, 0);
	free_path(&d->cur);
	d->have_cur = 1;
	fz_walk_path(ctx, fp, &walker, d);
	if (!d->cur.nitems) {
		free_path(&d->cur);
		d->have_cur = 0;
	}
}

static int same_items(path *a, path *b) {
	if (a->nitems != b->nitems) return 0;
	for (int i = 0; i < a->nitems; i++) {
		item *x = a->items + i, *y = b->items + i;
		if (strcmp(x->op, y->op)) return 0;
		int np = !strcmp(x->op, "l") ? 2 : 4;
		if (!strcmp(x->op, "re")) {
			if (x->r.x0 != y->r.x0 || x->r.y0 != y->r.y0 || x->r.x1 != y->r.x1 || x->r.y1 != y->r.y1 || x->orient != y->orient)
				return 0;
			continue;
		}
		for (int k = 0; k < np; k++)
			if (x->p[k].x != y->p[k].x || x->p[k].y != y->p[k].y) return 0;
	}
	return 1;
}

// append_merge: a stroke that repeats the fill just before it, item for
// item, joins it as "fs"; the fill keeps its own keys (rect, seqno, …).
static void append_merge(lineart *d) {
	path *p = &d->cur;
	if (d->nout && !strcmp(p->type, "s")) {
		path *prev = d->out + d->nout - 1;
		if (!strcmp(prev->type, "f") && same_items(prev, p)) {
			prev->stroke_opacity = p->stroke_opacity;
			prev->color_n = p->color_n;
			memcpy(prev->color, p->color, sizeof prev->color);
			prev->width = p->width;
			memcpy(prev->cap, p->cap, sizeof prev->cap);
			prev->linejoin = p->linejoin;
			if (!prev->has_close) {
				prev->has_close = 1;
				prev->close = p->close;
			}
			prev->dashes = p->dashes;
			p->dashes = NULL;
			strcpy(prev->type, "fs");
			free_path(p);
			d->have_cur = 0;
			return;
		}
	}
	if (d->nout == d->capo) {
		d->capo = d->capo ? d->capo * 2 : 64;
		d->out = realloc(d->out, d->capo * sizeof(path));
	}
	d->out[d->nout++] = *p;
	memset(p, 0, sizeof *p);
	d->have_cur = 0;
}

static int rgb(fz_context *ctx, fz_colorspace *cs, const float *color, float out[3]) {
	if (!cs) return -1;
	fz_convert_color(ctx, cs, color, fz_device_rgb(ctx), out, NULL, fz_default_color_params);
	return 3;
}

static void fill_path(fz_context *ctx, fz_device *dev, const fz_path *fp, int even_odd, fz_matrix ctm,
	fz_colorspace *cs, const float *color, float alpha, fz_color_params cp) {
	lineart *d = (lineart *)dev;
	d->ctm = ctm;
	d->path_type = FILL_PATH;
	walk(ctx, d, fp);
	if (!d->have_cur) return;
	path *p = &d->cur;
	strcpy(p->type, "f");
	p->even_odd = even_odd ? 1 : 0;
	p->fill_opacity = alpha;
	p->fill_n = rgb(ctx, cs, color, p->fill);
	p->rect = d->pathrect;
	p->seqno = d->seqno;
	p->layer = dupstr(d->layer);
	p->level = d->depth;
	append_merge(d);
	d->seqno += 1;
}

static void stroke_path(fz_context *ctx, fz_device *dev, const fz_path *fp, const fz_stroke_state *st,
	fz_matrix ctm, fz_colorspace *cs, const float *color, float alpha, fz_color_params cp) {
	lineart *d = (lineart *)dev;
	d->pathfactor = sqrtf(fabsf(ctm.a * ctm.d - ctm.b * ctm.c));
	d->ctm = ctm;
	d->path_type = STROKE_PATH;
	walk(ctx, d, fp);
	if (!d->have_cur) return;
	path *p = &d->cur;
	strcpy(p->type, "s");
	p->stroke_opacity = alpha;
	p->color_n = rgb(ctx, cs, color, p->color);
	p->width = d->pathfactor * st->linewidth;
	p->cap[0] = st->start_cap;
	p->cap[1] = st->dash_cap;
	p->cap[2] = st->end_cap;
	p->linejoin = (float)st->linejoin;
	if (!p->has_close) {
		p->has_close = 1;
		p->close = 0;
	}
	fz_buffer *buf = fz_new_buffer(ctx, 256);
	fz_try(ctx) {
		if (st->dash_len) {
			fz_append_string(ctx, buf, "[ ");
			for (int i = 0; i < st->dash_len; i++)
				fz_append_printf(ctx, buf, "%g ", d->pathfactor * st->dash_list[i]);
			fz_append_printf(ctx, buf, "] %g", d->pathfactor * st->dash_phase);
		} else {
			fz_append_string(ctx, buf, "[] 0");
		}
		fz_terminate_buffer(ctx, buf);
		p->dashes = dupstr(fz_string_from_buffer(ctx, buf));
	}
	fz_always(ctx) { fz_drop_buffer(ctx, buf); }
	fz_catch(ctx) { fz_rethrow(ctx); }
	p->rect = d->pathrect;
	p->layer = dupstr(d->layer);
	p->seqno = d->seqno;
	p->level = d->depth;
	append_merge(d);
	d->seqno += 1;
}

static void clip(fz_context *ctx, lineart *d, const fz_path *fp, fz_matrix ctm, int type, int even_odd) {
	if (!d->clips) return;
	d->ctm = ctm;
	d->path_type = type;
	walk(ctx, d, fp);
	if (!d->have_cur) return;
	path *p = &d->cur;
	strcpy(p->type, "clip");
	p->even_odd = even_odd;
	if (!p->has_close) {
		p->has_close = 1;
		p->close = 0;
	}
	p->rect = compute_scissor(d);
	p->level = d->depth;
	p->layer = dupstr(d->layer);
	append_merge(d);
	d->depth++;
}

static void clip_path(fz_context *ctx, fz_device *dev, const fz_path *fp, int even_odd, fz_matrix ctm, fz_rect sc) {
	clip(ctx, (lineart *)dev, fp, ctm, CLIP_PATH, even_odd ? 1 : 0);
}

static void clip_stroke_path(fz_context *ctx, fz_device *dev, const fz_path *fp, const fz_stroke_state *st, fz_matrix ctm, fz_rect sc) {
	clip(ctx, (lineart *)dev, fp, ctm, CLIP_STROKE_PATH, -1);
}

// A text or image-mask clip opens a level without a path of its own; its
// scissor is the last path's box.
static void clip_other(lineart *d) {
	if (!d->clips) return;
	compute_scissor(d);
	d->depth++;
}

static void clip_text(fz_context *ctx, fz_device *dev, const fz_text *t, fz_matrix ctm, fz_rect sc) { clip_other((lineart *)dev); }
static void clip_stroke_text(fz_context *ctx, fz_device *dev, const fz_text *t, const fz_stroke_state *st, fz_matrix ctm, fz_rect sc) { clip_other((lineart *)dev); }
static void clip_image_mask(fz_context *ctx, fz_device *dev, fz_image *img, fz_matrix ctm, fz_rect sc) { clip_other((lineart *)dev); }

static void pop_clip(fz_context *ctx, fz_device *dev) {
	lineart *d = (lineart *)dev;
	if (!d->clips || !d->nsc) return;
	d->nsc--;
	d->depth--;
}

static void begin_group(fz_context *ctx, fz_device *dev, fz_rect bbox, fz_colorspace *cs, int isolated, int knockout, int blendmode, float alpha) {
	lineart *d = (lineart *)dev;
	if (!d->clips) return;
	free_path(&d->cur);
	path *p = &d->cur;
	strcpy(p->type, "group");
	p->rect = bbox;
	p->isolated = isolated ? 1 : 0;
	p->knockout = knockout ? 1 : 0;
	p->blendmode = fz_blendmode_name(blendmode);
	p->opacity = alpha;
	p->level = d->depth;
	p->layer = dupstr(d->layer);
	append_merge(d);
	d->depth++;
}

static void end_group(fz_context *ctx, fz_device *dev) {
	lineart *d = (lineart *)dev;
	if (d->clips) d->depth--;
}

// Text, shades and images draw nothing here but take a sequence number.
static void seq(fz_device *dev) { ((lineart *)dev)->seqno += 1; }
static void fill_text(fz_context *ctx, fz_device *dev, const fz_text *t, fz_matrix m, fz_colorspace *cs, const float *c, float a, fz_color_params cp) { seq(dev); }
static void stroke_text(fz_context *ctx, fz_device *dev, const fz_text *t, const fz_stroke_state *s, fz_matrix m, fz_colorspace *cs, const float *c, float a, fz_color_params cp) { seq(dev); }
static void ignore_text(fz_context *ctx, fz_device *dev, const fz_text *t, fz_matrix m) { seq(dev); }
static void fill_shade(fz_context *ctx, fz_device *dev, fz_shade *s, fz_matrix m, float a, fz_color_params cp) { seq(dev); }
static void fill_image(fz_context *ctx, fz_device *dev, fz_image *i, fz_matrix m, float a, fz_color_params cp) { seq(dev); }
static void fill_image_mask(fz_context *ctx, fz_device *dev, fz_image *i, fz_matrix m, fz_colorspace *cs, const float *c, float a, fz_color_params cp) { seq(dev); }

static void begin_layer(fz_context *ctx, fz_device *dev, const char *name) {
	lineart *d = (lineart *)dev;
	free(d->layer);
	d->layer = dupstr(name);
}

static void end_layer(fz_context *ctx, fz_device *dev) {
	lineart *d = (lineart *)dev;
	free(d->layer);
	d->layer = NULL;
}

static void drop_dev(fz_context *ctx, fz_device *dev) {
	lineart *d = (lineart *)dev;
	for (int i = 0; i < d->nout; i++) free_path(d->out + i);
	free(d->out);
	free_path(&d->cur);
	free(d->scissors);
	free(d->layer);
}

static void put_color(fz_context *ctx, fz_buffer *b, int n, const float *c) {
	if (n < 0) { nx_j_raw(ctx, b, "[]"); return; }
	nx_j_raw(ctx, b, "[");
	for (int i = 0; i < n; i++) {
		if (i) nx_j_raw(ctx, b, ",");
		nx_j_num(ctx, b, c[i]);
	}
	nx_j_raw(ctx, b, "]");
}

static void put_bool(fz_context *ctx, fz_buffer *b, const char *key, int v) {
	nx_j_raw(ctx, b, key);
	nx_j_raw(ctx, b, v < 0 ? "null" : v ? "true" : "false");
}

// put_path writes a path as get_drawings returns it: a box item normalized,
// and a path of only fill or only stroke without the other's keys (Go reads
// them as absent).
static void put_path(fz_context *ctx, fz_buffer *b, path *p, int extended) {
	int group = !strcmp(p->type, "group"), isclip = !strcmp(p->type, "clip");
	int filled = p->type[0] == 'f', stroked = !strcmp(p->type, "s") || !strcmp(p->type, "fs");
	nx_j_raw(ctx, b, "{\"type\":");
	nx_j_str(ctx, b, p->type, strlen(p->type));
	nx_j_raw(ctx, b, isclip ? ",\"scissor\":" : ",\"rect\":");
	nx_j_rect(ctx, b, p->rect);
	nx_j_raw(ctx, b, ",\"layer\":");
	nx_j_str(ctx, b, p->layer ? p->layer : "", p->layer ? strlen(p->layer) : 0);
	if (extended || isclip || group) {
		nx_j_raw(ctx, b, ",\"level\":");
		nx_j_int(ctx, b, p->level);
	}
	if (group) {
		put_bool(ctx, b, ",\"isolated\":", p->isolated);
		put_bool(ctx, b, ",\"knockout\":", p->knockout);
		nx_j_raw(ctx, b, ",\"blendmode\":");
		nx_j_str(ctx, b, p->blendmode, strlen(p->blendmode));
		nx_j_raw(ctx, b, ",\"opacity\":");
		nx_j_num(ctx, b, p->opacity);
		nx_j_raw(ctx, b, "}");
		return;
	}
	nx_j_raw(ctx, b, ",\"items\":[");
	for (int i = 0; i < p->nitems; i++) {
		item *it = p->items + i;
		nx_j_raw(ctx, b, i ? ",{\"op\":" : "{\"op\":");
		nx_j_str(ctx, b, it->op, strlen(it->op));
		if (!strcmp(it->op, "re")) {
			fz_rect r = it->r; // Rect(…).normalize()
			if (r.x1 < r.x0) { float t = r.x0; r.x0 = r.x1; r.x1 = t; }
			if (r.y1 < r.y0) { float t = r.y0; r.y0 = r.y1; r.y1 = t; }
			nx_j_raw(ctx, b, ",\"rect\":");
			nx_j_rect(ctx, b, r);
			nx_j_raw(ctx, b, ",\"orientation\":");
			nx_j_int(ctx, b, it->orient);
		} else {
			int np = !strcmp(it->op, "l") ? 2 : 4;
			nx_j_raw(ctx, b, ",\"points\":[");
			for (int k = 0; k < np; k++) {
				if (k) nx_j_raw(ctx, b, ",");
				nx_j_point(ctx, b, it->p[k]);
			}
			nx_j_raw(ctx, b, "]");
		}
		nx_j_raw(ctx, b, "}");
	}
	nx_j_raw(ctx, b, "]");
	if (p->has_close) put_bool(ctx, b, ",\"closePath\":", p->close);
	if (isclip || filled) put_bool(ctx, b, ",\"even_odd\":", p->even_odd);
	if (isclip) {
		nx_j_raw(ctx, b, "}");
		return;
	}
	nx_j_raw(ctx, b, ",\"seqno\":");
	nx_j_int(ctx, b, (long long)p->seqno);
	if (filled) {
		nx_j_raw(ctx, b, ",\"fill\":");
		put_color(ctx, b, p->fill_n, p->fill);
		nx_j_raw(ctx, b, ",\"fill_opacity\":");
		nx_j_num(ctx, b, p->fill_opacity);
	}
	if (stroked) {
		nx_j_raw(ctx, b, ",\"color\":");
		put_color(ctx, b, p->color_n, p->color);
		nx_j_raw(ctx, b, ",\"stroke_opacity\":");
		nx_j_num(ctx, b, p->stroke_opacity);
		nx_j_raw(ctx, b, ",\"width\":");
		nx_j_num(ctx, b, p->width);
		nx_j_raw(ctx, b, ",\"lineCap\":[");
		nx_j_int(ctx, b, p->cap[0]); nx_j_raw(ctx, b, ",");
		nx_j_int(ctx, b, p->cap[1]); nx_j_raw(ctx, b, ",");
		nx_j_int(ctx, b, p->cap[2]);
		nx_j_raw(ctx, b, "],\"lineJoin\":");
		nx_j_num(ctx, b, p->linejoin);
		nx_j_raw(ctx, b, ",\"dashes\":");
		nx_j_str(ctx, b, p->dashes ? p->dashes : "", p->dashes ? strlen(p->dashes) : 0);
	}
	nx_j_raw(ctx, b, "}");
}

// nx_write_drawings appends page pno's drawings to b. It throws on failure.
void nx_write_drawings(fz_context *ctx, fz_document *doc, int pno, int extended, fz_buffer *b) {
	fz_page *page = NULL;
	lineart *d = NULL;
	pdf_obj *pobj = NULL;
	int old = 0;
	fz_var(page); fz_var(d); fz_var(pobj); fz_var(old);
	pdf_document *pdf = pdf_specifics(ctx, doc);
	fz_try(ctx) {
		page = fz_load_page(ctx, doc, pno);
		if (pdf) {
			pobj = pdf_lookup_page_obj(ctx, pdf, pno);
			old = nx_norm_rotation(pdf_to_int(ctx, pdf_dict_get_inheritable(ctx, pobj, PDF_NAME(Rotate))));
			if (old) pdf_dict_put_int(ctx, pobj, PDF_NAME(Rotate), 0);
		}
		d = fz_new_derived_device(ctx, lineart);
		d->super.drop_device = drop_dev;
		d->super.fill_path = fill_path;
		d->super.stroke_path = stroke_path;
		d->super.clip_path = clip_path;
		d->super.clip_stroke_path = clip_stroke_path;
		d->super.fill_text = fill_text;
		d->super.stroke_text = stroke_text;
		d->super.clip_text = clip_text;
		d->super.clip_stroke_text = clip_stroke_text;
		d->super.ignore_text = ignore_text;
		d->super.fill_shade = fill_shade;
		d->super.fill_image = fill_image;
		d->super.fill_image_mask = fill_image_mask;
		d->super.clip_image_mask = clip_image_mask;
		d->super.pop_clip = pop_clip;
		d->super.begin_group = begin_group;
		d->super.end_group = end_group;
		d->super.begin_layer = begin_layer;
		d->super.end_layer = end_layer;
		d->clips = extended;
		fz_run_page(ctx, page, &d->super, fz_identity, NULL);
		fz_close_device(ctx, &d->super);
		nx_j_raw(ctx, b, "[");
		for (int i = 0; i < d->nout; i++) {
			if (i) nx_j_raw(ctx, b, ",");
			put_path(ctx, b, d->out + i, extended);
		}
		nx_j_raw(ctx, b, "]");
	}
	fz_always(ctx) {
		if (pobj && old) pdf_dict_put_int(ctx, pobj, PDF_NAME(Rotate), old);
		fz_drop_device(ctx, &d->super);
		fz_drop_page(ctx, page);
	}
	fz_catch(ctx) { fz_rethrow(ctx); }
}

void nx_drawings(fz_context *ctx, fz_document *doc, int pno, int extended, nx_json *out) {
	fz_buffer *b = NULL;
	fz_var(b);
	fz_try(ctx) {
		b = fz_new_buffer(ctx, 1 << 14);
		nx_write_drawings(ctx, doc, pno, extended, b);
		nx_json_done(ctx, b, out);
	}
	fz_catch(ctx) { nx_json_fail(ctx, b, out); }
}
