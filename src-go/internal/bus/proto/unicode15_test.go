package proto

import (
	"testing"
	"unicode"
)

func TestUnicode15TableIsWhatGoAssignsAndPython14DoesNot(t *testing.T) {
	total := 0
	for i, rg := range unicode15New {
		if rg[0] > rg[1] || (i > 0 && unicode15New[i-1][1]+1 >= rg[0]) {
			t.Fatalf("range %d %X-%X is not sorted and apart", i, rg[0], rg[1])
		}
		for r := rg[0]; r <= rg[1]; r++ {
			if !unicode.In(r, unicode.L, unicode.M, unicode.N, unicode.P, unicode.S, unicode.Cf) {
				t.Fatalf("%U is not assigned in Go's tables", r)
			}
		}
		total += int(rg[1]-rg[0]) + 1
	}
	if total != 4489 || len(unicode15New) != 32 {
		t.Fatalf("%d code points in %d ranges", total, len(unicode15New))
	}
	if !NewInUnicode15(0x1fadc-2) || NewInUnicode15(0x1fad9) || NewInUnicode15('a') || !NewInUnicode15(0x323af) {
		t.Fatal("lookup")
	}
}
