package ftpcamera

import (
	"slices"
	"testing"
)

func TestParseModesAndModifiers(t *testing.T) {
	o, help, problems := parse([]string{"--async", "--view", "--hotspot", "--debug-ftp"})
	if help || problems != nil || !o.async || o.asyncCopy || o.viewer != viewRAW || !o.hotspot || !o.debugFTP || !o.server() {
		t.Fatalf("%+v %v %v", o, help, problems)
	}
	o, help, problems = parse([]string{"--view-jpg"})
	if o.viewer != viewJPG || !o.server() || help || problems != nil {
		t.Fatalf("%+v", o)
	}
}

func TestParseNonServerModes(t *testing.T) {
	for _, flag := range []string{"--sort", "--pass", "--push", "--copy", "--copy-preflight", "--prune"} {
		o, help, problems := parse([]string{flag})
		if problems != nil || o.server() || help {
			t.Errorf("%s: %+v %v", flag, o, problems)
		}
	}
}

// E8: the launcher's choice as flags.
func TestLauncherFlags(t *testing.T) {
	cases := []struct {
		mode, viewer, hotspot string
		want                  []string
	}{
		{"default      [+viewer]  Start FTP server, sort at end on Ctrl+C", "no viewer", "no hotspot", nil},
		{"async        [+viewer]  Sort each upload as it arrives", "view RAW", "no hotspot", []string{"--async", "--view"}},
		{"default      [+viewer]  x", "no viewer", "use hotspot", []string{"--hotspot"}},
		{"prune                   Delete orphan RAW files (interactive) and exit", "", "", []string{"--prune"}},
	}
	for _, tc := range cases {
		got := launcherFlags(tc.mode, tc.viewer, tc.hotspot)
		if !slices.Equal(got, tc.want) {
			t.Errorf("%q %q %q: %q, want %q", tc.mode, tc.viewer, tc.hotspot, got, tc.want)
		}
	}
	if !serverKeyword("async-copy") || serverKeyword("pass") {
		t.Error("serverKeyword")
	}
}
