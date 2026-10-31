//go:build !cgo || nomupdf

package pdftext

import (
	"errors"
	"testing"
)

func TestUnavailable(t *testing.T) {
	if _, err := Open([]byte("%PDF-1.4")); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("got %v, want ErrUnavailable", err)
	}
}
