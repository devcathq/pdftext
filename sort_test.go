package pdftext

import (
	"errors"
	"testing"
)

func w(x0, y0, x1, y1 float32, text string) Word {
	return Word{X0: x0, Y0: y0, X1: x1, Y1: y1, Text: text}
}

// The cases follow PyMuPDF's get_sorted_text by hand; TestAgainstPyMuPDF
// checks the same code against Python's own output on real files.
func TestSortedText(t *testing.T) {
	cases := []struct {
		name  string
		words []Word
		want  string
	}{
		{"nothing", nil, ""},
		// gap (25-20)/10*2 = 1 space
		{"two words", []Word{w(25, 0, 35, 10, "cd"), w(10, 0, 20, 10, "ab")}, "ab cd"},
		// gap 2.5 rounds half to even: 2 spaces; 3.5 gives 4
		{"round half even", []Word{w(10, 0, 20, 10, "ab"), w(32.5, 0, 42.5, 10, "cd")}, "ab  cd"},
		{"round half even up", []Word{w(10, 0, 20, 10, "ab"), w(37.5, 0, 47.5, 10, "cd")}, "ab    cd"},
		// touching words: no space
		{"touching", []Word{w(10, 0, 20, 10, "ab"), w(20, 0, 30, 10, "cd")}, "abcd"},
		// tops within 3 points: one line, ordered left to right; gap 10/10*1
		{"same line", []Word{w(30, 1, 40, 12, "b"), w(10, 0, 20, 10, "a")}, "a b"},
		// a line 2 heights below: 3 newlines
		{"lines apart", []Word{w(10, 0, 20, 10, "a"), w(10, 30, 20, 40, "b")}, "a\n\n\nb"},
		// more than 5 heights: capped at 6 newlines
		{"far apart", []Word{w(10, 0, 20, 10, "a"), w(10, 200, 20, 210, "b")}, "a\n\n\n\n\n\nb"},
		// a tall line whose top is far above the previous bottom: round(-0.8)
		// is -1, so no newline; it keeps its indent from the left of all text
		{"overlapping lines", []Word{w(10, 0, 20, 10, "a"), w(30, -30, 40, 20, "b")}, "a  b"},
		// the first word sits at the left edge of all text: no indent
		{"indent", []Word{w(10, 0, 20, 10, "a"), w(30, 30, 40, 40, "b")}, "a\n\n\n  b"},
	}
	for _, c := range cases {
		got, err := SortedText(c.words)
		if err != nil || got != c.want {
			t.Errorf("%s: got %q, %v; want %q", c.name, got, err, c.want)
		}
	}
}

func TestSortedTextNoWidth(t *testing.T) {
	// Python divides by the word's width and raises ZeroDivisionError.
	_, err := SortedText([]Word{w(10, 0, 10, 10, "a")})
	if !errors.Is(err, ErrNoSize) {
		t.Fatalf("got %v, want ErrNoSize", err)
	}
	// ...and by a later line's height.
	_, err = SortedText([]Word{w(10, 0, 20, 10, "a"), w(10, 30, 20, 30, "b")})
	if !errors.Is(err, ErrNoSize) {
		t.Fatalf("got %v, want ErrNoSize", err)
	}
}
