package wsi

import "testing"

func TestW20TheDurationIsItsWholeSeconds(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{{"2.000000\n", 2}, {"0.4", 0}, {"17", 17}, {"\t3.5\n", 3}}
	for _, c := range cases {
		if got, ok := wholeSecondsOf(c.in); !ok || got != c.want {
			t.Errorf("wholeSecondsOf(%q) = %d, %v; want %d", c.in, got, ok, c.want)
		}
	}
	for _, bad := range []string{"", "abc", "-", ".5x"} {
		if _, ok := wholeSecondsOf(bad); ok {
			t.Errorf("wholeSecondsOf(%q) was read as a number", bad)
		}
	}
}
