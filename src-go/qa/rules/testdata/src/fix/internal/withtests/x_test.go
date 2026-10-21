package withtests

import "testing"

func TestF(t *testing.T) {
	if F() != 1 {
		t.Fatal("F")
	}
}
