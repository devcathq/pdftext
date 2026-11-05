//go:build cgo && !nomupdf

package pdftext

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
)

// The shapes below are what scripts/pymupdf_golden.py writes for a page's
// blocks, rawdict and drawings (PyMuPDF's tuples, Rects and Points as lists,
// bytes as their SHA-256), built from what Go read, so the two compare as
// JSON values.

func f64(v float32) float64 { return float64(v) }

func rectShape(r Rect) []any   { return []any{f64(r.X0), f64(r.Y0), f64(r.X1), f64(r.Y1)} }
func pointShape(p Point) []any { return []any{f64(p.X), f64(p.Y)} }

func blocksShape(bs []Block) any {
	out := []any{}
	for _, b := range bs {
		out = append(out, []any{f64(b.X0), f64(b.Y0), f64(b.X1), f64(b.Y1), b.Text, b.No, b.Type})
	}
	return out
}

func sha(b []byte) any {
	if b == nil {
		return nil
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func rawdictShape(t TextDict) any {
	blocks := []any{}
	for _, b := range t.Blocks {
		if b.Type != 0 {
			tr := []any{}
			for _, v := range b.Transform {
				tr = append(tr, f64(v))
			}
			blocks = append(blocks, map[string]any{
				"type": b.Type, "number": b.Number, "bbox": rectShape(b.BBox),
				"width": b.Width, "height": b.Height, "ext": b.Ext, "colorspace": b.ColorSpace,
				"xres": b.XRes, "yres": b.YRes, "bpc": b.BPC, "transform": tr, "size": b.Size,
				"image": sha(append([]byte{}, b.Image...)), "mask": sha(b.Mask),
			})
			continue
		}
		lines := []any{}
		for _, l := range b.Lines {
			spans := []any{}
			for _, s := range l.Spans {
				chars := []any{}
				for _, c := range s.Chars {
					chars = append(chars, map[string]any{
						"origin": pointShape(c.Origin), "bbox": rectShape(c.BBox), "c": c.C, "synthetic": c.Synthetic,
					})
				}
				spans = append(spans, map[string]any{
					"size": f64(s.Size), "flags": s.Flags, "bidi": s.Bidi, "char_flags": s.CharFlags,
					"font": s.Font, "color": s.Color, "alpha": s.Alpha,
					"ascender": f64(s.Ascender), "descender": f64(s.Descender),
					"chars": chars, "origin": pointShape(s.Origin), "bbox": rectShape(s.BBox),
				})
			}
			lines = append(lines, map[string]any{
				"spans": spans, "wmode": l.WMode, "dir": pointShape(l.Dir), "bbox": rectShape(l.BBox),
			})
		}
		blocks = append(blocks, map[string]any{
			"type": b.Type, "number": b.Number, "flags": b.Flags, "bbox": rectShape(b.BBox), "lines": lines,
		})
	}
	return map[string]any{"width": t.Width, "height": t.Height, "blocks": blocks}
}

func colorShape(c []float32) any {
	if c == nil {
		return nil
	}
	out := []any{}
	for _, v := range c {
		out = append(out, f64(v))
	}
	return out
}

func boolShape(b *bool) any {
	if b == nil {
		return nil
	}
	return *b
}

func itemsShape(items []PathItem) any {
	out := []any{}
	for _, it := range items {
		switch it.Op {
		case "re":
			out = append(out, []any{it.Op, rectShape(it.Rect), it.Orientation})
		case "qu":
			pts := []any{}
			for _, p := range it.Points {
				pts = append(pts, pointShape(p))
			}
			out = append(out, []any{it.Op, pts})
		default:
			v := []any{it.Op}
			for _, p := range it.Points {
				v = append(v, pointShape(p))
			}
			out = append(out, v)
		}
	}
	return out
}

func drawingsShape(paths []Path, extended bool) any {
	out := []any{}
	for _, p := range paths {
		m := map[string]any{"type": p.Type, "layer": p.Layer}
		if extended {
			m["level"] = p.Level
		}
		switch p.Type {
		case "group":
			m["rect"] = rectShape(p.Rect)
			m["isolated"], m["knockout"] = p.Isolated, p.Knockout
			m["blendmode"], m["opacity"] = p.BlendMode, f64(p.Opacity)
		case "clip":
			m["items"] = itemsShape(p.Items)
			m["scissor"] = rectShape(p.Scissor)
			m["even_odd"], m["closePath"] = boolShape(p.EvenOdd), boolShape(p.ClosePath)
		default:
			m["items"] = itemsShape(p.Items)
			m["rect"] = rectShape(p.Rect)
			m["seqno"] = p.Seqno
			m["closePath"] = boolShape(p.ClosePath)
			for _, k := range []string{"fill", "fill_opacity", "even_odd", "color", "stroke_opacity", "width", "lineCap", "lineJoin", "dashes"} {
				m[k] = nil
			}
			if p.Type != "s" {
				m["fill"], m["fill_opacity"], m["even_odd"] = colorShape(p.Fill), f64(p.FillOpacity), boolShape(p.EvenOdd)
			}
			if p.Type != "f" {
				m["color"], m["stroke_opacity"], m["width"] = colorShape(p.Color), f64(p.StrokeOpacity), f64(p.Width)
				m["lineCap"] = []any{p.LineCap[0], p.LineCap[1], p.LineCap[2]}
				m["lineJoin"], m["dashes"] = f64(p.LineJoin), p.Dashes
			}
		}
		out = append(out, m)
	}
	return out
}

// sameShape compares Go's shape with PyMuPDF's as JSON values; where they
// differ it names the first place.
func sameShape(got, want any) (bool, string) {
	data, err := json.Marshal(got)
	if err != nil {
		return false, err.Error()
	}
	var g any
	if err := json.Unmarshal(data, &g); err != nil {
		return false, err.Error()
	}
	if reflect.DeepEqual(g, want) {
		return true, ""
	}
	return false, firstDiff("", g, want)
}

func firstDiff(at string, g, w any) string {
	switch wv := w.(type) {
	case map[string]any:
		gv, ok := g.(map[string]any)
		if !ok {
			return fmt.Sprintf("%s: Go %v, PyMuPDF %v", at, short(g), short(w))
		}
		keys := []string{}
		for k := range wv {
			keys = append(keys, k)
		}
		for k := range gv {
			if _, ok := wv[k]; !ok {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		for _, k := range keys {
			if !reflect.DeepEqual(gv[k], wv[k]) {
				return firstDiff(at+"."+k, gv[k], wv[k])
			}
		}
	case []any:
		gv, ok := g.([]any)
		if !ok {
			return fmt.Sprintf("%s: Go %v, PyMuPDF %v", at, short(g), short(w))
		}
		for i := 0; i < len(wv) && i < len(gv); i++ {
			if !reflect.DeepEqual(gv[i], wv[i]) {
				return firstDiff(fmt.Sprintf("%s[%d]", at, i), gv[i], wv[i])
			}
		}
		if len(gv) != len(wv) {
			return fmt.Sprintf("%s: Go %d items, PyMuPDF %d", at, len(gv), len(wv))
		}
	}
	return fmt.Sprintf("%s: Go %v, PyMuPDF %v", at, short(g), short(w))
}

func short(v any) string {
	s := fmt.Sprintf("%v", v)
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}
