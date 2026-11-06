package badlistingx

import (
	"slices"
	"testing"

	"fix/internal/badlisting"
)

// The mark crosses a package: a test of another package that compares what badlisting's List
// returns in directory order is judged by the fact exported when badlisting was analysed.
func TestAnotherPackagesListing(t *testing.T) {
	got, err := badlisting.Dir{}.List()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []badlisting.Entry{{Name: "2"}}) { // want `got is a directory listing in directory order`
		t.Error("List")
	}
	sorted, err := badlisting.Dir{}.SortedList()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(sorted, []badlisting.Entry{{Name: "2"}}) {
		t.Error("SortedList")
	}
}
