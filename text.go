package pdftext

import (
	"encoding/json"
	"fmt"
)

// Point is a point in points, in MuPDF's float32.
type Point struct{ X, Y float32 }

// UnmarshalJSON reads [x, y]; null (a NaN in MuPDF) stays zero.
func (p *Point) UnmarshalJSON(b []byte) error {
	var v []*float32
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	if len(v) != 2 {
		return fmt.Errorf("pdftext: point %s", b)
	}
	p.X, p.Y = val(v[0]), val(v[1])
	return nil
}

// UnmarshalJSON reads [x0, y0, x1, y1]; null (a NaN in MuPDF) stays zero.
func (r *Rect) UnmarshalJSON(b []byte) error {
	var v []*float32
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	if len(v) != 4 {
		return fmt.Errorf("pdftext: rect %s", b)
	}
	*r = Rect{val(v[0]), val(v[1]), val(v[2]), val(v[3])}
	return nil
}

func val(f *float32) float32 {
	if f == nil {
		return 0
	}
	return *f
}

// Block is one entry of page.get_text("blocks"): (x0, y0, x1, y1, text,
// block_no, block_type). With PyMuPDF's default flags for "blocks" every
// block is text (Type 0): images come only with TEXT_PRESERVE_IMAGES.
type Block struct {
	X0, Y0, X1, Y1 float32
	Text           string // the block's lines, each ended by "\n"
	No             int    // block_no, from 0
	Type           int    // block_type: 0 text, 1 image
}

// UnmarshalJSON reads a block as nx_text.c writes it.
func (b *Block) UnmarshalJSON(data []byte) error {
	var v struct {
		BBox Rect   `json:"bbox"`
		Text string `json:"text"`
		No   int    `json:"no"`
		Type int    `json:"type"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*b = Block{v.BBox.X0, v.BBox.Y0, v.BBox.X1, v.BBox.Y1, v.Text, v.No, v.Type}
	return nil
}

// TextDict is page.get_text("rawdict"): the page's size (its CropBox) and
// its blocks, then lines, then spans, then characters. The flags are
// PyMuPDF's TEXTFLAGS_RAWDICT, so image blocks are listed with their bytes.
type TextDict struct {
	Width  float64     `json:"width"`
	Height float64     `json:"height"`
	Blocks []DictBlock `json:"blocks"`
}

// DictBlock is one block of a TextDict: text (Type 0, with Lines) or image
// (Type 1, with the image fields).
type DictBlock struct {
	Type   int    `json:"type"`
	Number int    `json:"number"`
	BBox   Rect   `json:"bbox"`
	Flags  int    `json:"flags"` // text
	Lines  []Line `json:"lines"` // text

	// An image block, as PyMuPDF's: Image is the image's own stream where
	// PyMuPDF keeps that (Ext names it: jpeg, jpx, png, …), else PNG; Mask
	// is its soft mask as PNG, or nil. YRes is XRes, as in PyMuPDF.
	Width      int        `json:"width"`
	Height     int        `json:"height"`
	Ext        string     `json:"ext"`
	ColorSpace int        `json:"colorspace"` // number of colour components
	XRes       int        `json:"xres"`
	YRes       int        `json:"yres"`
	BPC        int        `json:"bpc"`
	Transform  [6]float32 `json:"transform"` // the "transform" key: a, b, c, d, e, f
	Size       int        `json:"size"`      // len(Image)
	Image      []byte     `json:"image"`
	Mask       []byte     `json:"mask"`
}

// Line is one line of a text block.
type Line struct {
	Spans []Span `json:"spans"`
	WMode int    `json:"wmode"`
	Dir   Point  `json:"dir"`
	BBox  Rect   `json:"bbox"`
}

// Span is a run of characters in one font, size, colour and style. Flags:
// superscript 1, italic 2, serif 4, monospaced 8, bold 16. Color is
// sRGB 0xRRGGBB.
type Span struct {
	Size      float32 `json:"size"`
	Flags     int     `json:"flags"`
	Bidi      int     `json:"bidi"`
	CharFlags int     `json:"char_flags"`
	Font      string  `json:"font"`
	Color     int     `json:"color"`
	Alpha     int     `json:"alpha"`
	Ascender  float32 `json:"ascender"`
	Descender float32 `json:"descender"`
	Chars     []Char  `json:"chars"`
	Origin    Point   `json:"origin"`
	BBox      Rect    `json:"bbox"`
}

// Char is one character of a span.
type Char struct {
	Origin    Point  `json:"origin"`
	BBox      Rect   `json:"bbox"`
	C         string `json:"c"`
	Synthetic bool   `json:"synthetic"`
}
