package pdftext

import (
	"errors"
	"math"
	"sort"
	"strings"
	"unicode/utf8"
)

// SortedText is PyMuPDF 1.28.0's page.get_text("text", sort=True), which is
// utils.get_sorted_text over get_text_words(sort=True): words grouped into
// lines (tops or bottoms within 3 points), a gap between words written as
// spaces, lines sorted by their bottom, and the gap between lines as up to
// six newlines. The arithmetic is Python's: float64 over MuPDF's float32
// boxes, and round() rounds half to even.
func SortedText(words []Word) (string, error) {
	if len(words) == 0 {
		return "", nil
	}
	ws := sortWords(words)
	total := emptyRect
	for _, w := range ws {
		total = total.union(w.r)
	}
	var lines []line
	cur := line{r: ws[0].r, words: []word{ws[0]}}
	for _, w := range ws[1:] {
		if near(cur.r, w.r) {
			cur.words = append(cur.words, w)
			cur.r = cur.r.union(w.r)
			continue
		}
		lines = append(lines, cur)
		cur = line{r: w.r, words: []word{w}}
	}
	lines = append(lines, cur)
	for i := range lines {
		t, err := lineText(total, lines[i].words)
		if err != nil {
			return "", err
		}
		lines[i].text = t
	}
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].r.y1 < lines[j].r.y1 })
	return joinLines(lines)
}

// ErrNoSize is where Python divides by a zero width or height and raises
// ZeroDivisionError, so the page has no sorted text. Go also stops at a gap
// of more than maxGap spaces, which only a word a hair wide gives: Python
// would write them all (hundreds of megabytes for a sliver of a point), and
// that only wastes memory.
var ErrNoSize = errors.New("pdftext: a word or line with no usable width or height")

const (
	tolerance = 3.0
	maxGap    = 1 << 20
)

type word struct {
	r    rect
	text string
}

type line struct {
	r     rect
	words []word
	text  string
}

// near is the test both sorts use: the same top or the same bottom, within
// the tolerance.
func near(l, w rect) bool {
	return math.Abs(l.y0-w.y0) <= tolerance || math.Abs(l.y1-w.y1) <= tolerance
}

// sortWords is get_text_words' sort_words: by bottom then left, then each
// run of near words ordered left to right.
func sortWords(words []Word) []word {
	ws := make([]word, len(words))
	for i, w := range words {
		ws[i] = word{rect{float64(w.X0), float64(w.Y0), float64(w.X1), float64(w.Y1)}, w.Text}
	}
	sort.SliceStable(ws, func(i, j int) bool {
		if ws[i].r.y1 != ws[j].r.y1 {
			return ws[i].r.y1 < ws[j].r.y1
		}
		return ws[i].r.x0 < ws[j].r.x0
	})
	out := make([]word, 0, len(ws))
	start, lr := 0, ws[0].r
	for i := 1; i <= len(ws); i++ {
		if i < len(ws) && near(lr, ws[i].r) {
			lr = lr.union(ws[i].r)
			continue
		}
		run := ws[start:i]
		sort.SliceStable(run, func(a, b int) bool { return run[a].r.x0 < run[b].r.x0 })
		out = append(out, run...)
		if i < len(ws) {
			start, lr = i, ws[i].r
		}
	}
	return out
}

// lineText is get_sorted_text's line_text: words left to right, each gap as
// as many spaces as characters of the word would fill it, and at least one
// space between words that do not touch.
func lineText(clip rect, words []word) (string, error) {
	sort.SliceStable(words, func(i, j int) bool { return words[i].r.x0 < words[j].r.x0 })
	var b strings.Builder
	x1 := clip.x0
	for _, w := range words {
		if w.r.width() == 0 {
			return "", ErrNoSize
		}
		least := 1
		if x1 == clip.x0 || w.r.x0 <= x1 {
			least = 0
		}
		n := float64(utf8.RuneCountInString(w.text))
		gap := (w.r.x0 - x1) / w.r.width() * n
		if gap > maxGap {
			return "", ErrNoSize
		}
		b.WriteString(strings.Repeat(" ", max(pyRound(gap), least)))
		b.WriteString(w.text)
		x1 = w.r.x1
	}
	return b.String(), nil
}

// joinLines puts sorted lines together: one newline, plus one for each line
// height of space between them, up to six. A line that overlaps the one
// before by more than its height follows it with no newline at all, as
// Python's "\n" * (distance + 1) does for a negative distance.
func joinLines(lines []line) (string, error) {
	var b strings.Builder
	b.WriteString(lines[0].text)
	y1 := lines[0].r.y1
	for _, l := range lines[1:] {
		if l.r.height() == 0 {
			return "", ErrNoSize
		}
		d := min(pyRound((l.r.y0-y1)/l.r.height()), 5)
		b.WriteString(strings.Repeat("\n", max(d+1, 0)))
		b.WriteString(l.text)
		y1 = l.r.y1
	}
	return b.String(), nil
}

// pyRound is Python's round() to an int: half to even.
func pyRound(x float64) int { return int(math.RoundToEven(x)) }

// rect is pymupdf.Rect, with the parts the sort uses.
type rect struct{ x0, y0, x1, y1 float64 }

const (
	minInf = -2147483648.0 // FZ_MIN_INF_RECT
	maxInf = 2147483520.0  // FZ_MAX_INF_RECT
)

var emptyRect = rect{maxInf, maxInf, minInf, minInf} // EMPTY_RECT()

func (r rect) empty() bool     { return r.x0 >= r.x1 || r.y0 >= r.y1 }
func (r rect) infinite() bool  { return r == rect{minInf, minInf, maxInf, maxInf} }
func (r rect) width() float64  { return max(0, r.x1-r.x0) }
func (r rect) height() float64 { return max(0, r.y1-r.y0) }

// union is Rect.include_rect (r |= o): an empty side gives the other one.
func (r rect) union(o rect) rect {
	switch {
	case r.infinite() || o.infinite():
		return rect{minInf, minInf, maxInf, maxInf}
	case o.empty():
		return r
	case r.empty():
		return o
	}
	return rect{min(r.x0, o.x0), min(r.y0, o.y0), max(r.x1, o.x1), max(r.y1, o.y1)}
}
