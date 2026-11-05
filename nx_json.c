//go:build cgo && !nomupdf

// The JSON the newer readers hand to Go (nx_text.c, nx_vector.c,
// nx_image.c): numbers as the exact double of MuPDF's float32, so Go reads
// back the same float32; text as UTF-8; bytes as base64, which Go's
// encoding/json decodes into []byte.

#include <math.h>
#include <stdio.h>
#include <string.h>

#include "nx.h"

void nx_j_raw(fz_context *ctx, fz_buffer *b, const char *s) { fz_append_string(ctx, b, s); }

void nx_j_str(fz_context *ctx, fz_buffer *b, const char *s, size_t n) {
	static const char hex[] = "0123456789abcdef";
	fz_append_byte(ctx, b, '"');
	for (size_t i = 0; i < n; i++) {
		unsigned char c = (unsigned char)s[i];
		if (c == '"' || c == '\\') {
			fz_append_byte(ctx, b, '\\');
			fz_append_byte(ctx, b, c);
		} else if (c < 0x20) {
			char e[7] = {'\\', 'u', '0', '0', hex[c >> 4], hex[c & 15], 0};
			fz_append_string(ctx, b, e);
		} else {
			fz_append_byte(ctx, b, c);
		}
	}
	fz_append_byte(ctx, b, '"');
}

void nx_j_num(fz_context *ctx, fz_buffer *b, float f) {
	char s[40];
	if (isnan(f) || isinf(f)) {
		fz_append_string(ctx, b, "null");
		return;
	}
	snprintf(s, sizeof s, "%.17g", (double)f);
	fz_append_string(ctx, b, s);
}

void nx_j_double(fz_context *ctx, fz_buffer *b, double f) {
	char s[40];
	if (isnan(f) || isinf(f)) {
		fz_append_string(ctx, b, "null");
		return;
	}
	snprintf(s, sizeof s, "%.17g", f);
	fz_append_string(ctx, b, s);
}

void nx_j_int(fz_context *ctx, fz_buffer *b, long long v) {
	char s[24];
	snprintf(s, sizeof s, "%lld", v);
	fz_append_string(ctx, b, s);
}

void nx_j_rect(fz_context *ctx, fz_buffer *b, fz_rect r) {
	fz_append_byte(ctx, b, '[');
	nx_j_num(ctx, b, r.x0); fz_append_byte(ctx, b, ',');
	nx_j_num(ctx, b, r.y0); fz_append_byte(ctx, b, ',');
	nx_j_num(ctx, b, r.x1); fz_append_byte(ctx, b, ',');
	nx_j_num(ctx, b, r.y1);
	fz_append_byte(ctx, b, ']');
}

void nx_j_point(fz_context *ctx, fz_buffer *b, fz_point p) {
	fz_append_byte(ctx, b, '[');
	nx_j_num(ctx, b, p.x); fz_append_byte(ctx, b, ',');
	nx_j_num(ctx, b, p.y);
	fz_append_byte(ctx, b, ']');
}

void nx_j_bytes(fz_context *ctx, fz_buffer *b, const unsigned char *d, size_t n) {
	static const char t[] = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
	fz_append_byte(ctx, b, '"');
	size_t i = 0;
	for (; i + 2 < n; i += 3) {
		unsigned v = d[i] << 16 | d[i + 1] << 8 | d[i + 2];
		char q[5] = {t[v >> 18], t[(v >> 12) & 63], t[(v >> 6) & 63], t[v & 63], 0};
		fz_append_string(ctx, b, q);
	}
	if (n - i == 1) {
		unsigned v = d[i] << 16;
		char q[5] = {t[v >> 18], t[(v >> 12) & 63], '=', '=', 0};
		fz_append_string(ctx, b, q);
	} else if (n - i == 2) {
		unsigned v = d[i] << 16 | d[i + 1] << 8;
		char q[5] = {t[v >> 18], t[(v >> 12) & 63], t[(v >> 6) & 63], '=', 0};
		fz_append_string(ctx, b, q);
	}
	fz_append_byte(ctx, b, '"');
}

// nx_json_done hands the buffer to out; nx_json_fail drops it and records
// MuPDF's message. Both are called after the reader's fz_try.
void nx_json_done(fz_context *ctx, fz_buffer *b, nx_json *out) {
	out->buf = b;
	out->n = fz_buffer_storage(ctx, b, &out->data);
}

void nx_json_fail(fz_context *ctx, fz_buffer *b, nx_json *out) {
	fz_drop_buffer(ctx, b);
	out->failed = 1;
	snprintf(out->err, sizeof out->err, "%s", fz_caught_message(ctx));
}

void nx_free_json(fz_context *ctx, nx_json *j) { fz_drop_buffer(ctx, j->buf); }
