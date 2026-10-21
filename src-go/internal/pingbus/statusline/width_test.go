package statusline

import "testing"

// S27: colour escapes take no columns, wide and full-width characters two, and combining marks
// and format characters none, as unicodedata 14 (the container's Python 3.11) says.
func TestDisplayWidthCountsColumnsLikeThePython(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"desktop (worker)", 16},
		{"\x1b[36mdesktop\x1b[0m", 7},
		{"👥", 2},
		{"\xe6\xbc\xa2\xe5\xad\x97", 4},
		{"\xef\xbc\xa1\xef\xbc\xa2", 4},
		{"e\xcc\x81", 1},
		{"\xe2\x80\x8b", 0},
		{"a\xc2\xadb", 2},
		{"\U0001fa70", 2},
	}
	for _, c := range cases {
		if got := displayWidth(c.in); got != c.want {
			t.Errorf("displayWidth(%q): got %d, want %d", c.in, got, c.want)
		}
	}
}
