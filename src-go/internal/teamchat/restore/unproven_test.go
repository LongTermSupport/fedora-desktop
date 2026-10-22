package restore

import (
	"errors"
	"testing"
)

// RW2, n2g-D8: production's answer until H-C5 and H-C15 pass is a refusal (70), naming them.
func TestErrUnprovenIsARefusal(t *testing.T) {
	if !errors.Is(ErrUnproven, ErrRefused) {
		t.Fatal("ErrUnproven is not ErrRefused")
	}
	if want := "restore is not enabled: host checks H-C5 and H-C15 have not passed (DC8)"; ErrUnproven.Error() != want {
		t.Fatalf("ErrUnproven = %q, want %q", ErrUnproven, want)
	}
}
