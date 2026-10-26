package setuprclone

import (
	"strings"
	"testing"
)

// E15: a mount name.
func TestCheckName(t *testing.T) {
	others := []mount{newMount("r:/", "taken", "/m/taken")}
	for _, ok := range []string{"gdrive", "a", "0-x_y", strings.Repeat("a", 64)} {
		if err := checkName(ok, others); err != nil {
			t.Errorf("%q refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "Gdrive", "-a", "_a", "a b", "a.b", "a%", strings.Repeat("a", 65), "taken"} {
		if err := checkName(bad, others); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

// E15: the default name from a remote's name.
func TestDefaultName(t *testing.T) {
	cases := map[string]string{
		"gdrive":       "gdrive",
		"My Drive":     "my-drive",
		"Hetzner.Box!": "hetzner-box",
		"__x__":        "x",
		"..":           "mount",
		"a  .  b":      "a-b",
	}
	for in, want := range cases {
		if got := defaultName(in); got != want {
			t.Errorf("defaultName(%q) = %q, want %q", in, got, want)
		}
	}
}

// E15: a mountpoint.
func TestCheckMountpoint(t *testing.T) {
	others := []mount{newMount("r:/", "a", "/home/u/mnt/a")}
	for _, ok := range []string{"/home/u/mnt/b", "/home/u/mnt/ab", "/srv/x"} {
		if err := checkMountpoint(ok, others); err != nil {
			t.Errorf("%q refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "mnt/a", "/", "/home/u/mnt/b/", "/home/u//mnt", "/home/u/mnt/../x", "/home/u/mnt/a", "/home/u/mnt/a/sub", "/home/u/mnt", "/home/u/m nt", "/home/u/%h", "/home/u/{{x}}", "/home/u/$x", "/home/u/'x", "/home/u/\"x", "/home/u/`x`", "/home/u/a\\b", "/home/u/\tx"} {
		if err := checkMountpoint(bad, others); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

// E15: the remote spec.
func TestCheckRemote(t *testing.T) {
	for _, ok := range []string{"gdrive:/", "s3:bucket/photos", "box:", "a-b_c.d:x/y@z+1"} {
		if err := checkRemote(ok); err != nil {
			t.Errorf("%q refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"my drive:/", "g:/a b", "g:/%d", "g:/{x}", "g:/$x", "g:/'", "g:/\x01"} {
		if err := checkRemote(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
