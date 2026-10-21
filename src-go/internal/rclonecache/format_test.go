package rclonecache

import "testing"

// E15: the figures numfmt --to=iec --suffix=B writes (read off GNU coreutils numfmt).
func TestIEC(t *testing.T) {
	cases := map[int64]string{
		0: "0B", 1: "1B", 512: "512B", 1023: "1023B",
		1024: "1.0KB", 1025: "1.1KB", 1536: "1.5KB",
		10239: "10KB", 10240: "10KB", 10241: "11KB",
		1048575: "1.0MB", 1048576: "1.0MB",
		107374182400: "100GB", 429496729600: "400GB",
		1099511627776: "1.0TB", 999999999999999: "910TB",
		1 << 62: "4.0EB",
		-5:      "-5B",
	}
	for n, want := range cases {
		if got := IEC(n); got != want {
			t.Errorf("IEC(%d) = %q, want %q", n, got, want)
		}
	}
}

// E14: the cache max-age, coarsely.
func TestAge(t *testing.T) {
	cases := map[int64]string{
		-1: "off", 0: "off", 1: "1s", 59: "59s", 3599: "3599s",
		3600: "1h", 86399: "23h", 86400: "1d", 31536000: "365d",
	}
	for s, want := range cases {
		if got := Age(s); got != want {
			t.Errorf("Age(%d) = %q, want %q", s, got, want)
		}
	}
}
