package pdftext

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

// A table's rows and its cell text, as table.py's Table.rows and
// Table.extract() give them (extract_text without layout, WordExtractor's
// defaults).

func newTable(cells []Box, chars []tchar) Table {
	t := Table{Cells: cells}
	t.BBox = Box{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	for _, c := range cells {
		t.BBox = Box{math.Min(t.BBox.X0, c.X0), math.Min(t.BBox.Y0, c.Y0), math.Max(t.BBox.X1, c.X1), math.Max(t.BBox.Y1, c.Y1)}
	}
	sorted := append([]Box(nil), cells...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Y0 != sorted[j].Y0 {
			return sorted[i].Y0 < sorted[j].Y0
		}
		return sorted[i].X0 < sorted[j].X0
	})
	set := map[float64]bool{}
	var xs []float64
	for _, c := range cells {
		if !set[c.X0] {
			set[c.X0] = true
			xs = append(xs, c.X0)
		}
	}
	sort.Float64s(xs)
	for i := 0; i < len(sorted); {
		j := i
		byX := map[float64]Box{}
		for j < len(sorted) && sorted[j].Y0 == sorted[i].Y0 {
			byX[sorted[j].X0] = sorted[j]
			j++
		}
		row := make([]*Box, len(xs))
		for k, x := range xs {
			if c, ok := byX[x]; ok {
				row[k] = &c
			}
		}
		t.Rows = append(t.Rows, row)
		i = j
	}
	t.RowCount = len(t.Rows)
	for _, r := range t.Rows {
		t.ColCount = max(t.ColCount, len(r))
	}
	for _, row := range t.Rows {
		rb := Box{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
		for _, c := range row {
			if c != nil {
				rb = Box{math.Min(rb.X0, c.X0), math.Min(rb.Y0, c.Y0), math.Max(rb.X1, c.X1), math.Max(rb.Y1, c.Y1)}
			}
		}
		var rowChars []tchar
		for _, ch := range chars {
			if inBox(ch, rb) {
				rowChars = append(rowChars, ch)
			}
		}
		texts := make([]*string, len(row))
		for k, c := range row {
			if c == nil {
				continue
			}
			var cc []tchar
			for _, ch := range rowChars {
				if inBox(ch, *c) {
					cc = append(cc, ch)
				}
			}
			s := extractText(cc)
			texts[k] = &s
		}
		t.text = append(t.text, texts)
	}
	return t
}

// inBox is char_in_bbox: the char's middle within the box, right and bottom
// edges excluded.
func inBox(c tchar, b Box) bool {
	v, h := (c.top+c.bottom)/2, (c.x0+c.x1)/2
	return h >= b.X0 && h < b.X1 && v >= b.Y0 && v < b.Y1
}

// pyIsSpace is Python's str.isspace for a one-character string.
func pyIsSpace(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f)) {
			return false
		}
	}
	return true
}

var ligatures = map[string]string{"ﬀ": "ff", "ﬃ": "ffi", "ﬄ": "ffl", "ﬁ": "fi", "ﬂ": "fl", "ﬆ": "st", "ﬅ": "st"}

type tword struct {
	text                        string
	x0, x1, top, bottom, doctop float64
	rotation                    int
}

// sortChars is WordExtractor.iter_sort_chars: upright chars first, each
// group in lines (clustered within the y tolerance) read left to right;
// vertical ones in columns, read bottom to top.
func sortChars(chars []tchar) []tchar {
	var out []tchar
	upKey := func(c tchar) float64 {
		if c.upright {
			return -1
		}
		return 0
	}
	for _, group := range clusterObjects(chars, upKey, 0) {
		up := group[0].upright
		key := func(c tchar) float64 { return c.x0 }
		along := func(c tchar) float64 { return c.doctop }
		if up {
			key, along = along, key
		}
		for _, line := range clusterObjects(group, key, textTol) {
			line = append([]tchar(nil), line...)
			sort.SliceStable(line, func(i, j int) bool { return along(line[i]) < along(line[j]) })
			if !up {
				for i, j := 0, len(line)-1; i < j; i, j = i+1, j-1 {
					line[i], line[j] = line[j], line[i]
				}
			}
			out = append(out, line...)
		}
	}
	return out
}

// newWord is char_begins_new_word with WordExtractor's defaults.
func newWord(prev, cur tchar) bool {
	if cur.upright {
		return cur.x0 < prev.x0 || cur.x0 > prev.x1+textTol || cur.top > prev.top+textTol
	}
	ax, bx, cx := -prev.bottom, -prev.top, -cur.bottom
	return cx < ax || cx > bx+textTol || cur.x0 > prev.x0+textTol
}

// mergeChars is WordExtractor.merge_chars.
func mergeChars(cs []tchar) tword {
	w := tword{x0: math.Inf(1), top: math.Inf(1), x1: math.Inf(-1), bottom: math.Inf(-1)}
	for _, c := range cs {
		w.x0, w.top = math.Min(w.x0, c.x0), math.Min(w.top, c.top)
		w.x1, w.bottom = math.Max(w.x1, c.x1), math.Max(w.bottom, c.bottom)
	}
	w.doctop = w.top + (cs[0].doctop - cs[0].top)
	m := cs[0].matrix
	if !cs[0].upright && m[1] < 0 {
		r := make([]tchar, len(cs))
		for i, c := range cs {
			r[len(cs)-1-i] = c
		}
		cs = r
		w.rotation = 270
	}
	if m[0] < 0 && m[3] < 0 {
		w.rotation = 180
	} else if m[1] > 0 {
		w.rotation = 90
	}
	var b strings.Builder
	for _, c := range cs {
		if l, ok := ligatures[c.text]; ok {
			b.WriteString(l)
		} else {
			b.WriteString(c.text)
		}
	}
	w.text = b.String()
	return w
}

// extractWords is WordExtractor.extract_words: chars in reading order,
// split into words at white space and at gaps, per run of chars of one
// uprightness.
func extractWords(chars []tchar) []tword {
	ordered := sortChars(chars)
	var words []tword
	for i := 0; i < len(ordered); {
		j := i
		for j < len(ordered) && ordered[j].upright == ordered[i].upright {
			j++
		}
		var cur []tchar
		flush := func() {
			if len(cur) > 0 {
				words = append(words, mergeChars(cur))
			}
			cur = nil
		}
		for _, c := range ordered[i:j] {
			switch {
			case pyIsSpace(c.text):
				flush()
			case c.text == "": // `"" in ""`: split_at_punctuation's empty string
				flush()
				cur = []tchar{c}
				flush()
			case len(cur) > 0 && newWord(cur[len(cur)-1], c):
				flush()
				cur = []tchar{c}
			default:
				cur = append(cur, c)
			}
		}
		flush()
		i = j
	}
	return words
}

// extractText is extract_text without layout: words joined by spaces, lines
// (words clustered by doctop) by newlines; turned text read along its turn.
func extractText(chars []tchar) string {
	if len(chars) == 0 {
		return ""
	}
	words := extractWords(chars)
	rotation := 0
	if len(words) > 0 {
		rotation = words[0].rotation
	}
	join := func(ws []tword) string {
		parts := make([]string, len(ws))
		for i, w := range ws {
			parts[i] = w.text
		}
		return strings.Join(parts, " ")
	}
	switch rotation {
	case 90:
		sort.SliceStable(words, func(i, j int) bool {
			if words[i].x1 != words[j].x1 {
				return words[i].x1 < words[j].x1
			}
			return -words[i].top < -words[j].top
		})
		return join(words)
	case 270:
		sort.SliceStable(words, func(i, j int) bool {
			if words[i].x1 != words[j].x1 {
				return -words[i].x1 < -words[j].x1
			}
			return words[i].top < words[j].top
		})
		return join(words)
	}
	var lines []string
	for _, l := range clusterObjects(words, func(w tword) float64 { return w.doctop }, textTol) {
		lines = append(lines, join(l))
	}
	s := strings.Join(lines, "\n")
	if rotation == 180 {
		r := []rune(s)
		for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
			r[i], r[j] = r[j], r[i]
		}
		s = strings.ReplaceAll(string(r), "\n", " ")
	}
	return s
}
