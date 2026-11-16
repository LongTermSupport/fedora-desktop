package badstdin

import (
	"io"
	"os"
	"testing"
)

// A test reads what its fake stdin holds; the rule judges code that runs, not its tests.
func TestReadsItsOwnFake(t *testing.T) {
	if _, err := io.ReadAll(os.Stdin); err != nil {
		t.Fatal(err)
	}
}
