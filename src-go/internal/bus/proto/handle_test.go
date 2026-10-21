package proto

import (
	"errors"
	"testing"
)

func TestHandleErrorsAreHandleErrors(t *testing.T) {
	if _, err := NormaliseRepo("..."); !errors.Is(err, ErrHandle) {
		t.Fatalf("%v", err)
	}
	if _, err := BuildHandle("a", "Q", "h", "vm"); !errors.Is(err, ErrSeat) {
		t.Fatalf("%v", err)
	}
	if h, err := BuildNumberedHandle("a", 1, "h", "vm"); err != nil || h != "a.1+h.vm" {
		t.Fatalf("%q %v", h, err)
	}
}
