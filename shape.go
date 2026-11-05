package pdftext

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// What PyMuPDF 1.28.0's Shape writes into a page's content stream for
// Page.draw_line, draw_rect, insert_text and insert_textbox (src/__init__.py),
// in Python's float64 arithmetic. write_cgo.go adds the fonts, opacity
// states and the stream itself.

// Color is a colour as PyMuPDF takes it: nil for None, 1 component grey,
// 3 RGB, 4 CMYK, each 0 to 1. An empty, non-nil Color is falsy like ().
type Color []float64

// DrawOptions are draw_line's and draw_rect's arguments. NewDrawOptions
// gives PyMuPDF's defaults (black, width 1, opaque); the zero value is not.
type DrawOptions struct {
	Color, Fill                Color
	Width                      float64
	Dashes                     string // as "[3 2] 0"; "" for none
	LineCap, LineJoin          int
	EvenOdd                    bool
	StrokeOpacity, FillOpacity float64
	Background                 bool // overlay=False
}

// NewDrawOptions is draw_rect's and draw_line's defaults.
func NewDrawOptions() DrawOptions {
	return DrawOptions{Color: Color{0}, Width: 1, StrokeOpacity: 1, FillOpacity: 1}
}

// TextOptions are insert_text's and insert_textbox's arguments.
// NewTextOptions gives PyMuPDF's defaults; the zero value is not.
type TextOptions struct {
	FontName                   string // a Base-14 name ("helv", "tiro", …) or a CJK one ("japan", "china-s", …)
	FontSize                   float64
	LineHeight                 float64 // 0: from the font
	Color, Fill                Color
	RenderMode                 int
	BorderWidth                float64
	MiterLimit                 float64
	Rotate                     int
	Align                      int // insert_textbox: 0 left, 1 centre, 2 right, 3 justify
	ExpandTabs                 int // insert_textbox
	StrokeOpacity, FillOpacity float64
	Background                 bool // overlay=False
}

// NewTextOptions is insert_text's and insert_textbox's defaults.
func NewTextOptions() TextOptions {
	return TextOptions{FontName: "helv", FontSize: 11, BorderWidth: 0.05, MiterLimit: 1, ExpandTabs: 1,
		StrokeOpacity: 1, FillOpacity: 1}
}

// The text alignments of insert_textbox.
const (
	AlignLeft = iota
	AlignCenter
	AlignRight
	AlignJustify
)

// base14 is PyMuPDF's Base14_fontdict.
var base14 = map[string]string{
	"helv": "Helvetica", "heit": "Helvetica-Oblique", "hebo": "Helvetica-Bold", "hebi": "Helvetica-BoldOblique",
	"cour": "Courier", "coit": "Courier-Oblique", "cobo": "Courier-Bold", "cobi": "Courier-BoldOblique",
	"tiro": "Times-Roman", "tibo": "Times-Bold", "tiit": "Times-Italic", "tibi": "Times-BoldItalic",
	"helvetica": "Helvetica", "helvetica-oblique": "Helvetica-Oblique", "helvetica-bold": "Helvetica-Bold",
	"helvetica-boldoblique": "Helvetica-BoldOblique", "courier": "Courier", "courier-oblique": "Courier-Oblique",
	"courier-bold": "Courier-Bold", "courier-boldoblique": "Courier-BoldOblique", "times-roman": "Times-Roman",
	"times-bold": "Times-Bold", "times-italic": "Times-Italic", "times-bolditalic": "Times-BoldItalic",
}

// cjkFont is a CJK font name's ordering and serif flag, as insert_font
// reads them.
func cjkFont(name string) (ordering int, serif bool, ok bool) {
	for i, n := range []string{"china-t", "china-s", "japan", "korea"} {
		if name == n {
			return i, false, true
		}
	}
	for i, n := range []string{"china-ts", "china-ss", "japan-s", "korea-s"} {
		if name == n {
			return i, true, true
		}
	}
	return -1, false, false
}

// fontInfo is a document's FontInfos entry for an inserted font.
type fontInfo struct {
	name                string // BaseFont
	simple              bool
	ordering            int
	ascender, descender float64
	adv                 []float64 // for a Base-14 font: each code's advance
}

// pageGeom is what a Shape takes from its page.
type pageGeom struct {
	width, height float64 // mediabox_size
	x, y          float64 // cropbox_position
	ipctm         [6]float32
}

// invert is util_invert_matrix: computed in float64 from float32, each
// result stored as float32 as the FzMatrix fields it is written to.
func invert(m [6]float32) ([6]float32, bool) {
	a, b, c, d, e, f := float64(m[0]), float64(m[1]), float64(m[2]), float64(m[3]), float64(m[4]), float64(m[5])
	det := a*d - b*c
	if det >= -2.220446049250313e-16 && det <= 2.220446049250313e-16 {
		return [6]float32{}, false
	}
	r := 1 / det
	var o [6]float32
	o[0] = float32(d * r)
	o[1] = float32(-b * r)
	o[2] = float32(-c * r)
	o[3] = float32(a * r)
	ne := -e*float64(o[0]) - f*float64(o[2])
	o[5] = float32(-e*float64(o[1]) - f*float64(o[3]))
	o[4] = float32(ne)
	return o, true
}

// transform is Point * Matrix: fz_transform_point in float32, each product
// rounded on its own as MuPDF built without fused multiply-add rounds it.
func transform(x, y float64, m [6]float32) (float64, float64) {
	px, py := float32(x), float32(y)
	tx := float32(float32(px*m[0])+float32(py*m[2])) + m[4]
	ty := float32(float32(px*m[1])+float32(py*m[3])) + m[5]
	return float64(tx), float64(ty)
}

// roundN is Python's round(x, n).
func roundN(x float64, n int) float64 {
	v, _ := strconv.ParseFloat(strconv.FormatFloat(x, 'f', n, 64), 64)
	return v
}

// tuple is JM_TUPLE: each value rounded to 5 places, or 0 below 1e-4.
func tuple(vs ...float64) []float64 {
	out := make([]float64, len(vs))
	for i, v := range vs {
		if math.Abs(v) >= 1e-4 {
			out[i] = roundN(v, 5)
		}
	}
	return out
}

// fmtG is _format_g, MuPDF's "%g": the shortest digits of the value as a
// float32, without exponent or leading zero.
func fmtG(vs ...float64) string {
	parts := make([]string, len(vs))
	for i, v := range vs {
		s := strconv.FormatFloat(float64(float32(v)), 'f', -1, 32)
		if strings.HasPrefix(s, "0.") {
			s = s[1:]
		} else if strings.HasPrefix(s, "-0.") {
			s = "-" + s[2:]
		}
		parts[i] = s
	}
	return strings.Join(parts, " ")
}

// colorCode is ColorCode: c for stroke, f for fill.
func colorCode(c Color, f string) (string, error) {
	if len(c) == 0 {
		return "", nil
	}
	for _, v := range c {
		if v < 0 || v > 1 {
			return "", errors.New("pdftext: need 1, 3 or 4 color components in range 0 to 1")
		}
	}
	op := map[int][2]string{1: {"G", "g"}, 3: {"RG", "rg"}, 4: {"K", "k"}}[len(c)]
	if op[0] == "" {
		return "", errors.New("pdftext: need 1, 3 or 4 color components in range 0 to 1")
	}
	if f == "c" {
		return fmtG(c...) + " " + op[0] + " ", nil
	}
	return fmtG(c...) + " " + op[1] + " ", nil
}

// opacityName is the ExtGState Page._set_opacity names, "" where none is
// needed.
func opacityName(CA, ca float64) string {
	if CA >= 1 && ca >= 1 {
		return ""
	}
	t := func(v float64) int {
		n := int(math.RoundToEven(math.Max(v, 0) * 100))
		return min(n, 99)
	}
	return fmt.Sprintf("fitzca%02d%02d", t(CA), t(ca))
}

// drawPath is Shape.finish of a path already drawn (cont): its colours,
// width, caps, dashes and paint operator, wrapped in q … Q.
func drawPath(cont string, o DrawOptions, closePath bool, gs string) (string, error) {
	color, width := o.Color, o.Width
	if width == 0 {
		color = nil
	} else if color == nil {
		width = 0
	}
	cs, err := colorCode(color, "c")
	if err != nil {
		return "", err
	}
	fs, err := colorCode(o.Fill, "f")
	if err != nil {
		return "", err
	}
	if gs != "" {
		cont = "/" + gs + " gs\n" + cont
	}
	if width != 1 && width != 0 {
		cont += fmtG(width) + " w\n"
	}
	if o.LineCap != 0 {
		cont = fmt.Sprintf("%d J\n", o.LineCap) + cont
	}
	if o.LineJoin != 0 {
		cont = fmt.Sprintf("%d j\n", o.LineJoin) + cont
	}
	if o.Dashes != "" && o.Dashes != "[] 0" {
		cont = o.Dashes + " d\n" + cont
	}
	if closePath {
		cont += "h\n"
	}
	if color != nil {
		cont += cs
	}
	switch {
	case o.Fill != nil && color != nil && o.EvenOdd:
		cont += fs + "B*\n"
	case o.Fill != nil && color != nil:
		cont += fs + "B\n"
	case o.Fill != nil && o.EvenOdd:
		cont += fs + "f*\n"
	case o.Fill != nil:
		cont += fs + "f\n"
	default:
		cont += "S\n"
	}
	return "\nq\n" + cont + "Q\n", nil
}

// lineContent is Page.draw_line's content.
func lineContent(g pageGeom, x1, y1, x2, y2 float64, o DrawOptions, gs string) (string, error) {
	ax, ay := transform(x1, y1, g.ipctm)
	bx, by := transform(x2, y2, g.ipctm)
	cont := fmtG(tuple(ax, ay)...) + " m\n" + fmtG(tuple(bx, by)...) + " l\n"
	return drawPath(cont, o, false, gs)
}

// rectContent is Page.draw_rect's content (no rounded corners).
func rectContent(g pageGeom, r Box, o DrawOptions, gs string) (string, error) {
	bx, by := transform(r.X0, r.Y1, g.ipctm)
	cont := fmtG(tuple(bx, by, math.Max(0, r.X1-r.X0), math.Max(0, r.Y1-r.Y0))...) + " re\n"
	return drawPath(cont, o, true, gs)
}

// pySplitLines is Python's str.splitlines.
func pySplitLines(s string) []string {
	var out []string
	r := []rune(s)
	start := 0
	for i := 0; i < len(r); i++ {
		switch r[i] {
		case '\n', '\r', '\v', '\f', 0x1c, 0x1d, 0x1e, 0x85, 0x2028, 0x2029:
			out = append(out, string(r[start:i]))
			if r[i] == '\r' && i+1 < len(r) && r[i+1] == '\n' {
				i++
			}
			start = i + 1
		}
	}
	if start < len(r) {
		out = append(out, string(r[start:]))
	}
	return out
}

func pyRstrip(s string) string {
	r := []rune(s)
	for len(r) > 0 && pyIsSpace(string(r[len(r)-1])) {
		r = r[:len(r)-1]
	}
	return string(r)
}

// expandTabs is str.expandtabs(n).
func expandTabs(s string, n int) string {
	var b strings.Builder
	col := 0
	for _, r := range s {
		switch {
		case r == '\t':
			if n > 0 {
				k := n - col%n
				b.WriteString(strings.Repeat(" ", k))
				col += k
			}
		case r == '\n' || r == '\r':
			b.WriteRune(r)
			col = 0
		default:
			b.WriteRune(r)
			col++
		}
	}
	return b.String()
}

// tjString is getTJstr: a simple font's codes as 2 hex digits (b7 beyond
// Latin-1), a CJK font's as 4 (char codes).
func tjString(text string, simple bool) string {
	if strings.HasPrefix(text, "[<") && strings.HasSuffix(text, ">]") {
		return text
	}
	if text == "" {
		return "[<>]"
	}
	var b strings.Builder
	b.WriteString("[<")
	for _, c := range text {
		switch {
		case simple && c < 256:
			fmt.Fprintf(&b, "%02x", c)
		case simple:
			b.WriteString("b7")
		default:
			fmt.Fprintf(&b, "%04x", c)
		}
	}
	b.WriteString(">]")
	return b.String()
}

func lineHeight(f *fontInfo, o TextOptions) float64 {
	switch {
	case o.LineHeight != 0:
		return o.FontSize * o.LineHeight
	case f.ascender-f.descender <= 1:
		return o.FontSize * 1.2
	}
	return o.FontSize * (f.ascender - f.descender)
}

func normRot(r int) (int, error) {
	if r%90 != 0 {
		return 0, errors.New("pdftext: bad rotate value")
	}
	for r < 0 {
		r += 360
	}
	return r % 360, nil
}

// textContent is Shape.insert_text's content: lines from the point down,
// as many as fit above the page's bottom; nlines is how many.
func textContent(g pageGeom, x, y float64, lines []string, f *fontInfo, o TextOptions, gs string) (string, int, error) {
	lh := lineHeight(f, o)
	tab := make([]string, len(lines))
	for i, t := range lines {
		tab[i] = tjString(t, f.simple)
	}
	cs, err := colorCode(o.Color, "c")
	if err != nil {
		return "", 0, err
	}
	fill := o.Fill
	fs, err := colorCode(fill, "f")
	if err != nil {
		return "", 0, err
	}
	if len(fill) == 0 && o.RenderMode == 0 {
		fill = o.Color
		fs, _ = colorCode(o.Color, "f")
	}
	rot, err := normRot(o.Rotate)
	if err != nil {
		return "", 0, err
	}
	cm := ""
	top, left := g.height-y-g.y, x+g.x
	space := top
	switch rot {
	case 90:
		left, top = g.height-y-g.y, -x-g.x
		cm += "0 1 -1 0 0 0 cm\n"
		space = g.width - math.Abs(top)
	case 270:
		left, top = -g.height+y+g.y, x+g.x
		cm += "0 -1 1 0 0 0 cm\n"
		space = math.Abs(top)
	case 180:
		left, top = -x-g.x, -g.height+y+g.y
		cm += "-1 0 0 -1 0 0 cm\n"
		space = math.Abs(y + g.y)
	}
	alpha := ""
	if gs != "" {
		alpha = "/" + gs + " gs\n"
	}
	name := strings.TrimPrefix(o.FontName, "/")
	s := "\nq\n" + alpha + "BT\n" + cm + "1 0 0 1 " + fmtG(left, top) + " Tm\n/" + name + " " + fmtG(o.FontSize) + " Tf "
	if o.RenderMode > 0 {
		s += fmt.Sprintf("%d Tr ", o.RenderMode) + fmtG(o.BorderWidth*o.FontSize) + " w " + fmtG(o.MiterLimit) + " M "
	}
	if o.Color != nil {
		s += cs
	}
	if fill != nil {
		s += fs
	}
	s += tab[0]
	n := 1
	if len(tab) > 1 {
		s += "TJ\n0 -" + fmtG(lh) + " TD\n"
	} else {
		s += "TJ"
	}
	for i := 1; i < len(tab); i++ {
		if space < lh {
			break
		}
		if i > 1 {
			s += "\nT* "
		}
		s += tab[i] + "TJ"
		space -= lh
		n++
	}
	return s + "\nET\nQ\n", n, nil
}

const epsilon = 1e-5

// textboxContent is Shape.insert_textbox's content, and its return value:
// the height left unused, or (negative, and no content) how much more
// the text needs.
func textboxContent(g pageGeom, r Box, text string, f *fontInfo, o TextOptions, gs string) (string, float64, error) {
	if r.X1 <= r.X0 || r.Y1 <= r.Y0 {
		return "", 0, errors.New("pdftext: text box must be finite and not empty")
	}
	cs, err := colorCode(o.Color, "c")
	if err != nil {
		return "", 0, err
	}
	fill := o.Fill
	fs, err := colorCode(fill, "f")
	if err != nil {
		return "", 0, err
	}
	if fill == nil && o.RenderMode == 0 {
		fill = o.Color
		fs, _ = colorCode(o.Color, "f")
	}
	alpha := ""
	if gs != "" {
		alpha = "/" + gs + " gs\n"
	}
	rot, err := normRot(o.Rotate)
	if err != nil {
		return "", 0, err
	}
	w, h := math.Max(0, r.X1-r.X0), math.Max(0, r.Y1-r.Y0)
	if text == "" {
		if rot == 0 || rot == 180 {
			return "", h, nil
		}
		return "", w, nil
	}
	name := strings.TrimPrefix(o.FontName, "/")
	lhf := o.LineHeight
	if lhf == 0 {
		lhf = 1.2
		if f.ascender-f.descender > 1 {
			lhf = f.ascender - f.descender
		}
	}
	lh := o.FontSize * lhf
	if f.simple {
		var b strings.Builder
		for _, c := range text {
			if c < 256 {
				b.WriteRune(c)
			} else {
				b.WriteRune('?')
			}
		}
		text = b.String()
	}
	t0 := pySplitLines(text)
	pixlen := func(x string) float64 {
		if f.ordering < 0 {
			sum := 0.0
			for _, c := range x {
				sum += f.adv[c]
			}
			return sum * o.FontSize
		}
		return float64(len([]rune(x))) * o.FontSize
	}
	blen := o.FontSize
	if f.ordering < 0 {
		blen = f.adv[32] * o.FontSize
	}
	progr := 1.0
	cx, cy := 0.0, o.FontSize*f.ascender
	var px, py, maxw, maxh float64
	cm := ""
	switch rot {
	case 0:
		px, py, maxw, maxh = r.X0+cx, r.Y0+cy, w, h
	case 90:
		cx, cy = o.FontSize*f.ascender, 0
		px, py, maxw, maxh = r.X0+cx, r.Y1+cy, h, w
		cm += "0 1 -1 0 0 0 cm\n"
	case 180:
		cx, cy = -0.0, -(o.FontSize * f.ascender)
		px, py, maxw, maxh = r.X1+cx, r.Y1+cy, w, h
		progr = -1
		cm += "-1 0 0 -1 0 0 cm\n"
	default:
		cx, cy = -(o.FontSize * f.ascender), -0.0
		px, py, maxw, maxh = r.X1+cx, r.Y0+cy, h, w
		progr = -1
		cm += "0 -1 1 0 0 0 cm\n"
	}

	out := ""
	var just []bool
	for i, line := range t0 {
		words := strings.Split(expandTabs(line, o.ExpandTabs), " ")
		lbuff := ""
		rest := maxw
		for _, word := range words {
			pl := pixlen(word)
			if rest >= pl {
				lbuff += word + " "
				rest -= pl + blen
				continue
			}
			if lbuff != "" {
				out += pyRstrip(lbuff) + "\n"
				just = append(just, true)
			}
			lbuff = ""
			rest = maxw
			if pl <= maxw {
				lbuff = word + " "
				rest = maxw - pl - blen
				continue
			}
			if len(just) > 0 {
				just[len(just)-1] = false
			}
			for _, c := range word {
				if pixlen(lbuff) <= maxw-pixlen(string(c)) {
					lbuff += string(c)
				} else {
					lbuff += "\n"
					out += lbuff
					just = append(just, false)
					lbuff = string(c)
				}
			}
			lbuff += " "
			rest = maxw - pixlen(lbuff)
		}
		if lbuff != "" {
			out += pyRstrip(lbuff)
			just = append(just, false)
		}
		if i < len(t0)-1 {
			out += "\n"
		}
	}
	out = strings.TrimSuffix(out, "\n")
	count := strings.Count(out, "\n") + 1
	more := lh*float64(count) - f.descender*o.FontSize - maxh
	if more > epsilon {
		return "", -more, nil
	}
	more = math.Abs(more)
	if more < epsilon {
		more = 0
	}
	s := "\nq\n" + alpha + "BT\n" + cm
	lines := pySplitLines(out)
	if len(just) > 0 {
		just[len(just)-1] = false
	}
	for i, t := range lines {
		spacing := 0.0
		pl := maxw - pixlen(t)
		k := float64(i) * lhf
		x, y := px+cx*k, py+cy*k
		switch o.Align {
		case AlignCenter:
			if rot == 0 || rot == 180 {
				x += pl / 2 * progr
			} else {
				y -= pl / 2 * progr
			}
		case AlignRight:
			if rot == 0 || rot == 180 {
				x += pl * progr
			} else {
				y -= pl * progr
			}
		case AlignJustify:
			if sp := strings.Count(t, " "); sp > 0 && i < len(just) && just[i] {
				spacing = pl / float64(sp)
			}
		}
		top, left := g.height-y-g.y, x+g.x
		switch rot {
		case 90:
			left, top = g.height-y-g.y, -x-g.x
		case 270:
			left, top = -g.height+y+g.y, x+g.x
		case 180:
			left, top = -x-g.x, -g.height+y+g.y
		}
		s += "1 0 0 1 " + fmtG(left, top) + " Tm /" + name + " " + fmtG(o.FontSize) + " Tf "
		if o.RenderMode > 0 {
			s += fmt.Sprintf("%d Tr ", o.RenderMode) + fmtG(o.BorderWidth*o.FontSize) + " w " + fmtG(o.MiterLimit) + " M "
		}
		if o.Align == AlignJustify {
			s += fmtG(spacing) + " Tw "
		}
		if o.Color != nil {
			s += cs
		}
		if fill != nil {
			s += fs
		}
		s += tjString(t, f.simple) + "TJ\n"
	}
	return s + "ET\nQ\n", more, nil
}
