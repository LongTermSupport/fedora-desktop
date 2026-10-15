package ftpcamera

import "testing"

// E65: the values below are what coreutils numfmt --to=iec / --to=iec-i printed for them.
func TestIECMatchesNumfmt(t *testing.T) {
	cases := []struct {
		n       int64
		iec, iB string
	}{
		{0, "0", "0"},
		{1, "1", "1"},
		{999, "999", "999"},
		{1023, "1023", "1023"},
		{1024, "1.0K", "1.0Ki"},
		{1025, "1.1K", "1.1Ki"},
		{1536, "1.5K", "1.5Ki"},
		{2047, "2.0K", "2.0Ki"},
		{10239, "10K", "10Ki"},
		{10240, "10K", "10Ki"},
		{10241, "11K", "11Ki"},
		{15000, "15K", "15Ki"},
		{102400, "100K", "100Ki"},
		{1048575, "1.0M", "1.0Mi"},
		{1048576, "1.0M", "1.0Mi"},
		{5000000, "4.8M", "4.8Mi"},
		{1073741824, "1.0G", "1.0Gi"},
		{25000000000, "24G", "24Gi"},
	}
	for _, tc := range cases {
		if got := iec(tc.n, false); got != tc.iec {
			t.Errorf("iec(%d) = %q, want %q", tc.n, got, tc.iec)
		}
		if got := iec(tc.n, true); got != tc.iB {
			t.Errorf("iec-i(%d) = %q, want %q", tc.n, got, tc.iB)
		}
	}
	if got := iec(-5, false); got != "-5" {
		t.Errorf("negative: %q", got)
	}
}
