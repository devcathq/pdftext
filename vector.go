package pdftext

// Path is one entry of page.get_drawings(extended=…), read with the page
// derotated.
//
// Type is "f" (fill), "s" (stroke) or "fs" (a fill and the stroke of the same
// items just after it, merged). With extended, "clip" (a clip path: Items,
// Scissor, EvenOdd, ClosePath) and "group" (a transparency group: Rect and
// the group fields) come in drawing order too, and every entry has its Level.
//
// The fill fields (Fill, FillOpacity, EvenOdd) are set where Type has an f,
// the stroke fields (Color, StrokeOpacity, Width, LineCap, LineJoin, Dashes)
// where it has an s; PyMuPDF has None for the others. A Fill or Color that is
// empty but not nil is PyMuPDF's (): no colourspace. ClosePath is nil where
// PyMuPDF has None (a fill whose subpaths never close).
type Path struct {
	Type    string     `json:"type"`
	Items   []PathItem `json:"items"`
	Rect    Rect       `json:"rect"`    // not for a clip
	Scissor Rect       `json:"scissor"` // a clip's
	Seqno   int        `json:"seqno"`   // not for a clip or group
	Layer   string     `json:"layer"`
	Level   int        `json:"level"` // extended only

	ClosePath *bool `json:"closePath"`
	EvenOdd   *bool `json:"even_odd"` // nil for a stroke clip

	Fill        []float32 `json:"fill"` // RGB, 0 to 1
	FillOpacity float32   `json:"fill_opacity"`

	Color         []float32 `json:"color"` // RGB, 0 to 1
	StrokeOpacity float32   `json:"stroke_opacity"`
	Width         float32   `json:"width"`
	LineCap       [3]int    `json:"lineCap"`
	LineJoin      float32   `json:"lineJoin"`
	Dashes        string    `json:"dashes"` // as PyMuPDF writes it: "[] 0", "[ 3 2 ] 0"

	Isolated  bool    `json:"isolated"`  // group
	Knockout  bool    `json:"knockout"`  // group
	BlendMode string  `json:"blendmode"` // group
	Opacity   float32 `json:"opacity"`   // group
}

// PathItem is one item of a path: a line "l" (Points: start, end), a curve
// "c" (start, two control points, end), a box "re" (Rect, normalized, and
// Orientation 1 or -1) or a quad "qu" (Points: ul, ur, ll, lr).
type PathItem struct {
	Op          string  `json:"op"`
	Points      []Point `json:"points"`
	Rect        Rect    `json:"rect"`
	Orientation int     `json:"orientation"`
}
