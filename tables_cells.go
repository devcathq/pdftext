package pdftext

import (
	"math"
	"sort"
)

// From edges to cells to tables, as table.py (pdfplumber's table finder,
// with PyMuPDF's changes) does it, in Python's float64 arithmetic and with
// its stable sorts.

// getEdges is TableFinder.get_edges for the "lines" strategy: the vertical,
// then the horizontal edges of at least 1 pt, snapped and joined, then those
// of at least edge_min_length.
func getEdges(all []edge) []edge {
	var v, h []edge
	for _, e := range all {
		if e.orient == 'v' && e.height >= 1 {
			v = append(v, e)
		}
	}
	for _, e := range all {
		if e.orient == 'h' && e.width >= 1 {
			h = append(h, e)
		}
	}
	merged := mergeEdges(append(v, h...))
	var out []edge
	for _, e := range merged {
		dim := e.width
		if e.orient == 'v' {
			dim = e.height
		}
		if dim >= edgeMinLength {
			out = append(out, e)
		}
	}
	return out
}

// clusterList is cluster_list: sorted values, a new cluster where a value
// is more than tol past the one before.
func clusterList(xs []float64, tol float64) [][]float64 {
	xs = append([]float64(nil), xs...)
	sort.Float64s(xs)
	var out [][]float64
	if tol == 0 || len(xs) < 2 {
		for _, x := range xs {
			out = append(out, []float64{x})
		}
		return out
	}
	cur := []float64{xs[0]}
	last := xs[0]
	for _, x := range xs[1:] {
		if x <= last+tol {
			cur = append(cur, x)
		} else {
			out = append(out, cur)
			cur = []float64{x}
		}
		last = x
	}
	return append(out, cur)
}

// clusterObjects is cluster_objects: the objects grouped by the cluster of
// their key, clusters in key order, objects in their order within each.
func clusterObjects[T any](xs []T, key func(T) float64, tol float64) [][]T {
	set := map[float64]bool{}
	var vals []float64
	for _, x := range xs {
		if k := key(x); !set[k] {
			set[k] = true
			vals = append(vals, k)
		}
	}
	index := map[float64]int{}
	for i, c := range clusterList(vals, tol) {
		for _, v := range c {
			index[v] = i
		}
	}
	sorted := append([]T(nil), xs...)
	sort.SliceStable(sorted, func(i, j int) bool { return index[key(sorted[i])] < index[key(sorted[j])] })
	var out [][]T
	for i, x := range sorted {
		if i == 0 || index[key(x)] != index[key(sorted[i-1])] {
			out = append(out, nil)
		}
		out[len(out)-1] = append(out[len(out)-1], x)
	}
	return out
}

// snap is snap_objects: each cluster of edges moved to its average along
// the axis (x0 for vertical edges, top for horizontal ones).
func snap(edges []edge, vertical bool, tol float64) []edge {
	key := func(e edge) float64 { return e.top }
	if vertical {
		key = func(e edge) float64 { return e.x0 }
	}
	var out []edge
	for _, c := range clusterObjects(edges, key, tol) {
		sum := 0.0
		for _, e := range c {
			sum += key(e)
		}
		avg := sum / float64(len(c))
		for _, e := range c {
			d := avg - key(e)
			if vertical {
				e.x0 += d
				e.x1 += d
			} else {
				e.top += d
				e.bottom += d
				e.doctop += d
				e.y0 -= d
				e.y1 -= d
			}
			out = append(out, e)
		}
	}
	return out
}

// mergeEdges is merge_edges: snapped, then each run of edges on one line
// joined where they come within the join tolerance.
func mergeEdges(edges []edge) []edge {
	var v, h []edge
	for _, e := range edges {
		if e.orient == 'v' {
			v = append(v, e)
		} else {
			h = append(h, e)
		}
	}
	edges = append(snap(v, true, snapTol), snap(h, false, snapTol)...)
	pos := func(e edge) float64 {
		if e.orient == 'h' {
			return e.top
		}
		return e.x0
	}
	sort.SliceStable(edges, func(i, j int) bool {
		if edges[i].orient != edges[j].orient {
			return edges[i].orient < edges[j].orient
		}
		return pos(edges[i]) < pos(edges[j])
	})
	var out []edge
	for i := 0; i < len(edges); {
		j := i
		for j < len(edges) && edges[j].orient == edges[i].orient && pos(edges[j]) == pos(edges[i]) {
			j++
		}
		out = append(out, joinEdges(edges[i:j], edges[i].orient)...)
		i = j
	}
	return out
}

// joinEdges is join_edge_group: edges along one line joined where one
// starts within the tolerance of where the last ended.
func joinEdges(group []edge, orient byte) []edge {
	lo := func(e edge) float64 { return e.x0 }
	hi := func(e edge) float64 { return e.x1 }
	if orient == 'v' {
		lo = func(e edge) float64 { return e.top }
		hi = func(e edge) float64 { return e.bottom }
	}
	sorted := append([]edge(nil), group...)
	sort.SliceStable(sorted, func(i, j int) bool { return lo(sorted[i]) < lo(sorted[j]) })
	joined := []edge{sorted[0]}
	for _, e := range sorted[1:] {
		last := &joined[len(joined)-1]
		if lo(e) <= hi(*last)+joinTol {
			if hi(e) > hi(*last) {
				if orient == 'h' {
					last.x1 = e.x1
					last.width = e.x1 - last.x0
				} else {
					diff := e.bottom - last.bottom
					last.bottom = e.bottom
					last.height += diff
					last.y0 -= diff
				}
			}
		} else {
			joined = append(joined, e)
		}
	}
	return joined
}

type crossing struct{ v, h []edge }

// edgesToIntersections is edges_to_intersections: where a vertical edge
// meets a horizontal one, within the tolerances.
func edgesToIntersections(edges []edge, xt, yt float64) map[pt]*crossing {
	var v, h []edge
	for _, e := range edges {
		if e.orient == 'v' {
			v = append(v, e)
		}
	}
	for _, e := range edges {
		if e.orient == 'h' {
			h = append(h, e)
		}
	}
	sort.SliceStable(v, func(i, j int) bool {
		if v[i].x0 != v[j].x0 {
			return v[i].x0 < v[j].x0
		}
		return v[i].top < v[j].top
	})
	sort.SliceStable(h, func(i, j int) bool {
		if h[i].top != h[j].top {
			return h[i].top < h[j].top
		}
		return h[i].x0 < h[j].x0
	})
	out := map[pt]*crossing{}
	for _, ve := range v {
		for _, he := range h {
			if ve.top <= he.top+yt && ve.bottom >= he.top-yt && ve.x0 >= he.x0-xt && ve.x0 <= he.x1+xt {
				p := pt{ve.x0, he.top}
				c := out[p]
				if c == nil {
					c = &crossing{}
					out[p] = c
				}
				c.v = append(c.v, ve)
				c.h = append(c.h, he)
			}
		}
	}
	return out
}

// intersectionsToCells is intersections_to_cells: for each point, in order,
// the smallest box it starts whose four corners are points joined by edges.
func intersectionsToCells(ix map[pt]*crossing) []Box {
	points := make([]pt, 0, len(ix))
	for p := range ix {
		points = append(points, p)
	}
	sort.Slice(points, func(i, j int) bool {
		if points[i].x != points[j].x {
			return points[i].x < points[j].x
		}
		return points[i].y < points[j].y
	})
	share := func(a, b []edge) bool {
		set := map[[4]float64]bool{}
		for _, e := range a {
			set[e.bbox()] = true
		}
		for _, e := range b {
			if set[e.bbox()] {
				return true
			}
		}
		return false
	}
	connects := func(p1, p2 pt) bool {
		if p1.x == p2.x && share(ix[p1].v, ix[p2].v) {
			return true
		}
		return p1.y == p2.y && share(ix[p1].h, ix[p2].h)
	}
	var cells []Box
	for i, p := range points {
		if i == len(points)-1 {
			break
		}
		var below, right []pt
		for _, q := range points[i+1:] {
			if q.x == p.x {
				below = append(below, q)
			}
			if q.y == p.y {
				right = append(right, q)
			}
		}
	search:
		for _, b := range below {
			if !connects(p, b) {
				continue
			}
			for _, r := range right {
				if !connects(p, r) {
					continue
				}
				br := pt{r.x, b.y}
				if ix[br] != nil && connects(br, r) && connects(br, b) {
					cells = append(cells, Box{p.x, p.y, br.x, br.y})
					break search
				}
			}
		}
	}
	return cells
}

// cellsToTables is cells_to_tables: cells grouped where they share a
// corner; groups of a single column, or whose box shows only white space
// (page.get_textbox over the finder's text page), dropped; the rest by
// their top-left cell.
func cellsToTables(cells []Box, chars []tchar) [][]Box {
	corners := func(c Box) [4]pt { return [4]pt{{c.X0, c.Y0}, {c.X0, c.Y1}, {c.X1, c.Y0}, {c.X1, c.Y1}} }
	remaining := append([]Box(nil), cells...)
	current := map[pt]bool{}
	var group []Box
	var tables [][]Box
	for len(remaining) > 0 {
		before := len(group)
		for _, c := range append([]Box(nil), remaining...) {
			cc := corners(c)
			take := len(group) == 0
			for _, k := range cc {
				take = take || current[k]
			}
			if take {
				for _, k := range cc {
					current[k] = true
				}
				group = append(group, c)
				for i, r := range remaining {
					if r == c {
						remaining = append(remaining[:i], remaining[i+1:]...)
						break
					}
				}
			}
		}
		if len(group) == before {
			tables = append(tables, group)
			current = map[pt]bool{}
			group = nil
		}
	}
	if len(group) > 0 {
		tables = append(tables, group)
	}
	var kept [][]Box
	for _, t := range tables {
		r := Box{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
		x0s, x1s := map[float64]bool{}, map[float64]bool{}
		for _, c := range t {
			r = Box{math.Min(r.X0, c.X0), math.Min(r.Y0, c.Y0), math.Max(r.X1, c.X1), math.Max(r.Y1, c.Y1)}
			x0s[c.X0], x1s[c.X1] = true, true
		}
		if len(x1s) < 2 || len(x0s) < 2 || !hasText(chars, r) {
			continue
		}
		kept = append(kept, t)
	}
	topLeft := func(t []Box) pt {
		m := pt{math.Inf(1), math.Inf(1)}
		for _, c := range t {
			if c.Y0 < m.x || (c.Y0 == m.x && c.X0 < m.y) {
				m = pt{c.Y0, c.X0}
			}
		}
		return m
	}
	sort.SliceStable(kept, func(i, j int) bool {
		a, b := topLeft(kept[i]), topLeft(kept[j])
		if a.x != b.x {
			return a.x < b.x
		}
		return a.y < b.y
	})
	return kept
}

// hasText is not white_spaces.issuperset(page.get_textbox(r)): some
// character other than Python's string.whitespace overlaps r, its box (with
// small glyph heights) and r compared in MuPDF's float32.
func hasText(chars []tchar, r Box) bool {
	a := Rect{float32(r.X0), float32(r.Y0), float32(r.X1), float32(r.Y1)}
	for _, c := range chars {
		b := c.small
		if a.X0 >= b.X1 || a.Y0 >= b.Y1 || a.X1 <= b.X0 || a.Y1 <= b.Y0 {
			continue
		}
		for _, ch := range c.text {
			switch ch {
			case ' ', '\t', '\n', '\r', '\x0b', '\x0c':
			default:
				return true
			}
		}
	}
	return false
}
