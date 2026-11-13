package link

import "testing"

// A test spells the expected path, the oracle the resolver is checked against: not judged.
func TestExpected(t *testing.T) {
	if Joined("/run/user/1000", 7) != "/run/user/1000/cc-socks/7.sock" {
		t.Fatal("wrong path")
	}
}
