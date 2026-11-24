package a

import (
	"os"
	"testing"
)

func openAndClose() error {
	f, err := os.Open("x")
	if err != nil {
		return err
	}
	defer func() { err = f.Close() }() // want `a deferred function assigns err, which is not a named result \(project_defer_assign_lost\)`
	return nil
}

func TestOpenAndClose(t *testing.T) {
	if err := openAndClose(); err == nil {
		t.Fatal("x opened")
	}
}
