package pdftext

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Dest is one value of doc.resolve_names(): where a named destination
// points. Page is 0-based, -1 where no page is found. With XYZ (a /XYZ
// destination) X, Y (PDF coordinates, origin bottom left) and Zoom are
// PyMuPDF's "to" and "zoom"; without, Dest is its "dest": the destination
// as PDF source, from its first "/" (all of it where no page comes first).
type Dest struct {
	Page int
	XYZ  bool
	X, Y float64
	Zoom float64
	Dest string
}

// destInput is what nx_dest_names writes.
type destInput struct {
	Pages []int               `json:"pages"`
	Names [][]json.RawMessage `json:"names"`
}

// pyDecode is bytes.decode(errors="replace") cut at the first NUL, as
// JM_UnicodeFromBuffer reads the printed object.
func pyDecode(b []byte) string {
	var sb strings.Builder
	for len(b) > 0 {
		r, n := utf8.DecodeRune(b)
		if r == 0 {
			break
		}
		sb.WriteRune(r) // an invalid byte is U+FFFD, one per byte
		b = b[n:]
	}
	return sb.String()
}

// resolveNames is resolve_names over what nx_dest_names read.
func resolveNames(in destInput) (map[string]Dest, error) {
	pageOf := map[int]int{}
	for i, x := range in.Pages {
		pageOf[x] = i
	}
	out := map[string]Dest{}
	for _, entry := range in.Names {
		if len(entry) != 2 {
			return nil, fmt.Errorf("pdftext: dest entry of %d items", len(entry))
		}
		var name string
		if err := json.Unmarshal(entry[0], &name); err != nil {
			return nil, err
		}
		d := Dest{Page: -1}
		if string(entry[1]) == "null" {
			out[name] = d
			continue
		}
		var raw []byte
		if err := json.Unmarshal(entry[1], &raw); err != nil {
			return nil, err
		}
		array := strings.ReplaceAll(pyDecode(raw), "null", "0")
		if r := []rune(array); len(r) >= 2 {
			array = string(r[1 : len(r)-1])
		} else {
			array = ""
		}
		idx := strings.Index(array, "/")
		if idx < 1 {
			d.Dest = array
			out[name] = d
			continue
		}
		sub := strings.TrimSpace(array[:idx])
		array = array[idx:]
		d.Dest = array
		if strings.HasPrefix(array, "/XYZ") {
			d.Dest, d.XYZ = "", true
			toks := strings.Fields(array)[1:]
			if len(toks) > 3 {
				toks = toks[:3]
			}
			for len(toks) < 3 {
				toks = append(toks, "0")
			}
			var t [3]float64
			for i, s := range toks {
				v, err := strconv.ParseFloat(s, 64)
				if err != nil {
					return nil, fmt.Errorf("pdftext: dest %q: %v", name, err)
				}
				t[i] = v
			}
			d.X, d.Y, d.Zoom = t[0], t[1], t[2]
		}
		if strings.HasSuffix(sub, "0 R") {
			x, err := strconv.Atoi(strings.Fields(sub)[0])
			if err != nil {
				return nil, fmt.Errorf("pdftext: dest %q: %v", name, err)
			}
			if p, ok := pageOf[x]; ok {
				d.Page = p
			}
		} else {
			p, err := strconv.Atoi(sub)
			if err != nil {
				return nil, fmt.Errorf("pdftext: dest %q: %v", name, err)
			}
			d.Page = p
		}
		out[name] = d
	}
	return out, nil
}
