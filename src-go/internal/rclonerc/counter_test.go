package rclonerc

import (
	"encoding/json"
	"testing"
)

// Counter: an RC counter as an int64; null (the empty number) is 0, a float is truncated, and
// anything out of range is refused.
func TestCounter(t *testing.T) {
	cases := map[json.Number]int64{"": 0, "0": 0, "42": 42, "10240.9": 10240, "-3": -3, "1e3": 1000}
	for in, want := range cases {
		if got, err := Counter(in); err != nil || got != want {
			t.Errorf("Counter(%q) = %d %v, want %d", in, got, err, want)
		}
	}
	for _, in := range []json.Number{"lots", "1e40", "-1e40"} {
		if got, err := Counter(in); err == nil {
			t.Errorf("Counter(%q) = %d, want a refusal", in, got)
		}
	}
}
