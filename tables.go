package pdftext

import (
	"math"
	"sort"
	"strconv"
)

// Box is a box in points as PyMuPDF's table finder holds it, in float64:
// x0, top (y0), x1, bottom (y1).
type Box struct{ X0, Y0, X1, Y1 float64 }

// Table is one table of page.find_tables() (PyMuPDF 1.28.0, default
// settings: the "lines" strategy), as a TableFinder lists it.
type Table struct {
	BBox     Box      // table.bbox
	Cells    []Box    // table.cells, in the finder's order
	Rows     [][]*Box // table.rows: each row's cells by column, nil where a row has none
	RowCount int      // table.row_count
	ColCount int      // table.col_count
	text     [][]*string
}

// Extract is table.extract(): each cell's text, nil where the row has no
// cell in that column ("" for a cell without text).
func (t Table) Extract() [][]*string { return t.text }

// tableInput is what nx_table.c reads of a page.
type tableInput struct {
	Rect    Rect       `json:"rect"`
	Ctm     [6]float32 `json:"ctm"`
	RawDict struct {
		Blocks []struct {
			Lines []struct {
				Dir   Point `json:"dir"`
				Spans []struct {
					Size  float32 `json:"size"`
					BBox  Rect    `json:"bbox"`
					Chars []struct {
						BBox    Rect   `json:"bbox"`
						CtmBBox Rect   `json:"ctm_bbox"`
						C       string `json:"c"`
					} `json:"chars"`
				} `json:"spans"`
			} `json:"lines"`
		} `json:"blocks"`
	} `json:"rawdict"`
	Drawings []Path `json:"drawings"`
}

// tchar is one entry of table.py's CHARS.
type tchar struct {
	x0, x1, top, bottom, doctop float64
	y0, y1                      float64 // the box mapped by the page matrix
	upright                     bool
	matrix                      [4]float64 // a, b, c, d
	text                        string
	// small is the char's box in float32 (small glyph heights), for the
	// textbox test of cells_to_tables.
	small Rect
}

// The finder's default settings (TableSettings with find_tables' defaults).
const (
	snapTol         = 3.0
	joinTol         = 3.0
	edgeMinLength   = 3.0
	intersectionTol = 3.0
	textTol         = 3.0
)

// findTables is table.py's find_tables for one page read by nx_table.c;
// number is the page's 0-based number (doctop counts from page 0).
func findTables(in tableInput, number int) []Table {
	pageHeight := math.Max(0, float64(in.Rect.Y1)-float64(in.Rect.Y0))
	chars := makeChars(in, pageHeight, number)
	edges := makeEdges(in, chars, pageHeight, number)
	edges = getEdges(edges)
	cells := intersectionsToCells(edgesToIntersections(edges, intersectionTol, intersectionTol))
	var tables []Table
	for _, group := range cellsToTables(cells, chars) {
		tables = append(tables, newTable(group, chars))
	}
	return tables
}

// round4 is Python's round(x, 4).
func round4(x float64) float64 {
	v, _ := strconv.ParseFloat(strconv.FormatFloat(x, 'f', 4, 64), 64)
	return v
}

// makeChars is make_chars: the rawdict's characters, spans and chars each
// sorted by their left edge.
func makeChars(in tableInput, pageHeight float64, number int) []tchar {
	base := pageHeight * float64(number)
	var out []tchar
	for _, b := range in.RawDict.Blocks {
		for _, l := range b.Lines {
			d0, d1 := round4(float64(l.Dir.X)), round4(float64(l.Dir.Y))
			upright := d1 == 0
			spans := l.Spans
			sort.SliceStable(spans, func(i, j int) bool { return spans[i].BBox.X0 < spans[j].BBox.X0 })
			for _, s := range spans {
				cs := s.Chars
				sort.SliceStable(cs, func(i, j int) bool { return cs[i].BBox.X0 < cs[j].BBox.X0 })
				for _, c := range cs {
					x0, y0, x1, y1 := float64(c.BBox.X0), float64(c.BBox.Y0), float64(c.BBox.X1), float64(c.BBox.Y1)
					out = append(out, tchar{
						x0: x0, x1: x1, top: y0, bottom: y1, doctop: y0 + base,
						y0: float64(c.CtmBBox.Y0), y1: float64(c.CtmBBox.Y1),
						upright: upright, matrix: [4]float64{d0, -d1, d1, d0},
						text: c.C, small: c.BBox,
					})
				}
			}
		}
	}
	return out
}

// edge is one item of EDGES: a line of the page (object_type "line"),
// horizontal ('h') where top == bottom, else vertical ('v').
type edge struct {
	x0, x1, top, bottom, width, height, doctop, y0, y1 float64
	orient                                             byte
}

func (e edge) bbox() [4]float64 { return [4]float64{e.x0, e.top, e.x1, e.bottom} }

type pt struct{ x, y float64 }

func box32(r Rect) Box { return Box{float64(r.X0), float64(r.Y0), float64(r.X1), float64(r.Y1)} }

// makeEdges is make_edges: the drawings' lines, boxes and quads as
// axis-parallel lines, plus the border of every group of touching drawings
// that holds text.
func makeEdges(in tableInput, chars []tchar, pageHeight float64, number int) []edge {
	basis := float64(number) * pageHeight
	clip := box32(in.Rect)
	var edges []edge
	makeLine := func(p1, p2 pt) {
		if !(math.Abs(p1.x-p2.x) <= snapTol || math.Abs(p1.y-p2.y) <= snapTol) {
			return
		}
		x0, x1 := math.Min(p1.x, p2.x), math.Max(p1.x, p2.x)
		y0, y1 := math.Min(p1.y, p2.y), math.Max(p1.y, p2.y)
		if x0 > clip.X1 || x1 < clip.X0 || y0 > clip.Y1 || y1 < clip.Y0 {
			return
		}
		if x0 < clip.X0 {
			x0 = clip.X0
		}
		if x1 > clip.X1 {
			x1 = clip.X1
		}
		if y0 < clip.Y0 {
			y0 = clip.Y0
		}
		if y1 > clip.Y1 {
			y1 = clip.Y1
		}
		w, h := x1-x0, y1-y0
		if w == 0 && h == 0 {
			return
		}
		o := byte('v')
		if y0 == y1 {
			o = 'h'
		}
		edges = append(edges, edge{x0: x0, x1: x1, top: y0, bottom: y1, width: w, height: h,
			doctop: y0 + basis, y0: pageHeight - y0, y1: pageHeight - y1, orient: o})
	}
	p := func(q Point) pt { return pt{float64(q.X), float64(q.Y)} }

	for _, path := range in.Drawings {
		items := path.Items
		if path.ClosePath != nil && *path.ClosePath && len(items) > 0 && items[0].Op == "l" && items[len(items)-1].Op == "l" {
			items = append(items[:len(items):len(items)], PathItem{Op: "l", Points: []Point{items[len(items)-1].Points[1], items[0].Points[0]}})
		}
		for _, it := range items {
			switch it.Op {
			case "l":
				makeLine(p(it.Points[0]), p(it.Points[1]))
			case "re":
				r := box32(it.Rect)
				w, h := math.Max(0, r.X1-r.X0), math.Max(0, r.Y1-r.Y0)
				if w <= edgeMinLength && w < h {
					x := math.Abs(r.X1+r.X0) / 2
					makeLine(pt{x, r.Y0}, pt{x, r.Y1})
					continue
				}
				if h <= edgeMinLength && h < w {
					y := math.Abs(r.Y1+r.Y0) / 2
					makeLine(pt{r.X0, y}, pt{r.X1, y})
					continue
				}
				tl, bl, br, tr := pt{r.X0, r.Y0}, pt{r.X0, r.Y1}, pt{r.X1, r.Y1}, pt{r.X1, r.Y0}
				makeLine(tl, bl)
				makeLine(bl, br)
				makeLine(br, tr)
				makeLine(tr, tl)
			case "qu":
				ul, ur, ll, lr := p(it.Points[0]), p(it.Points[1]), p(it.Points[2]), p(it.Points[3])
				makeLine(ul, ll)
				makeLine(ll, lr)
				makeLine(lr, ur)
				makeLine(ur, ul)
			}
		}
	}
	for _, b := range cleanGraphics(in.Drawings, chars) {
		tl, tr, bl, br := pt{b.X0, b.Y0}, pt{b.X1, b.Y0}, pt{b.X0, b.Y1}, pt{b.X1, b.Y1}
		makeLine(tl, tr)
		makeLine(bl, br)
		makeLine(tl, bl)
		makeLine(tr, br)
	}
	return edges
}

// cleanGraphics is clean_graphics: the drawings' boxes joined where they
// touch (within the snap tolerance), each kept if a character lies inside.
// Python sorts a set of the boxes by (y1, x0); boxes equal in both are
// taken here by y0, then x1.
func cleanGraphics(paths []Path, chars []tchar) []Box {
	seen := map[Box]bool{}
	var rects []Box
	for _, p := range paths {
		b := box32(p.Rect)
		if !seen[b] {
			seen[b] = true
			rects = append(rects, b)
		}
	}
	sort.Slice(rects, func(i, j int) bool {
		a, b := rects[i], rects[j]
		if a.Y1 != b.Y1 {
			return a.Y1 < b.Y1
		}
		if a.X0 != b.X0 {
			return a.X0 < b.X0
		}
		if a.Y0 != b.Y0 {
			return a.Y0 < b.Y0
		}
		return a.X1 < b.X1
	})
	var out []Box
	for len(rects) > 0 {
		r0 := rects[0]
		for repeat := true; repeat; {
			repeat = false
			for i := len(rects) - 1; i > 0; i-- {
				if neighbors(r0, rects[i]) {
					r0 = includePoint(includePoint(r0, rects[i].X0, rects[i].Y0), rects[i].X1, rects[i].Y1)
					rects = append(rects[:i], rects[i+1:]...)
					repeat = true
				}
			}
		}
		if charsInRect(chars, r0) {
			out = append(out, r0)
		}
		rects = rects[1:]
	}
	return out
}

// includePoint is Rect |= Point.
func includePoint(r Box, x, y float64) Box {
	return Box{math.Min(r.X0, x), math.Min(r.Y0, y), math.Max(r.X1, x), math.Max(r.Y1, y)}
}

// neighbors is are_neighbors: some corner coordinate of one box lies within
// the other, widened by the snap tolerance, in both directions.
func neighbors(r1, r2 Box) bool {
	in := func(v, lo, hi float64) bool { return lo <= v && v <= hi }
	if (in(r1.X0, r2.X0-snapTol, r2.X1+snapTol) || in(r1.X1, r2.X0-snapTol, r2.X1+snapTol)) &&
		(in(r1.Y0, r2.Y0-snapTol, r2.Y1+snapTol) || in(r1.Y1, r2.Y0-snapTol, r2.Y1+snapTol)) {
		return true
	}
	return (in(r2.X0, r1.X0-snapTol, r1.X1+snapTol) || in(r2.X1, r1.X0-snapTol, r1.X1+snapTol)) &&
		(in(r2.Y0, r1.Y0-snapTol, r1.Y1+snapTol) || in(r2.Y1, r1.Y0-snapTol, r1.Y1+snapTol))
}

// charsInRect is chars_in_rect, which compares the box's y with each char's
// matrix-mapped y0, y1, as PyMuPDF does.
func charsInRect(chars []tchar, r Box) bool {
	for _, c := range chars {
		if r.X0 <= c.x0 && c.x1 <= r.X1 && r.Y0 <= c.y0 && r.Y1 >= c.y1 {
			return true
		}
	}
	return false
}
