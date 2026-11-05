package pdftext

import (
	"encoding/json"
	"fmt"
)

// ImageRef is one entry of page.get_images(full=True): an image XObject the
// page's resources list, those of its Form XObjects included.
type ImageRef struct {
	Xref          int
	SMask         int // xref of its /SMask (or /Mask), 0 for none
	Width, Height int
	BPC           int
	ColorSpace    string // its /ColorSpace name, "" when it is not a name
	AltColorSpace string // the alternate of a Separation or DeviceN space
	Name          string // its name in the resources, as /Im0
	Filter        string // its first /Filter
	Referencer    int    // xref of the Form XObject that lists it, 0 for the page
}

// UnmarshalJSON reads the tuple nx_image.c writes.
func (r *ImageRef) UnmarshalJSON(b []byte) error {
	var v []json.RawMessage
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	if len(v) != 10 {
		return fmt.Errorf("pdftext: image entry %s", b)
	}
	for i, dst := range []any{&r.Xref, &r.SMask, &r.Width, &r.Height, &r.BPC, &r.ColorSpace, &r.AltColorSpace, &r.Name, &r.Filter, &r.Referencer} {
		if err := json.Unmarshal(v[i], dst); err != nil {
			return err
		}
	}
	return nil
}

// Image is the image at an xref decoded, as Pixmap(doc, xref) gives it
// (optionally converted, as Pixmap(csRGB, pix)), with its tobytes("png").
type Image struct {
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	N          int    `json:"n"` // components, alpha included
	Alpha      bool   `json:"alpha"`
	ColorSpace string `json:"colorspace"` // MuPDF's name, as "DeviceRGB"; "" for none
	Digest     string `json:"digest"`     // hex of Pixmap.digest (MD5 of the pixels)
	PNG        []byte `json:"png"`
	// PNGError is set, and PNG nil, where tobytes("png") raises: a
	// colourspace of more than three components (CMYK).
	PNGError string `json:"png_error"`
}
