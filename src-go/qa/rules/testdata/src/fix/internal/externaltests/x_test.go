package externaltests_test

import (
	"testing"

	"fix/internal/externaltests"
)

func TestF(t *testing.T) {
	if externaltests.F() != 1 {
		t.Fatal("F")
	}
}
